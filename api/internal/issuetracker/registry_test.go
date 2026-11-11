package issuetracker

import (
	"errors"
	"testing"
)

func TestNewUnknownKind(t *testing.T) {
	d, err := New(Kind("nope"), Options{})
	if d != nil {
		t.Fatalf("driver = %v, want nil", d)
	}
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("err = %v, want ErrUnsupportedKind", err)
	}
}
