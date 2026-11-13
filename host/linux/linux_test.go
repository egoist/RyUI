//go:build linux && !android

package linux

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// GTK runs on the main thread: TestMain runs GLib's main loop there, and
// the tests send their work to it. They need a display (Xvfb, a Wayland
// compositor) and a session bus: run them under xvfb-run and
// dbus-run-session.

var (
	mainMu   sync.Mutex
	mainQ    []func()
	wakeMain func()
	testApp  Ptr
)

func TestMain(m *testing.M) {
	os.Setenv("ASUKA_TEST", "1")
	os.Setenv("GTK_A11Y", "none")
	if err := Load(); err != nil {
		fmt.Println("skipping the Linux host's tests:", err)
		os.Exit(0)
	}
	registerClasses()
	testApp = AdwApplicationNew("dev.asuka.HostTest", 1<<5) // non-unique
	var gerr Ptr
	if !ApplicationRegister(testApp, 0, &gerr) {
		fmt.Println("skipping the Linux host's tests: the application doesn't register (no display or session bus?)")
		os.Exit(0)
	}
	ApplicationHold(testApp)
	wakeMain = Waker(runMainQ)
	done := make(chan int, 1)
	go func() {
		code := m.Run()
		done <- code
		wakeMain()
	}()
	for {
		select {
		case code := <-done:
			os.Exit(code)
		default:
		}
		MainContextIteration(0, true)
	}
}

func runMainQ() {
	mainMu.Lock()
	q := mainQ
	mainQ = nil
	mainMu.Unlock()
	for _, f := range q {
		f()
	}
}

// onMain runs f on the main thread. A failure in f (fatal) ends f, not
// the main thread's loop, which t.Fatal's Goexit would.
func onMain(f func()) {
	ok := make(chan struct{})
	mainMu.Lock()
	mainQ = append(mainQ, func() {
		defer close(ok)
		defer func() {
			if r := recover(); r != nil && r != errFatal {
				panic(r)
			}
		}()
		f()
	})
	mainMu.Unlock()
	wakeMain()
	<-ok
}

var errFatal = new(int)

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
	panic(errFatal)
}

// mount mounts root on a new host with a window of its own, shown.
func mount(t *testing.T, root func() ui.Node) (*Host, *ui.Root) {
	return mountWith(t, nil, root)
}

// mountWith is mount with prep run on the new host first.
func mountWith(t *testing.T, prep func(h *Host), root func() ui.Node) (*Host, *ui.Root) {
	var h *Host
	var r *ui.Root
	onMain(func() {
		h = newHost()
		h.app = testApp
		if prep != nil {
			prep(h)
		}
		h.setup()
		h.enter(func() { r = ui.Mount(h, root) })
		h.showMain()
	})
	settle(t, h, func() bool { return h.root != nil && (h.main.envW > 0 || h.main.hidden) })
	t.Cleanup(func() {
		onMain(func() {
			h.enter(r.Dispose)
			for _, w := range h.wins {
				WindowDestroy(w.window)
			}
		})
	})
	return h, r
}

// settle lets the main loop run (layout, drawing, posted work) until
// cond holds, checked on the main thread after each turn; it fails after
// many turns without.
func settle(t *testing.T, h *Host, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		var ok bool
		onMain(func() { ok = cond() })
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond) // the loop runs its frames meanwhile
	}
	t.Fatal("the condition never held")
}

// find returns the nodes of kind k.
func find(h *Host, k ops.Kind) []*node {
	var out []*node
	for _, n := range h.nodes {
		if n.kind == k {
			out = append(out, n)
		}
	}
	return out
}

// click clicks a button, as the mouse does (GtkButton's clicked; its
// activate waits 250 ms showing it pressed, and takes no second one
// meanwhile).
func click(b Ptr) {
	s := CString("clicked")
	Call(Sym("g_signal_emit_by_name"), b, Addr(s))
}

// allocation is a widget's allocated box in its parent.
func allocation(w Ptr) (x, y, width, height float32) {
	var b Rect
	WidgetComputeBounds(w, WidgetGetParent(w), &b)
	return b.X, b.Y, b.W, b.H
}

func TestCounter(t *testing.T) {
	count := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Update(func(n int) int { return n + 1 }) }}),
		)
	})
	var label, button *node
	onMain(func() {
		texts, buttons := find(h, ops.KindText), find(h, ops.KindButton)
		if len(texts) != 1 || len(buttons) != 1 {
			fatalf(t, "%d texts, %d buttons", len(texts), len(buttons))
		}
		label, button = texts[0], buttons[0]
		if s := GoString(Call(Sym("gtk_label_get_text"), label.inner)); s != "Count: 0" {
			fatalf(t, "text %q", s)
		}
		// A real click, through the button's own activation.
		click(button.inner)
		click(button.inner)
	})
	settle(t, h, func() bool { return label.text == "Count: 2" })
	onMain(func() {
		if s := GoString(Call(Sym("gtk_label_get_text"), label.inner)); s != "Count: 2" {
			fatalf(t, "after two clicks: %q", s)
		}
	})
	// The views sit at the frames the core gave them.
	settle(t, h, func() bool {
		x, y, w, ht := allocation(button.widget)
		return x == button.frame[0] && y == button.frame[1] && w == button.frame[2] && ht == button.frame[3]
	})
}
