package issuetracker

import "errors"

var (
	// ErrUnsupportedKind is returned by New for a Kind with no driver.
	ErrUnsupportedKind = errors.New("issuetracker: unsupported kind")
	// ErrProjectNotBound is returned when a project-scoped call names a project
	// other than the one the connection is bound to.
	ErrProjectNotBound = errors.New("issuetracker: project not bound to this connection")
	// ErrStateMappingUnset is returned when open/closed state mapping is needed
	// but the connection has not configured it.
	ErrStateMappingUnset = errors.New("issuetracker: state mapping not configured")
	// ErrInvalidLabelName is returned for a label name the issue tracker cannot carry.
	ErrInvalidLabelName = errors.New("issuetracker: invalid label name")
	// ErrRedirectRefused is returned when the server answers with a redirect.
	// The client never follows one, so the token cannot be forwarded.
	ErrRedirectRefused = errors.New("issuetracker: redirect refused")
)
