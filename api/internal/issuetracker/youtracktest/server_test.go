package youtracktest

import (
	"fmt"
	"net/http"
	"testing"
)

// recorder is a testing.TB that records Errorf calls and holds cleanups so
// the test can run them by hand. Embedding the interface leaves every other
// method nil: calling one fails loudly.
type recorder struct {
	testing.TB
	errors   []string
	cleanups []func()
}

func (r *recorder) Helper()                   {}
func (r *recorder) Cleanup(f func())          { r.cleanups = append(r.cleanups, f) }
func (r *recorder) Errorf(f string, a ...any) { r.errors = append(r.errors, fmt.Sprintf(f, a...)) }

func TestUnmodelledRouteIsLoud(t *testing.T) {
	rec := &recorder{TB: t}
	srv := NewServer(rec)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/nowhere", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+srv.Token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", resp.StatusCode)
	}
	for i := len(rec.cleanups) - 1; i >= 0; i-- {
		rec.cleanups[i]()
	}
	if len(rec.errors) != 1 {
		t.Fatalf("cleanup reported %d errors, want 1: %v", len(rec.errors), rec.errors)
	}
}

func TestModelledRoutesAreQuietAtCleanup(t *testing.T) {
	rec := &recorder{TB: t}
	srv := NewServer(rec)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/users/me?fields=id", nil)
	req.Header.Set("Authorization", "Bearer "+srv.Token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for i := len(rec.cleanups) - 1; i >= 0; i-- {
		rec.cleanups[i]()
	}
	if len(rec.errors) != 0 {
		t.Fatalf("cleanup reported %v", rec.errors)
	}
}

func TestKnownPathWithAnUnmodelledMethodIsLoud(t *testing.T) {
	rec := &recorder{TB: t}
	srv := NewServer(rec)
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/tags", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+srv.Token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 rather than a silent 405", resp.StatusCode)
	}
	for i := len(rec.cleanups) - 1; i >= 0; i-- {
		rec.cleanups[i]()
	}
	if len(rec.errors) != 1 {
		t.Fatalf("cleanup reported %d errors, want 1: %v", len(rec.errors), rec.errors)
	}
}

func TestWrongBearerTokenIsRefused(t *testing.T) {
	srv := NewServer(t)
	for name, auth := range map[string]string{
		"wrong token": "Bearer " + srv.Token + "x",
		"no scheme":   srv.Token,
		"absent":      "",
	} {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/users/me?fields=id", nil)
		if err != nil {
			t.Fatal(err)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, resp.StatusCode)
		}
	}
}
