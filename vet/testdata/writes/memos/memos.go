package memos

import "github.com/egoist/ryui/signal"

var (
	count = signal.New(0)
	done  = signal.New(false)
	items = signal.NewFunc([]string(nil), nil)
)

func load() { items.Set(nil) }

// Reported: a write inside a memo.

func Memos() {
	signal.NewMemo(func() int {
		count.Set(1) // want `count.Set inside a memo panics: a memo only derives a value; write in an effect or an event handler`
		return 1
	})
	signal.NewMemoFunc(func() []string {
		signal.Untrack(func() int { count.Update(func(n int) int { return n + 1 }); return 0 }) // want `count.Update inside a memo panics`
		load()                                                                                  // want `load writes a signal \(items.Set\) inside a memo, which panics`
		return nil
	}, nil)
	signal.NewMemo(compute) // want `compute writes a signal \(done.Set\) inside a memo, which panics`
}

func compute() bool {
	done.Set(true)
	return true
}

// Not reported: an effect writes; a memo may make a handler that does.

func Effects() {
	signal.Effect(func() { count.Set(len(items.Get())) })
	signal.NewMemoFunc(func() func() {
		return func() { count.Set(0) }
	}, nil)
}
