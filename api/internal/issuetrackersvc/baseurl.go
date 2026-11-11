package issuetrackersvc

import (
	"errors"
	"net/url"
	"strings"

	"github.com/vtmocanu/uzi/api/internal/config"
)

// strictBaseURL validates raw as an https scheme-and-host base URL and returns
// config.NormalizeForgeBaseURL's canonical form. Unlike that function it
// refuses, rather than strips, userinfo, an opaque part, a path other than
// empty or "/", a query and a fragment: stripping would let
// "https://allowed@evil" or "https://host/x" mean something other than what was
// checked. The returned string is the one value that is checked against the
// allowlist, stored, and bound into the token's AAD.
func strictBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid base URL")
	}
	// "?" and "#" with nothing after them parse to empty RawQuery/Fragment, so
	// they are refused on the text too.
	if u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "?#") {
		return "", errors.New("invalid base URL: use https scheme and host only, with no credentials, path, query or fragment")
	}
	norm, err := config.NormalizeForgeBaseURL(raw)
	if err != nil {
		return "", errors.New("invalid base URL: it must be an https URL with a host")
	}
	return norm, nil
}
