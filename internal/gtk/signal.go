//go:build linux && !android

package gtk

import (
	"github.com/ebitengine/purego"
)

// Signals: purego can make only so many callbacks (about two thousand,
// never freed), so there is one C callback per signal signature, made
// once, and each connection's Go function is found by the data pointer
// GLib passes back: an index into handlers.

var (
	handlers  = map[uintptr]any{}
	nextID    uintptr
	cbDestroy uintptr // GClosureNotify: forgets a connection's function

	cb0, cb1, cb2, cb3, cbD, cbDD, cbIDD, cbPDD, cbKey, cbTick uintptr
)

func initSignals() {
	cbDestroy = purego.NewCallback(func(data, closure uintptr) uintptr {
		delete(handlers, data)
		return 0
	})
	cb0 = purego.NewCallback(func(obj, data uintptr) uintptr {
		switch f := handlers[data].(type) {
		case func():
			f()
		case func() bool:
			return Bool(f())
		}
		return 0
	})
	cb1 = purego.NewCallback(func(obj, a, data uintptr) uintptr {
		switch f := handlers[data].(type) {
		case func(uintptr):
			f(a)
		case func(uintptr) bool:
			return Bool(f(a))
		case func(uintptr) uintptr:
			return f(a)
		}
		return 0
	})
	cb2 = purego.NewCallback(func(obj, a, b, data uintptr) uintptr {
		switch f := handlers[data].(type) {
		case func(a, b uintptr):
			f(a, b)
		case func(a, b uintptr) bool:
			return Bool(f(a, b))
		case func(a, b uintptr) uintptr:
			return f(a, b)
		}
		return 0
	})
	cb3 = purego.NewCallback(func(obj, a, b, c, data uintptr) uintptr {
		switch f := handlers[data].(type) {
		case func(a, b, c uintptr):
			f(a, b, c)
		case func(a, b, c uintptr) bool:
			return Bool(f(a, b, c))
		}
		return 0
	})
	cbD = purego.NewCallback(func(obj uintptr, x float64, data uintptr) uintptr {
		if f, ok := handlers[data].(func(float64)); ok {
			f(x)
		}
		return 0
	})
	cbDD = purego.NewCallback(func(obj uintptr, x, y float64, data uintptr) uintptr {
		switch f := handlers[data].(type) {
		case func(x, y float64):
			f(x, y)
		case func(x, y float64) bool:
			return Bool(f(x, y))
		case func(x, y float64) uintptr:
			return f(x, y)
		}
		return 0
	})
	cbIDD = purego.NewCallback(func(obj uintptr, n int32, x, y float64, data uintptr) uintptr {
		if f, ok := handlers[data].(func(n int, x, y float64)); ok {
			f(int(n), x, y)
		}
		return 0
	})
	cbPDD = purego.NewCallback(func(obj, p uintptr, x, y float64, data uintptr) uintptr {
		switch f := handlers[data].(type) {
		case func(p uintptr, x, y float64) bool:
			return Bool(f(p, x, y))
		case func(p uintptr, x, y float64):
			f(p, x, y)
		case func(p uintptr, x, y float64) uintptr:
			return f(p, x, y)
		}
		return 0
	})
	cbKey = purego.NewCallback(func(obj uintptr, keyval, keycode, state uint32, data uintptr) uintptr {
		if f, ok := handlers[data].(func(keyval, keycode, state uint32) bool); ok {
			return Bool(f(keyval, keycode, state))
		}
		return 0
	})
	// GtkTickCallback: (widget, frame clock, data) → keep going.
	cbTick = purego.NewCallback(func(widget, clock, data uintptr) uintptr {
		if f, ok := handlers[data].(func(clock uintptr) bool); ok {
			return Bool(f(clock))
		}
		return 0
	})
}

func register(f any) uintptr {
	nextID++
	handlers[nextID] = f
	return nextID
}

func connect(obj Ptr, signal string, cb uintptr, f any) uint64 {
	id := register(f)
	s := CString(signal)
	r := Call(FnSignalConnectData, obj, Addr(s), cb, id, cbDestroy, 0)
	return uint64(r)
}

// Connect connects f to obj's signal. f's type says the signal's
// arguments (after the instance, before the data) and result:
//
//	func(), func() bool                       (instance)
//	func(uintptr) [bool|uintptr]              one pointer or integer
//	func(a, b uintptr) [bool|uintptr]         two
//	func(a, b, c uintptr) [bool]              three
//	func(float64)                             a double
//	func(x, y float64) [bool|uintptr]         two doubles
//	func(n int, x, y float64)                 an int and two doubles (a click)
//	func(p uintptr, x, y float64) [bool|uintptr] a pointer and two doubles (a drop)
//	func(keyval, keycode, state uint32) bool  a key
//
// It returns the handler's ID (Disconnect).
func Connect(obj Ptr, signal string, f any) uint64 {
	var cb uintptr
	switch f.(type) {
	case func(), func() bool:
		cb = cb0
	case func(uintptr), func(uintptr) bool, func(uintptr) uintptr:
		cb = cb1
	case func(a, b uintptr), func(a, b uintptr) bool, func(a, b uintptr) uintptr:
		cb = cb2
	case func(a, b, c uintptr), func(a, b, c uintptr) bool:
		cb = cb3
	case func(float64):
		cb = cbD
	case func(x, y float64), func(x, y float64) bool, func(x, y float64) uintptr:
		cb = cbDD
	case func(n int, x, y float64):
		cb = cbIDD
	case func(p uintptr, x, y float64) bool, func(p uintptr, x, y float64), func(p uintptr, x, y float64) uintptr:
		cb = cbPDD
	case func(keyval, keycode, state uint32) bool:
		cb = cbKey
	default:
		panic("gtk: Connect: unsupported handler type")
	}
	return connect(obj, signal, cb, f)
}

// Disconnect disconnects a handler Connect returned.
func Disconnect(obj Ptr, handler uint64) {
	if handler != 0 {
		Call(FnSignalHandlerDisconnect, obj, uintptr(handler))
	}
}

// AddTickCallback calls f at each frame the widget's frame clock draws
// while it returns true (gtk_widget_add_tick_callback).
func AddTickCallback(widget Ptr, f func(clock Ptr) bool) uint32 {
	id := register(f)
	return uint32(Call(FnWidgetAddTickCallback, widget, cbTick, id, cbDestroy))
}

// Sources: idle and timeout functions, by the same routing.

var cbSource uintptr

func initSources() {
	cbSource = purego.NewCallback(func(data uintptr) uintptr {
		if f, ok := handlers[data].(func() bool); ok {
			return Bool(f())
		}
		return 0
	})
}

// Priorities of GLib's main loop.
const (
	PriorityHigh        = -100
	PriorityDefault     = 0
	PriorityHighIdle    = 100
	PriorityDefaultIdle = 200
)

// TimeoutAdd calls f every ms milliseconds on the main thread while it
// returns true. Main thread.
func TimeoutAdd(ms uint32, f func() bool) uint32 {
	id := register(f)
	return uint32(Call(FnTimeoutAddFull, uintptr(PriorityDefault), uintptr(ms), cbSource, id, cbDestroy))
}

// IdleAdd calls f on the main thread when it's idle, while it returns
// true. Main thread (the host's Post wakes the loop with its own source).
func IdleAdd(priority int, f func() bool) uint32 {
	id := register(f)
	return uint32(Call(FnIdleAddFull, uintptr(priority), cbSource, id, cbDestroy))
}

// SourceRemove removes a source TimeoutAdd or IdleAdd returned.
func SourceRemove(id uint32) {
	if id != 0 {
		Call(FnSourceRemove, uintptr(id))
	}
}

// Waker returns a function that, from any goroutine, has f run on the
// main thread once the loop is next idle (at default priority, ahead of
// redraws). Make it on the main thread, once.
func Waker(f func()) func() {
	id := register(func() bool { f(); return false })
	return func() {
		// g_idle_add_full is thread-safe; no destroy notify, so the
		// function stays registered for the next wake.
		Call(FnIdleAddFull, uintptr(PriorityDefault), cbSource, id, 0)
	}
}

// AsyncCall is a GAsyncReadyCallback and its data, for a GIO-style
// asynchronous call.
type AsyncCall struct{ Callback, Data uintptr }

var cbAsync uintptr

// Async starts an asynchronous call (start passes the callback and data
// to the _async function) whose done runs once, on the main thread, with
// the source object and the GAsyncResult for its _finish.
func Async(start func(AsyncCall), done func(source, res Ptr)) {
	if cbAsync == 0 {
		cbAsync = purego.NewCallback(func(source, res, data uintptr) uintptr {
			f, _ := handlers[data].(func(source, res Ptr))
			delete(handlers, data)
			if f != nil {
				f(source, res)
			}
			return 0
		})
	}
	id := register(done)
	start(AsyncCall{Callback: cbAsync, Data: id})
}
