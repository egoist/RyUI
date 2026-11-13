package ui_test

import (
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A pushed screen's OnBack takes the user's back: the host asks the
// core, and the screen pops only when every handler lets it.
func TestOnBackScreen(t *testing.T) {
	dirty := signal.New(true)
	asked := 0
	var nav *ui.Navigator
	editor := func() ui.Node {
		ui.OnBack(func() bool {
			asked++
			return dirty.Peek()
		})
		return ui.StaticText("editor")
	}
	h, r := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.Button(ui.ButtonProps{Title: ui.Val("Edit"), OnPress: func() {
				nav.Push(editor, ui.ScreenOptions{Title: ui.Val("Editor")})
			}})
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	stack := h.OfKind(ops.KindStack)[0]
	if stack.Children[0].Bool(ops.PropInterceptBack) {
		t.Fatal("the first screen takes back with no handler")
	}
	h.Tap(h.One(ops.KindButton, "Edit"))
	r.Flush()
	top := stack.Children[1]
	if !top.Bool(ops.PropInterceptBack) {
		t.Fatal("the editor doesn't take back")
	}
	h.Back(stack)
	r.Flush()
	if asked != 1 || len(stack.Children) != 2 || nav.Depth() != 2 {
		t.Fatalf("a dirty editor went back: asked %d, %d screens", asked, len(stack.Children))
	}
	dirty.Set(false)
	h.Back(stack)
	r.Flush()
	if asked != 2 || len(stack.Children) != 1 || nav.Depth() != 1 {
		t.Fatalf("a clean editor stayed: asked %d, %d screens", asked, len(stack.Children))
	}
	if len(h.Commands(ops.CmdBackExit)) != 0 {
		t.Fatal("a screen's back left the app")
	}
}

// Outside screens (and on a stack's first), OnBack is the app's: back
// with nothing to pop asks it, and when it lets go, the host is told to
// leave. Its handler goes with its owner.
func TestOnBackRoot(t *testing.T) {
	open := signal.New(true)
	panel := signal.New(true)
	h, r := mount(t, func() ui.Node {
		return ui.Show(open, func() ui.Node {
			ui.OnBack(func() bool {
				if panel.Peek() {
					panel.Set(false)
					return true
				}
				return false
			})
			return ui.StaticText("home")
		})
	})
	if !h.BackFromRoot() {
		t.Fatal("the root doesn't take back")
	}
	r.Flush()
	if panel.Peek() || len(h.Commands(ops.CmdBackExit)) != 0 {
		t.Fatal("the handler didn't take the first back")
	}
	h.BackFromRoot()
	r.Flush()
	if len(h.Commands(ops.CmdBackExit)) != 1 {
		t.Fatal("an untaken back didn't leave")
	}
	open.Set(false)
	r.Flush()
	if h.BackFromRoot() {
		t.Fatal("a disposed handler still takes back")
	}
}

// A sheet's OnBack takes its dismissal; letting it go closes the sheet.
func TestOnBackSheet(t *testing.T) {
	open := signal.New(true)
	keep := true
	dismissed := 0
	h, r := mount(t, func() ui.Node {
		return ui.Sheet(ui.SheetProps{Open: open, OnDismiss: func() { dismissed++ }, Content: func() ui.Node {
			ui.OnBack(func() bool { return keep })
			return ui.StaticText("compose")
		}})
	})
	sheet := h.OfKind(ops.KindSheet)[0]
	h.Dismiss(sheet)
	r.Flush()
	if !open.Peek() || len(h.OfKind(ops.KindSheet)) != 1 {
		t.Fatal("a sheet that keeps itself closed")
	}
	keep = false
	h.Dismiss(sheet)
	r.Flush()
	if open.Peek() || dismissed != 1 || len(h.OfKind(ops.KindSheet)) != 0 {
		t.Fatalf("the sheet stayed: open %v, dismissed %d", open.Peek(), dismissed)
	}
}
