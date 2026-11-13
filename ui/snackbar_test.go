package ui_test

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A snackbar shows its message and action; its action runs OnPress, not
// OnDismiss, and the core destroys it.
func TestSnackbarAction(t *testing.T) {
	undone, dismissed := 0, 0
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Delete"), OnPress: func() {
			ui.Snackbar(ui.SnackbarProps{
				Message:   "Note deleted",
				Action:    &ui.Action{Title: ui.Val("Undo"), OnPress: func() { undone++ }},
				OnDismiss: func() { dismissed++ },
			})
		}})
	})
	h.Reset()
	h.Tap(h.One(ops.KindButton, "Delete"))
	s := h.Snackbar()
	if s == nil || s.String(ops.PropSnackbarMessage) != "Note deleted" || s.String(ops.PropSnackbarAction) != "Undo" {
		t.Fatalf("snackbar %v", s)
	}
	if _, ok := s.Props[ops.PropSnackbarDuration]; ok {
		t.Fatal("the default duration is sent")
	}
	if h.PresentedOn(s) != h.Root() {
		t.Fatal("not on the main window")
	}
	// One view, its props, its two listens and Present: nothing else.
	if n := h.Count(ops.OpCreate); n != 1 {
		t.Fatalf("%d creates\n%s", n, h.Log())
	}
	if n := len(h.Commands(ops.CmdPresent)); n != 1 {
		t.Fatalf("%d presents", n)
	}
	h.PressSnackbarAction(s)
	if undone != 1 || dismissed != 0 || h.Snackbar() != nil || len(h.OfKind(ops.KindSnackbar)) != 0 {
		t.Fatalf("undone %d, dismissed %d, snackbars %d", undone, dismissed, len(h.OfKind(ops.KindSnackbar)))
	}
}

// Timing out and the user's swipe both report Dismissed: OnDismiss runs,
// once, and the core destroys it.
func TestSnackbarTimeout(t *testing.T) {
	dismissed := 0
	h, r := mount(t, func() ui.Node { return ui.Column(ui.Style{}) })
	ui.Snackbar(ui.SnackbarProps{Message: "Saved", Duration: ui.SnackbarLong, OnDismiss: func() { dismissed++ }})
	r.Flush()
	s := h.Snackbar()
	if s.Int(ops.PropSnackbarDuration) != int64(ops.SnackbarDurationLong) {
		t.Fatal("duration")
	}
	if _, ok := s.Props[ops.PropSnackbarAction]; ok {
		t.Fatal("an action without one")
	}
	h.TimeOut(s)
	if dismissed != 1 || len(h.OfKind(ops.KindSnackbar)) != 0 {
		t.Fatalf("dismissed %d", dismissed)
	}
	dismiss := ui.Snackbar(ui.SnackbarProps{Message: "Saved", OnDismiss: func() { dismissed++ }})
	r.Flush()
	h.Dismiss(h.Snackbar())
	dismiss() // gone already: nothing
	if dismissed != 2 || len(h.OfKind(ops.KindSnackbar)) != 0 {
		t.Fatalf("dismissed %d", dismissed)
	}
}

// A new snackbar replaces the one showing: the old one is destroyed and
// its OnDismiss runs, before the new one shows.
func TestSnackbarReplaces(t *testing.T) {
	var log []string
	h, r := mount(t, func() ui.Node { return ui.Column(ui.Style{}) })
	ui.Snackbar(ui.SnackbarProps{Message: "one", OnDismiss: func() { log = append(log, "one dismissed") }})
	r.Flush()
	first := h.Snackbar()
	h.Reset()
	ui.Snackbar(ui.SnackbarProps{Message: "two", OnDismiss: func() { log = append(log, "two dismissed") }})
	log = append(log, "two shown")
	r.Flush()
	if h.Views[first.ID] != nil || len(h.OfKind(ops.KindSnackbar)) != 1 || h.Snackbar().String(ops.PropSnackbarMessage) != "two" {
		t.Fatalf("snackbars %d", len(h.OfKind(ops.KindSnackbar)))
	}
	if h.Count(ops.OpDestroy) != 1 || h.Count(ops.OpCreate) != 1 {
		t.Fatalf("\n%s", h.Log())
	}
	if len(log) != 2 || log[0] != "one dismissed" || log[1] != "two shown" {
		t.Fatal(log)
	}
	// A late event for the replaced one is ignored.
	h.Dispatch(ops.Event{ID: first.ID, Kind: ops.EventDismissed})
	if len(log) != 2 {
		t.Fatal(log)
	}
}

// A snackbar's OnDismiss may show another: it's replaced in turn.
func TestSnackbarReplacedByOnDismiss(t *testing.T) {
	h, r := mount(t, func() ui.Node { return ui.Column(ui.Style{}) })
	ui.Snackbar(ui.SnackbarProps{Message: "one", OnDismiss: func() {
		ui.Snackbar(ui.SnackbarProps{Message: "from one"})
	}})
	ui.Snackbar(ui.SnackbarProps{Message: "two"})
	r.Flush()
	if ss := h.OfKind(ops.KindSnackbar); len(ss) != 1 || ss[0].String(ops.PropSnackbarMessage) != "two" {
		t.Fatalf("%d snackbars", len(ss))
	}
}

// dismiss dismisses it (Destroy: the host animates it out) and runs
// OnDismiss once; calling it again does nothing.
func TestSnackbarDismissFunc(t *testing.T) {
	dismissed := 0
	h, r := mount(t, func() ui.Node { return ui.Column(ui.Style{}) })
	dismiss := ui.Snackbar(ui.SnackbarProps{Message: "Uploading", Duration: ui.SnackbarIndefinite, OnDismiss: func() { dismissed++ }})
	r.Flush()
	if h.Snackbar().Int(ops.PropSnackbarDuration) != int64(ops.SnackbarDurationIndefinite) {
		t.Fatal("duration")
	}
	h.Reset()
	dismiss()
	dismiss()
	r.Flush()
	if dismissed != 1 || h.Count(ops.OpDestroy) != 1 || h.Snackbar() != nil {
		t.Fatalf("dismissed %d\n%s", dismissed, h.Log())
	}
	// A later one isn't dismissed by the old func.
	ui.Snackbar(ui.SnackbarProps{Message: "Again"})
	r.Flush()
	dismiss()
	r.Flush()
	if h.Snackbar() == nil {
		t.Fatal("dismissed by an old func")
	}
}

// The action's title follows its accessor while the snackbar shows, and
// nothing once it's gone.
func TestSnackbarActionTitleFollows(t *testing.T) {
	title := signal.New("Undo")
	h, r := mount(t, func() ui.Node { return ui.Column(ui.Style{}) })
	dismiss := ui.Snackbar(ui.SnackbarProps{Message: "Archived", Action: &ui.Action{Title: title}})
	r.Flush()
	s := h.Snackbar()
	title.Set("Rückgängig")
	r.Flush()
	if s.String(ops.PropSnackbarAction) != "Rückgängig" {
		t.Fatal(s.String(ops.PropSnackbarAction))
	}
	h.Reset()
	title.Set("Rückgängig")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("\n%s", h.Log())
	}
	dismiss()
	r.Flush()
	h.Reset()
	title.Set("Undo")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("followed after it was gone\n%s", h.Log())
	}
}

// On a desktop it goes on the window of the view whose event shows it.
func TestSnackbarWindow(t *testing.T) {
	open := signal.New(true)
	dismissed := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), OnPress: func() {
				ui.Snackbar(ui.SnackbarProps{Message: "Saved", OnDismiss: func() { dismissed++ }})
			}}),
			ui.Window(ui.WindowProps{Open: open, Title: ui.Val("Inspector"), Content: func() ui.Node {
				return ui.Button(ui.ButtonProps{Title: ui.Val("Copy"), OnPress: func() {
					ui.Snackbar(ui.SnackbarProps{Message: "Copied"})
				}})
			}}),
		)
	})
	h.Tap(h.One(ops.KindButton, "Copy"))
	w := h.PresentedOn(h.Snackbar())
	if w == nil || w.Kind != ops.KindWindow {
		t.Fatalf("on %v", w)
	}
	// One on the main window leaves the Window's showing: one per window.
	h.Tap(h.One(ops.KindButton, "Save"))
	if n := len(h.OfKind(ops.KindSnackbar)); n != 2 {
		t.Fatalf("%d snackbars, want the Window's and the main window's", n)
	}
	// Another on the Window replaces only the Window's.
	h.Tap(h.One(ops.KindButton, "Copy"))
	if n := len(h.OfKind(ops.KindSnackbar)); n != 2 || dismissed != 0 {
		t.Fatalf("%d snackbars, main one dismissed %d times", n, dismissed)
	}
}

// Disposing the root takes a snackbar showing down, without its
// OnDismiss, and leaves nothing behind.
func TestSnackbarDispose(t *testing.T) {
	h := fake.New()
	dismissed := 0
	title := signal.New("Undo")
	r := ui.Mount(h, func() ui.Node { return ui.Column(ui.Style{}) })
	dismiss := ui.Snackbar(ui.SnackbarProps{Message: "m", Action: &ui.Action{Title: title}, OnDismiss: func() { dismissed++ }})
	r.Flush()
	r.Dispose()
	if len(h.Views) != 0 || dismissed != 0 {
		t.Fatalf("%d views left, dismissed %d", len(h.Views), dismissed)
	}
	dismiss()
	title.Set("x")
	if dismissed != 0 {
		t.Fatal("dismissed after disposal")
	}
}

// Showing and dismissing snackbars repeatedly leaves no views, effects or
// handlers behind: each one's work is the same.
func TestSnackbarNoLeaks(t *testing.T) {
	title := signal.New("Undo")
	h, r := mount(t, func() ui.Node { return ui.Column(ui.Style{}) })
	views := len(h.Views)
	round := func() int {
		h.Reset()
		dismiss := ui.Snackbar(ui.SnackbarProps{Message: "m", Action: &ui.Action{Title: title}})
		r.Flush()
		h.TimeOut(h.Snackbar())
		ui.Snackbar(ui.SnackbarProps{Message: "m", Action: &ui.Action{Title: title}})
		r.Flush()
		dismiss() // the old one's: nothing
		h.PressSnackbarAction(h.Snackbar())
		title.Set(title.Get() + "!") // gone: nothing follows it
		r.Flush()
		return len(h.Ops)
	}
	first := round()
	for range 5 {
		if n := round(); n != first {
			t.Fatalf("%d ops, the first round %d", n, first)
		}
	}
	if len(h.Views) != views {
		t.Fatalf("%d views, %d before", len(h.Views), views)
	}
}
