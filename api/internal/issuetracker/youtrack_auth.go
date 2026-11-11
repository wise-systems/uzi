package issuetracker

import (
	"context"
	"net/http"
	"net/url"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// VerifyToken implements Driver. It asks YouTrack who the token belongs to.
func (y *youTrack) VerifyToken(ctx context.Context) (Account, error) {
	var u ytUser
	_, _, err := y.do(ctx, ytCall{
		method: http.MethodGet,
		path:   []string{"users", "me"},
		query:  url.Values{"fields": {"id,login"}},
		limit:  objectBytes,
		prov:   forge.ResponseSuccess,
	}, &u)
	if err != nil {
		return Account{}, err
	}
	return Account(u), nil
}
