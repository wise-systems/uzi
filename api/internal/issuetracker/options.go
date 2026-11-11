package issuetracker

import "time"

// ProjectRef identifies the project a connection is bound to.
type ProjectRef struct {
	ID        string
	ShortName string
}

// Options configures a driver. There is deliberately no TLS field: a driver
// trusts the system roots and nothing else (PRD #4173 S3).
type Options struct {
	// BaseURL is the issue tracker's root URL; the caller has already validated it.
	BaseURL string
	// Token is the permanent token. It never appears in a returned error.
	Token string
	// Project is the project the connection is bound to.
	Project ProjectRef
	// StateField, OpenState and ClosedState map neutral open/closed onto the
	// issue tracker's own state field and values.
	StateField  string
	OpenState   string
	ClosedState string
	// Timeout bounds one HTTP request; zero selects defaultTimeout.
	Timeout time.Duration
}
