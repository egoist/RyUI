package ui_test

import (
	"strings"
	"testing"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// A popover points at its anchor, is as big as its content (no wider than
// MaxWidth), and closes when dismissed or when Open goes false.
func TestPopover(t *testing.T) {
	open := signal.New(false)
	dismissed := 0
	h, r := mount(t, func() ui.Node {
		return ui.Popover(ui.PopoverProps{Open: open, MaxWidth: 200, OnDismiss: func() { dismissed++ },
			Content: func() ui.Node {
				return ui.Column(ui.Style{Padding: ui.All(10)}, ui.StaticText("Hi"))
			}},
			ui.Button(ui.ButtonProps{Title: ui.Val("Info"), OnPress: func() { open.Set(true) }}))
	})
	if len(h.OfKind(ops.KindPopover)) != 0 {
		t.Fatal("a closed popover")
	}
	h.Tap(h.One(ops.KindButton, "Info"))
	r.Flush()
	pops := h.OfKind(ops.KindPopover)
	if len(pops) != 1 || len(h.Commands(ops.CmdPresent)) != 1 {
		t.Fatalf("popovers %d", len(pops))
	}
	pop := pops[0]
	anchor := h.One(ops.KindButton, "Info").Parent
	if ops.NodeID(pop.Int(ops.PropPopoverAnchor)) != anchor.ID {
		t.Fatal("it doesn't point at its anchor")
	}
	w, ht := pop.Float(ops.PropPopoverWidth), pop.Float(ops.PropPopoverHeight)
	text := h.One(ops.KindText, "Hi")
	if w != float64(text.Frame[2])+20 || ht != float64(text.Frame[3])+20 {
		t.Fatalf("popover %vx%v, text %v", w, ht, text.Frame)
	}
	h.Dismiss(pop)
	r.Flush()
	if open.Peek() || dismissed != 1 || len(h.OfKind(ops.KindPopover)) != 0 {
		t.Fatal("dismissing didn't close it")
	}
}

func TestPopoverMaxWidth(t *testing.T) {
	open := signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Popover(ui.PopoverProps{Open: open, MaxWidth: 200,
			Content: func() ui.Node { return ui.StaticText(strings.Repeat("word ", 30)) }},
			ui.StaticText("anchor"))
	})
	pop := h.OfKind(ops.KindPopover)[0]
	if w := pop.Float(ops.PropPopoverWidth); w != 200 {
		t.Fatalf("width %v", w)
	}
	if ht := pop.Float(ops.PropPopoverHeight); ht <= 20 {
		t.Fatalf("height %v: the text didn't wrap", ht)
	}
}
