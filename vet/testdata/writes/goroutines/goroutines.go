package goroutines

import (
	"time"

	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

var (
	count = signal.New(0)
	done  = signal.New(false)
	items = signal.NewFunc([]string(nil), nil)
)

// Reported: a write on a goroutine.

func Goroutines() {
	go func() {
		count.Set(1) // want `count.Set on a goroutine writes a signal off the main thread: write it in ui.Post\(func\(\) \{ ... \}\)`
	}()
	go func() {
		defer done.Set(true) // want `done.Set on a goroutine`
		time.Sleep(time.Second)
		count.Update(func(n int) int { return n + 1 }) // want `count.Update on a goroutine`
	}()
	go count.Set(2) // want `count.Set on a goroutine`
	go load()       // want `load writes a signal \(items.Set\) on a goroutine, not the main thread: call it in ui.Post\(func\(\) \{ ... \}\)`
	go func() {
		store(fetch()) // want `store writes a signal \(items.Set\) on a goroutine`
	}()
	go func() {
		signal.Batch(func() { count.Set(3) }) // want `count.Set on a goroutine`
	}()
	go func() {
		reset := func() { count.Set(0) }
		reset() // want `reset writes a signal \(count.Set\) on a goroutine`
	}()
}

// Not reported: the write reaches the main thread, or isn't the
// goroutine's.

func Posted() {
	go func() {
		v := fetch()
		ui.Post(func() {
			items.Set(v)
			load()
		})
	}()
	go func() {
		later := func() { count.Set(1) }
		ui.Post(later)
	}()
	count.Set(4)
	load()
}

func load() { items.Set(fetch()) }

func store(v []string) { items.Set(v) }

func fetch() []string { return []string{"a"} }
