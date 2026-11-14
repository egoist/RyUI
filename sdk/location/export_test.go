package location

import (
	"os"
	"testing"

	"github.com/egoist/asuka/internal/locationspec"
	"github.com/egoist/asuka/sdk/store"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "asuka-location")
	if err != nil {
		panic(err)
	}
	os.Setenv("ASUKA_DATA", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// reset forgets what a test before left: the host's capabilities, the
// handlers and held events, what's computed and kept.
func reset() {
	capsMu.Lock()
	capsGot, capsVal, capsMax = false, locationspec.Capabilities{}, 0
	capsMu.Unlock()
	events.mu.Lock()
	events.started = false
	events.handlers = [2][]*any{}
	events.held = [2][]locationspec.Event{}
	events.mu.Unlock()
	computed.mu.Lock()
	for _, t := range computed.dwells {
		t.Stop()
	}
	if computed.flushAt != nil {
		computed.flushAt.Stop()
	}
	stop := computed.stop
	computed.loaded, computed.st, computed.stop, computed.last, computed.seen = false, computedState{}, nil, nil, nil
	computed.pending, computed.flushAt, computed.dwells = nil, nil, nil
	computed.mu.Unlock()
	if stop != nil {
		stop()
	}
	store.Delete(storeKey)
}
