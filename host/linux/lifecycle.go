//go:build linux && !android

package linux

import (
	"os"
	"os/signal"
	"syscall"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// The app's lifecycle on Linux, as on a Mac: it's in the background while
// none of its windows shows, each hidden, minimized or suspended (GDK's
// state for a window the compositor doesn't show: on another workspace,
// or covered), and as it quits: its last window closing, the session
// ending (GtkApplication's query-end, through the session manager or, in
// a sandbox, the portal), or a signal (SIGTERM as a systemd session
// stops, SIGINT, SIGHUP), which quits it as closing it does. Each sends
// Lifecycle, and the core's handlers run then: asuka/store writes what's
// pending, so nothing set in the app's last moment is lost. GLib's memory
// monitor's warnings are LifecycleMemoryWarning.

// GdkToplevelState's flags that mean the window isn't seen.
const (
	toplevelMinimized = 1 << 0
	toplevelSuspended = 1 << 16 // GTK 4.12
)

// toplevelState is a GdkToplevel's state (tests replace it).
var toplevelState = func(surface Ptr) uint32 {
	return uint32(Call(Sym("gdk_toplevel_get_state"), surface))
}

// lifecycle sends s to the core: Foreground and Background as the app's
// state changes (not again when it's already in it), MemoryWarning each
// time.
func (h *Host) lifecycle(s ops.Lifecycle) {
	switch s {
	case ops.LifecycleBackground:
		if h.inBackground {
			return
		}
		h.inBackground = true
	case ops.LifecycleForeground:
		if !h.inBackground || h.quitting {
			return
		}
		h.inBackground = false
	}
	if h.root == nil {
		return
	}
	h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventLifecycle, Int: int64(s)})
}

// watchWindow follows a window's showing: made visible or hidden, its
// surface's state (once it has one, as it's realized), and its end.
func (h *Host) watchWindow(w *win) {
	window := w.window
	Connect(window, "notify::visible", func(uintptr) { h.updateLifecycle() })
	Connect(window, "realize", func() {
		if s := NativeGetSurface(window); s != 0 {
			Connect(s, "notify::state", func(uintptr) { h.updateLifecycle() })
		}
	})
	// Destroyed (the user closed the main window, or the core a Window):
	// it's no longer the app's.
	Connect(window, "destroy", func() {
		w.closing = true
		if h.wins[window] == w {
			delete(h.wins, window)
		}
		if h.activeWin == w {
			h.activeWin = nil
		}
		h.updateLifecycle()
	})
}

// shown is whether a window is seen: visible, and neither minimized nor
// suspended.
func (h *Host) shown(w *win) bool {
	if w.closing || !WidgetGetVisible(w.window) {
		return false
	}
	s := NativeGetSurface(w.window)
	return s == 0 || toplevelState(s)&(toplevelMinimized|toplevelSuspended) == 0
}

// updateLifecycle sends Background when no window of the app's is seen,
// Foreground when one is again.
func (h *Host) updateLifecycle() {
	if h.root == nil {
		return // not mounted: it starts in the foreground
	}
	for _, w := range h.wins {
		if h.shown(w) {
			h.lifecycle(ops.LifecycleForeground)
			return
		}
	}
	h.lifecycle(ops.LifecycleBackground)
}

// quit ends the app as closing its last window does: Background (the
// store written), then the application's main loop.
func (h *Host) quit() {
	h.lifecycle(ops.LifecycleBackground)
	h.quitting = true
	ApplicationQuit(h.app)
}

// watchApp follows the application's end: the session ending
// (query-end; the session manager then has it quit, or ends it) and its
// shutdown however it quits. Before the application starts: the session
// manager is joined as it registers.
func (h *Host) watchApp() {
	if hasProperty(h.app, "register-session") {
		SetBoolProperty(h.app, "register-session", true)
		Connect(h.app, "query-end", func() { h.lifecycle(ops.LifecycleBackground) })
	}
	Connect(h.app, "shutdown", func() {
		h.lifecycle(ops.LifecycleBackground)
		h.quitting = true
	})
}

// watchSignals quits the app on SIGTERM, SIGINT and SIGHUP as closing it
// does, so its last values are written; a second signal ends it at once
// (the main thread may be stuck). Not under asuka dev, whose SIGINT saves
// the run's state (devstate.go).
func (h *Host) watchSignals() {
	if os.Getenv("ASUKA_STATE") != "" {
		return
	}
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		s := <-sig
		h.Post(h.quit)
		<-sig
		code := 128
		if n, ok := s.(syscall.Signal); ok {
			code += int(n)
		}
		os.Exit(code)
	}()
}

// watchMemory sends MemoryWarning as GLib's memory monitor warns
// (low-memory-monitor, the kernel's pressure stall information, or in a
// sandbox the portal).
func (h *Host) watchMemory() {
	f := Sym("g_memory_monitor_dup_default")
	if f == 0 {
		return
	}
	if m := Call(f); m != 0 { // kept for the process's life
		Connect(m, "low-memory-warning", func(level uintptr) { h.lifecycle(ops.LifecycleMemoryWarning) })
	}
}

// hasProperty is whether o's class has a property of the name.
func hasProperty(o Ptr, name string) bool {
	class := *At[Ptr](o) // G_OBJECT_GET_CLASS: an instance starts with its class
	return Call(Sym("g_object_class_find_property"), class, Addr(CString(name))) != 0
}
