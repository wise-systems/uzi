package issuetrackersvc

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vtmocanu/uzi/api/internal/issuetracker"
	"github.com/vtmocanu/uzi/api/internal/store"
)

// Connect verifies a token against the issue tracker and stores it sealed for
// the user, creating the connection or rotating the existing one for the same
// (user, kind, base URL). The order is fixed: kind, token presence, token shape
// (issuetracker.ValidateToken), strict base URL, allowlist, driver build, token
// verification, account check, seal, upsert. A refused base URL never reaches the driver factory, so no outbound
// request or token use happens for it. The token is trimmed of surrounding
// whitespace, and it appears in no returned error or log line.
//
// Reconnecting never changes the account of a connection that a repo links to
// (PRD #4173 D11, D14): the new token must belong to the stored account, else the
// result is a Conflict. The refusal is made by the upsert statement itself and,
// when UseTransactions is wired, also by a locked check before it.
func (s *Service) Connect(ctx context.Context, userID uuid.UUID, kind, baseURL, token string) (Connection, error) {
	parsedKind, ok := issuetracker.ParseKind(strings.TrimSpace(kind))
	if !ok {
		return Connection{}, &Error{Kind: KindBadRequest, Message: "unsupported issue tracker kind; supported values: " + issuetracker.SupportedKindsList()}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Connection{}, &Error{Kind: KindBadRequest, Message: "a token is required"}
	}
	if err := issuetracker.ValidateToken(token); err != nil {
		// ValidateToken's message names the rule that failed and never
		// includes the token, so it is passed through.
		return Connection{}, &Error{Kind: KindBadRequest, Message: "invalid token: " + err.Error()}
	}
	norm, err := strictBaseURL(baseURL)
	if err != nil {
		return Connection{}, &Error{Kind: KindBadRequest, Message: err.Error()}
	}
	if !s.allowed(norm) {
		return Connection{}, &Error{Kind: KindBadRequest, Message: "base URL is not in the allowed issue tracker list"}
	}

	d, err := s.newDriver(parsedKind, issuetracker.Options{BaseURL: norm, Token: token})
	if err != nil {
		s.log.Error("build issue tracker driver", "kind", string(parsedKind), "error", err)
		// The driver's build errors never carry the token.
		return Connection{}, &Error{Kind: KindBadRequest, Message: "invalid token or connection settings: " + err.Error()}
	}
	account, err := d.VerifyToken(ctx)
	if err != nil {
		// err is already token-redacted by the driver.
		return Connection{}, &Error{Kind: KindBadGateway, Message: "token verification failed: " + err.Error()}
	}
	if err := validateAccount(account); err != nil {
		return Connection{}, &Error{Kind: KindBadGateway, Message: err.Error()}
	}

	sealed, err := sealToken(s.box, userID, parsedKind, norm, token)
	if err != nil {
		s.log.Error("seal issue tracker token", "error", err)
		return Connection{}, internal()
	}
	var row store.IssueTrackerConnection
	err = s.inTx(ctx, func(q Querier) error {
		// The existing row is locked FOR UPDATE first, so a link (which holds the
		// row FOR SHARE) and this reconnect cannot interleave: the link either
		// commits before the count below and is seen by it, or waits and then
		// finds the new account.
		existing, err := q.LockIssueTrackerConnectionByIdentityForUpdate(ctx, store.LockIssueTrackerConnectionByIdentityForUpdateParams{
			UserID: userID, Kind: string(parsedKind), BaseUrl: norm,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// A first connect: no repo can be linked to a row that does not exist.
		case err != nil:
			return err
		case existing.AccountID != account.ID:
			// A separate statement after the lock, so READ COMMITTED sees every link
			// committed before the lock was granted.
			n, err := q.CountReposLinkedToIssueTrackerConnection(ctx, existing.ID)
			if err != nil {
				return err
			}
			if n > 0 {
				return errReconnectChangesAccount()
			}
		}
		row, err = q.UpsertIssueTrackerConnection(ctx, store.UpsertIssueTrackerConnectionParams{
			UserID:          userID,
			Kind:            string(parsedKind),
			BaseUrl:         norm,
			AccountID:       account.ID,
			AccountLogin:    account.Login,
			TokenCiphertext: sealed,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The statement's own WHERE refused: a repo links to the row and the account
			// would change. This holds without the lock above, and closes the race of a
			// first connect against a link.
			return errReconnectChangesAccount()
		}
		return err
	})
	if err != nil {
		return Connection{}, s.txError("connect issue tracker", err)
	}
	return toConnection(row), nil
}

func errReconnectChangesAccount() *Error {
	return conflict("this connection is linked to a repo and the new token belongs to a different account; " +
		"use a token of the account the connection already stands for, or connect to a different base URL")
}
