package issuetracker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

const (
	// redactPlaceholder replaces any occurrence of the token in a message.
	redactPlaceholder = "[REDACTED]"
	// maxMessageBytes bounds a returned error message.
	maxMessageBytes = 512
	// minTokenLen is the shortest token a constructor accepts. A shorter secret
	// could not be scrubbed without mangling unrelated text.
	minTokenLen = 16
	// minSegmentLen is the shortest dot-separated token segment scrubbed alone.
	minSegmentLen = 8
)

// validateToken rejects a token too short to redact safely. The message never
// includes the token.
func validateToken(token string) error {
	if len(token) < minTokenLen {
		return errors.New("issuetracker: token shorter than the minimum length")
	}
	return nil
}

// redactor scrubs one token out of every error a driver returns. Build it from
// the token once, in the constructor.
type redactor struct {
	secrets []string
}

// newRedactor lists the token's raw, query-escaped, path-escaped and
// JSON-escaped forms, plus its final '.'-separated segment when long enough
// (a permanent token has the shape prefix:id.secret).
func newRedactor(token string) redactor {
	var secrets []string
	add := func(s string) {
		if s != "" && len(s) >= minSegmentLen {
			secrets = append(secrets, s)
		}
	}
	add(token)
	add(url.QueryEscape(token))
	add(url.PathEscape(token))
	if b, err := json.Marshal(token); err == nil && len(b) >= 2 {
		add(string(b[1 : len(b)-1]))
	}
	if i := strings.LastIndexByte(token, '.'); i >= 0 {
		add(token[i+1:])
	}
	return redactor{secrets: secrets}
}

// string scrubs secrets, strips control and bidi characters, and truncates.
func (r redactor) string(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, redactPlaceholder)
	}
	return truncate(sanitize(s), maxMessageBytes)
}

// sanitize replaces C0/C1 control characters and bidi overrides with a space.
func sanitize(s string) string {
	return strings.Map(func(c rune) rune {
		switch {
		case c < 0x20, c >= 0x7f && c <= 0x9f,
			c >= 0x202a && c <= 0x202e,
			c >= 0x2066 && c <= 0x2069:
			return ' '
		}
		return c
	}, s)
}

// truncate cuts s to at most limit bytes on a UTF-8 boundary.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// classifiedError carries a scrubbed message and a normalized class. It has no
// Unwrap on purpose: the original error may contain the token.
type classifiedError struct {
	message string
	class   forge.ErrorClass
}

func (e *classifiedError) Error() string           { return e.message }
func (e *classifiedError) Class() forge.ErrorClass { return e.class }

// sentinelError is a redacted error that stays identifiable: errors.Is matches
// the fixed sentinel and nothing else, because Unwrap returns only the
// sentinel. Use it only for a sentinel that carries no secret.
type sentinelError struct {
	classifiedError
	sentinel error
}

func (e *sentinelError) Unwrap() error { return e.sentinel }

// error returns err as a scrubbed, classified error, or nil for nil. A forge
// oversize error passes through as a fresh copy so its provenance survives.
func (r redactor) error(err error) error {
	return r.errorStatus(err, 0)
}

// errorStatus is error with the HTTP status of the response, when known.
func (r redactor) errorStatus(err error, status int) error {
	if err == nil {
		return nil
	}
	var large *forge.ResponseTooLargeError
	if errors.As(err, &large) {
		return &forge.ResponseTooLargeError{Provenance: large.Provenance, Outcome: large.Class()}
	}
	return &classifiedError{message: r.string(err.Error()), class: classify(err, status)}
}

// sentinel returns a redacted error for sentinel with extra context in its
// message. errors.Is(result, sentinel) holds; the chain exposes nothing else.
func (r redactor) sentinel(sentinel error, status int, detail string) error {
	msg := sentinel.Error()
	if detail != "" {
		msg += ": " + detail
	}
	return &sentinelError{
		classifiedError: classifiedError{message: r.string(msg), class: classifyStatus(status)},
		sentinel:        sentinel,
	}
}

func classify(err error, status int) forge.ErrorClass {
	if errors.Is(err, context.DeadlineExceeded) {
		return forge.ErrorClassTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return forge.ErrorClassTimeout
	}
	if c := classifyStatus(status); c != forge.ErrorClassOther {
		return c
	}
	return forge.Class(err)
}

func classifyStatus(status int) forge.ErrorClass {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return forge.ErrorClassAuth
	case status == http.StatusTooManyRequests:
		return forge.ErrorClassRateLimited
	case status >= 500:
		return forge.ErrorClassServerError
	}
	return forge.ErrorClassOther
}
