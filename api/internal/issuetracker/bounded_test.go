package issuetracker

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

func TestReadBounded(t *testing.T) {
	got, err := readBounded(strings.NewReader("hello"), 5, forge.ResponseSuccess)
	if err != nil || string(got) != "hello" {
		t.Fatalf("at limit: %q, %v", got, err)
	}
	_, err = readBounded(strings.NewReader("hello!"), 5, forge.ResponseIssueSuccess)
	if !errors.Is(err, forge.ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
	if !forge.IsIssueResponseTooLarge(err) {
		t.Fatal("provenance not issue success")
	}
	_, err = readBounded(strings.NewReader("hello!"), 5, forge.ResponseError)
	if forge.IsIssueResponseTooLarge(err) {
		t.Fatal("error provenance reported as issue success")
	}
}

func TestReadBoundedCapsDecompressedBytes(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(bytes.Repeat([]byte("a"), 64<<10)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if gz.Len() > 1<<10 {
		t.Fatalf("fixture not small: %d", gz.Len())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gz.Bytes())
	}))
	defer srv.Close()

	resp, err := newHTTPClient(0).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ContentLength >= 64<<10 && resp.ContentLength != -1 {
		t.Fatalf("declared length %d", resp.ContentLength)
	}
	_, err = readBounded(resp.Body, 1<<10, forge.ResponseIssueSuccess)
	if !forge.IsIssueResponseTooLarge(err) {
		t.Fatalf("decompressed overflow not capped: %v", err)
	}
}
