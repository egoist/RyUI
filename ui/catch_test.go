package ui_test

import (
	"fmt"
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A build that panics shows the fallback, and the views it made before
// it panicked are destroyed; reset builds again.
func TestCatchBuild(t *testing.T) {
	broken := true
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("header"),
			ui.Catch(func() ui.Node {
				first := ui.StaticText("first")
				if broken {
					panic("no profile")
				}
				return ui.Column(ui.Style{}, first, ui.StaticText("second"))
			}, func(err any, reset func()) ui.Node {
				return ui.Button(ui.ButtonProps{Title: ui.Val(fmt.Sprint("retry: ", err)), OnPress: reset})
			}))
	})
	if h.One(ops.KindButton, "retry: no profile") == nil || len(h.Find(ops.KindText, "first")) != 0 {
		t.Fatalf("no fallback:\n%s", h.Root().Tree())
	}
	for _, v := range h.Views {
		if v.Text() == "first" {
			t.Fatal("the failed build's view wasn't destroyed")
		}
	}
	broken = false
	h.Tap(h.One(ops.KindButton, "retry: no profile"))
	r.Flush()
	if h.One(ops.KindText, "second") == nil || len(h.OfKind(ops.KindButton)) != 0 {
		t.Fatalf("reset didn't build again:\n%s", h.Root().Tree())
	}
}

// An effect in the caught UI that panics later swaps in the fallback and
// stops the failed UI's effects.
func TestCatchEffect(t *testing.T) {
	n := signal.New(1)
	runs := 0
	h, r := mount(t, func() ui.Node {
		return ui.Catch(func() ui.Node {
			signal.Effect(func() { n.Get(); runs++ })
			return ui.Text(signal.Func(func() string {
				if n.Get() == 0 {
					panic("division by zero")
				}
				return fmt.Sprint(10 / n.Get())
			}))
		}, func(err any, reset func()) ui.Node { return ui.StaticText(fmt.Sprint(err)) })
	})
	if h.One(ops.KindText, "10") == nil {
		t.Fatal("no text")
	}
	n.Set(0)
	r.Flush()
	if h.One(ops.KindText, "division by zero") == nil || len(h.Find(ops.KindText, "10")) != 0 {
		t.Fatalf("no fallback:\n%s", h.Root().Tree())
	}
	runs = 0
	n.Set(2)
	r.Flush()
	if runs != 0 {
		t.Fatal("the failed UI's effect still runs")
	}
}
