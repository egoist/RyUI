package dock_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/dock"
	"github.com/egoist/asuka/ui"
)

type call struct {
	method string
	args   []byte
}

func mount(t *testing.T, attentionID uint32) (*[]call, func()) {
	h := fake.New()
	var calls []call
	h.Modules = map[string]func(string, []byte) ([]byte, error){"dock": func(m string, a []byte) ([]byte, error) {
		calls = append(calls, call{m, a})
		if m == "attention" {
			return binary.LittleEndian.AppendUint32(nil, attentionID), nil
		}
		return nil, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	return &calls, func() {
		r.Flush()
		h.RunPosted()
		r.Flush()
	}
}

// SetProgress sends the state and the value, clamped; a value without a
// state shows as Normal.
func TestSetProgress(t *testing.T) {
	calls, run := mount(t, 0)
	for _, c := range []struct {
		p     dock.Progress
		state dock.State
		value float64
	}{
		{dock.Progress{Value: 0.25}, dock.Normal, 0.25},
		{dock.Progress{State: dock.Indeterminate}, dock.Indeterminate, 0},
		{dock.Progress{State: dock.Error, Value: 2}, dock.Error, 1},
		{dock.Progress{State: dock.Paused, Value: math.NaN()}, dock.Paused, 0},
		{dock.Progress{Value: -1}, dock.None, 0},
		{dock.Progress{}, dock.None, 0},
	} {
		*calls = nil
		dock.SetProgress(c.p)
		run()
		if len(*calls) != 1 || (*calls)[0].method != "progress" || len((*calls)[0].args) != 9 {
			t.Fatalf("%+v: calls %v", c.p, *calls)
		}
		a := (*calls)[0].args
		if dock.State(a[0]) != c.state || math.Float64frombits(binary.LittleEndian.Uint64(a[1:])) != c.value {
			t.Errorf("%+v: sent %v", c.p, a)
		}
	}
}

// RequestAttention asks with its kind; cancelling it cancels the host's
// request, whether its ID has come yet or not.
func TestRequestAttention(t *testing.T) {
	calls, run := mount(t, 7)
	cancel := dock.RequestAttention(dock.Critical)
	run()
	cancel()
	cancel()
	run()
	want := []call{{"attention", []byte{1}}, {"cancelAttention", []byte{7, 0, 0, 0}}}
	if len(*calls) != 2 || (*calls)[0].method != want[0].method || string((*calls)[0].args) != string(want[0].args) ||
		(*calls)[1].method != want[1].method || string((*calls)[1].args) != string(want[1].args) {
		t.Fatalf("calls %v", *calls)
	}

	// Cancelled before the host answered.
	*calls = nil
	cancel = dock.RequestAttention(dock.Informational)
	cancel()
	run()
	if len(*calls) != 2 || (*calls)[1].method != "cancelAttention" {
		t.Fatalf("early cancel: calls %v", *calls)
	}

	// A host that asks nothing (the app's in use) answers 0: nothing to
	// cancel.
	calls, run = mount(t, 0)
	cancel = dock.RequestAttention(dock.Informational)
	run()
	cancel()
	run()
	if len(*calls) != 1 {
		t.Fatalf("nothing asked: calls %v", *calls)
	}
}
