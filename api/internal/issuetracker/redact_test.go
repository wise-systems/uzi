package issuetracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// fixtureCredential assembles a credential-shaped value at runtime so no
// complete token literal sits in source. It contains characters that each
// escaping form rewrites (space, slash, quote).
func fixtureCredential() string {
	return strings.Join([]string{"perm", ":", "ab c/d\"e", ".", "Zq9xLm2VnB7wKp"}, "")
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestRedactorScrubsEveryEncoding(t *testing.T) {
	cred := fixtureCredential()
	r := newRedactor(cred)
	segment := cred[strings.LastIndexByte(cred, '.')+1:]
	jsonForm := strings.Trim(mustJSON(t, cred), `"`)
	for name, form := range map[string]string{
		"raw":     cred,
		"query":   url.QueryEscape(cred),
		"path":    url.PathEscape(cred),
		"json":    jsonForm,
		"segment": segment,
	} {
		t.Run(name, func(t *testing.T) {
			got := r.error(fmt.Errorf("boom %s end", form)).Error()
			if strings.Contains(got, form) {
				t.Fatalf("message still contains %s form: %q", name, got)
			}
			if !strings.Contains(got, redactPlaceholder) {
				t.Fatalf("no placeholder in %q", got)
			}
		})
	}
}

func mustJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRedactorShortSegmentKept(t *testing.T) {
	r := newRedactor(strings.Join([]string{"abcdefghijklmnop", ".", "xyz"}, ""))
	if got := r.string("see xyz here"); got != "see xyz here" {
		t.Fatalf("short segment scrubbed: %q", got)
	}
}

func TestRedactorStripsControlAndBidi(t *testing.T) {
	r := newRedactor(fixtureCredential())
	got := r.string("a\x00b\x1bc\u0085d‮e⁦f⁩g\th")
	for _, c := range got {
		if c < 0x20 || (c >= 0x7f && c <= 0x9f) || (c >= 0x202a && c <= 0x202e) || (c >= 0x2066 && c <= 0x2069) {
			t.Fatalf("unsafe rune %U in %q", c, got)
		}
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "h") {
		t.Fatalf("printable text lost: %q", got)
	}
}

func TestRedactorTruncatesOnRuneBoundary(t *testing.T) {
	r := newRedactor(fixtureCredential())
	in := strings.Repeat("a", maxMessageBytes-1) + "éé"
	got := r.string(in)
	if len(got) > maxMessageBytes {
		t.Fatalf("len = %d", len(got))
	}
	if got != strings.Repeat("a", maxMessageBytes-1) {
		t.Fatalf("split rune kept: len %d", len(got))
	}
}

func TestRedactorErrorHasNoUnwrap(t *testing.T) {
	r := newRedactor(fixtureCredential())
	orig := fmt.Errorf("wrap: %w", context.Canceled)
	err := r.error(orig)
	if errors.Is(err, context.Canceled) {
		t.Fatal("redacted error exposes the original chain")
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("redacted error has Unwrap")
	}
	if r.error(nil) != nil {
		t.Fatal("nil must stay nil")
	}
}

func TestRedactorClass(t *testing.T) {
	r := newRedactor(fixtureCredential())
	for _, tc := range []struct {
		name   string
		err    error
		status int
		want   forge.ErrorClass
	}{
		{"deadline", context.DeadlineExceeded, 0, forge.ErrorClassTimeout},
		{"net timeout", timeoutErr{}, 0, forge.ErrorClassTimeout},
		{"401", errors.New("x"), 401, forge.ErrorClassAuth},
		{"403", errors.New("x"), 403, forge.ErrorClassAuth},
		{"429", errors.New("x"), 429, forge.ErrorClassRateLimited},
		{"500", errors.New("x"), 500, forge.ErrorClassServerError},
		{"503", errors.New("x"), 503, forge.ErrorClassServerError},
		{"404", errors.New("x"), 404, forge.ErrorClassOther},
		{"plain", errors.New("x"), 0, forge.ErrorClassOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := forge.Class(r.errorStatus(tc.err, tc.status)); got != tc.want {
				t.Fatalf("Class = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRedactorKeepsOversizeProvenance(t *testing.T) {
	r := newRedactor(fixtureCredential())
	err := r.error(&forge.ResponseTooLargeError{Provenance: forge.ResponseIssueSuccess})
	if !forge.IsIssueResponseTooLarge(err) || !errors.Is(err, forge.ErrResponseTooLarge) {
		t.Fatalf("oversize identity lost: %v", err)
	}
}

func TestRedactorSentinelIdentifiableAndRedacted(t *testing.T) {
	cred := fixtureCredential()
	r := newRedactor(cred)
	err := r.sentinel(ErrRedirectRefused, 302, "to "+cred)
	if !errors.Is(err, ErrRedirectRefused) {
		t.Fatal("sentinel not identifiable")
	}
	if strings.Contains(err.Error(), cred) {
		t.Fatalf("token leaked: %q", err.Error())
	}
	if errors.Is(err, ErrProjectNotBound) {
		t.Fatal("matches an unrelated sentinel")
	}
	if got := errors.Unwrap(err); got != ErrRedirectRefused {
		t.Fatalf("Unwrap = %v, want only the sentinel", got)
	}
	if forge.Class(err) != forge.ErrorClassOther {
		t.Fatalf("class = %q", forge.Class(err))
	}
}

func TestValidateToken(t *testing.T) {
	if validateToken(strings.Repeat("a", minTokenLen-1)) == nil {
		t.Fatal("short token accepted")
	}
	if err := validateToken(strings.Repeat("a", minTokenLen)); err != nil {
		t.Fatal(err)
	}
	short := strings.Repeat("q", minTokenLen-1)
	if strings.Contains(validateToken(short).Error(), short) {
		t.Fatal("error leaks token")
	}
}
