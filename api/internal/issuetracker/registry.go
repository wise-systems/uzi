package issuetracker

import "fmt"

// Constructor builds a driver from options.
type Constructor func(Options) (Driver, error)

// constructors maps each Kind to its driver constructor. A driver adds one
// entry here; there is no init-time registration.
var constructors = map[Kind]Constructor{}

// New constructs the driver for kind. An unknown kind wraps ErrUnsupportedKind.
func New(kind Kind, opts Options) (Driver, error) {
	ctor, ok := constructors[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q (supported: %s)", ErrUnsupportedKind, string(kind), SupportedKindsList())
	}
	return ctor(opts)
}
