package app

import (
	"fmt"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A quit runs OnQuit's handlers, which may cancel it, but not as the
// session ends; a handler stops with its owner, or with stop. What they
// change reaches the host.
func TestOnQuit(t *testing.T) {
	h := fake.New()
	var log []string
	unsaved := signal.New(true)
	r := ui.Mount(h, func() ui.Node {
		OnQuit(func(e *QuitEvent) {
			log = append(log, fmt.Sprintf("root ending=%v", e.SessionEnding))
			if unsaved.Get() {
				e.Cancel()
			}
		})
		return ui.Text(signal.Func(func() string {
			if unsaved.Get() {
				return "unsaved"
			}
			return "saved"
		}))
	})
	stop := OnQuit(func(e *QuitEvent) { log = append(log, fmt.Sprintf("main canceled=%v", e.Canceled())) })
	r.Flush()
	if !boot.QuitRequested(false) {
		t.Fatal("a handler canceled the quit, and it goes on")
	}
	if boot.QuitRequested(true) {
		t.Fatal("the session ending was canceled")
	}
	stop()
	unsaved.Set(false)
	r.Flush()
	if boot.QuitRequested(false) {
		t.Fatal("a quit with nothing unsaved was canceled")
	}
	r.Dispose()
	boot.QuitRequested(false)
	want := "[root ending=false main canceled=true root ending=true main canceled=false root ending=false]"
	if fmt.Sprint(log) != want {
		t.Fatalf("handlers ran:\n%v\nwant\n%v", log, want)
	}
}

// A handler that changes the app as the quit is asked (an alert to show)
// has its change flushed to the host.
func TestOnQuitFlushes(t *testing.T) {
	h := fake.New()
	asked := signal.New(false)
	r := ui.Mount(h, func() ui.Node {
		OnQuit(func(e *QuitEvent) {
			asked.Set(true)
			e.Cancel()
		})
		return ui.Text(signal.Func(func() string {
			if asked.Get() {
				return "Save your changes?"
			}
			return "editing"
		}))
	})
	defer r.Dispose()
	r.Flush()
	boot.QuitRequested(false)
	h.RunPosted()
	if len(h.Find(ops.KindText, "Save your changes?")) != 1 {
		t.Fatalf("the alert's text didn't reach the host:\n%s", h.Log())
	}
}

// Another launch's arguments reach OnSecondInstance's handlers, on the
// main thread, until they stop.
func TestOnSecondInstance(t *testing.T) {
	h := fake.New()
	var got []string
	r := ui.Mount(h, func() ui.Node {
		OnSecondInstance(func(args []string, dir string) { got = append(got, fmt.Sprint(args, " in ", dir)) })
		return ui.StaticText("x")
	})
	r.Flush()
	boot.SecondInstance([]string{"--new-window", "a.md"}, "/home/a")
	h.RunPosted()
	r.Dispose()
	boot.SecondInstance([]string{"b.md"}, "/home/a")
	h.RunPosted()
	if fmt.Sprint(got) != "[[--new-window a.md] in /home/a]" {
		t.Fatalf("got %q", got)
	}
}

// Quit asks the host to quit, where one can.
func TestQuit(t *testing.T) {
	defer func(q func()) { boot.Quit = q }(boot.Quit)
	quits := 0
	boot.Quit = func() { quits++ }
	Quit()
	boot.Quit = nil
	Quit() // a phone's: nothing
	if quits != 1 {
		t.Fatalf("quits = %d", quits)
	}
}
