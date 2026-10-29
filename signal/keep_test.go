package signal_test

import (
	"testing"

	"github.com/egoist/nui/signal"
)

type private struct{ n int }

// component makes the signals a component would.
func component() (count *signal.Signal[int], rows []*signal.Signal[string], p *signal.Signal[private]) {
	count = signal.New(0)
	for i := 0; i < 3; i++ {
		rows = append(rows, signal.New(""))
	}
	p = signal.New(private{})
	return
}

func TestStateIsKeptAcrossRuns(t *testing.T) {
	defer signal.StopKeeping()
	signal.KeepState(nil)
	var dispose func()
	var count *signal.Signal[int]
	var rows []*signal.Signal[string]
	var p *signal.Signal[private]
	mount := func() { signal.Root(func(d func()) { dispose = d; count, rows, p = component() }) }
	mount()
	count.Set(3)
	rows[0].Set("a")
	rows[2].Set("c")
	p.Set(private{7})
	saved := signal.KeptState()
	dispose()

	// The next run: the same signals, made at the same places, come back.
	signal.KeepState(saved)
	mount()
	if count.Peek() != 3 || rows[0].Peek() != "a" || rows[1].Peek() != "" || rows[2].Peek() != "c" {
		t.Fatal(count.Peek(), rows[0].Peek(), rows[1].Peek(), rows[2].Peek())
	}
	// A value JSON would lose (unexported fields) starts over.
	if p.Peek() != (private{}) {
		t.Fatal(p.Peek())
	}
}

func TestAnOwnersSignalsGoWithIt(t *testing.T) {
	defer signal.StopKeeping()
	signal.KeepState(nil)
	// Made, disposed, made again (a Show toggled): the place is reused,
	// so there's one kept value, not two.
	for i := 0; i < 3; i++ {
		signal.Root(func(d func()) {
			signal.New(i)
			if i < 2 {
				d()
			}
		})
	}
	if got := signal.KeptState(); len(got) != 1 {
		t.Fatal(got)
	}
}
