// Package host is the interface between nui's core and a platform host
// (iOS, Android, the fake host for tests, desktop later).
package host

import (
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

// Environment is what the host reports about a root view.
type Environment = ops.Environment

// Host is a platform's side. Every method but Post is called on the main
// thread.
type Host interface {
	// Apply applies a batch of mount operations. The batch is valid only
	// during the call.
	Apply(b *ops.Batch)
	// Measure returns the size a native view wants within c. It is only
	// called for views created in an earlier Apply.
	Measure(id ops.NodeID, c layout.Constraints) layout.Size
	// Post schedules f on the main thread. Safe from any goroutine.
	Post(f func())
	// Environment returns the root view's current environment.
	Environment() Environment
}

// Core is what a host calls into, on the main thread: events and the
// synchronous requests of lists.
type Core interface {
	Dispatch(ev ops.Event)
	ListCount(list ops.NodeID) int
	ListBind(list ops.NodeID, index int, cell ops.NodeID)
	ListUnbind(list ops.NodeID, cell ops.NodeID)
}

// Connector is implemented by hosts that call into the core; ui.Mount
// hands them the core.
type Connector interface {
	Connect(c Core)
}
