package issuetracker

import (
	"net/http"
	"time"

	"github.com/vtmocanu/uzi/api/internal/httptransport"
)

// defaultTimeout bounds one request when Options.Timeout is zero.
const defaultTimeout = 15 * time.Second

// testTransport, when non-nil, replaces the production transport. It is nil
// outside tests: export_test.go sets it to trust an httptest server's roots,
// which Options deliberately cannot express.
var testTransport http.RoundTripper

// newHTTPClient builds the client a driver uses: an independent transport, a
// finite timeout, and no redirect following, so a Location header cannot carry
// the token to another host.
func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	var rt http.RoundTripper = httptransport.New(http.DefaultTransport)
	if testTransport != nil {
		rt = testTransport
	}
	return &http.Client{
		Transport: rt,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// isRedirect reports whether resp is any 3xx, including 304 and a response with
// no Location. None is ever followed or treated as success.
func isRedirect(resp *http.Response) bool {
	return resp.StatusCode >= 300 && resp.StatusCode < 400
}
