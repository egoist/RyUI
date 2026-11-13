//go:build windows

package windows

import (
	"testing"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// keyLoop hands m on as the message loop does: the host's first (a
// view's Keys, the accelerators, IsDialogMessage, which dispatches what
// it doesn't take itself), else TranslateMessage (which posts a key's
// characters) and DispatchMessage.
func keyLoop(h *Host, m *Msg) {
	if h.preTranslate(m) {
		return
	}
	TranslateMessage(m)
	DispatchMessage(m)
}

// pressKey presses virtual key vk with the keyboard's focus in hwnd and
// mods (modPrimary: Ctrl, modShift, modAlt) held, repeats more times
// held down, and lets it go, as the message loop gets the messages, with
// the characters TranslateMessage posts. It reports how many of its
// presses (and repeats) a view's Keys took.
func pressKey(h *Host, hwnd, vk uintptr, mods uint8, repeats int) int {
	saved := KeyboardState()
	st := saved
	if mods&modPrimary != 0 {
		st[VK_CONTROL] = 0x80
	}
	if mods&modShift != 0 {
		st[VK_SHIFT] = 0x80
	}
	if mods&modAlt != 0 {
		st[VK_MENU] = 0x80
	}
	SetKeyboardState(st)
	defer SetKeyboardState(saved)
	down, up := uint32(WM_KEYDOWN), uint32(WM_KEYUP)
	if mods&modAlt != 0 && mods&modPrimary == 0 {
		down, up = WM_SYSKEYDOWN, WM_SYSKEYUP
	}
	lp := uintptr(1) | uintptr(ScanCode(uint32(vk)))<<16
	if mods&modAlt != 0 {
		lp |= 1 << 29
	}
	taken := 0
	for i := range repeats + 1 {
		m := Msg{Hwnd: hwnd, Message: down, WParam: vk, LParam: lp}
		if i > 0 {
			m.LParam |= 1 << 30 // an auto-repeat
		}
		if h.routeKey(&m) { // as preTranslate starts
			taken++
		} else {
			keyLoop(h, &m)
		}
		pumpKeys(h)
	}
	m := Msg{Hwnd: hwnd, Message: up, WParam: vk, LParam: lp | 1<<30 | 1<<31}
	keyLoop(h, &m)
	pumpKeys(h)
	return taken
}

// pumpKeys runs the messages the thread has now (a key's characters)
// through the message loop's steps.
func pumpKeys(h *Host) {
	var m Msg
	for PeekMessage(&m) {
		keyLoop(h, &m)
	}
}

// caret is an EDIT's selection.
func caret(ctl uintptr) (start, end int) {
	r := SendMessage(ctl, EM_GETSEL, 0, 0)
	return int(r & 0xFFFF), int(r >> 16 & 0xFFFF)
}

// A view's keys, with the focus in a field inside it: Up, Down and Left
// are the view's (the field's caret doesn't move), Enter too (the field
// doesn't submit); keys it didn't declare go on to the field (Right
// moves its caret, X types), and to the menu bar (Ctrl+S). Its Ctrl+D
// beats the menu's, and a declared Control is Ctrl. Held, each repeat
// goes to it, and letting go of a key it took (and of Alt, after Alt+G)
// is its too.
func TestKeysAroundField(t *testing.T) {
	value := signal.New("hello")
	var got []string
	submitted, menuD, saved := 0, 0, 0
	log := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{{Title: "Edit", Items: []ui.MenuItem{
				{Title: "Duplicate", Shortcut: ui.Cmd("d"), OnPress: func() { menuD++ }},
				{Title: "Save", Shortcut: ui.Cmd("s"), OnPress: func() { saved++ }},
			}}})),
			ui.View(ui.ViewProps{Keys: ui.KeyMap{
				{Key: "up"}:                  log("up"),
				{Key: "down"}:                log("down"),
				{Key: "left"}:                log("left"),
				{Key: "enter"}:               log("enter"),
				ui.Cmd("d"):                  log("ctrl d"),
				{Key: "e", Mods: ui.Control}: log("control e"),
				{Key: "g", Mods: ui.Alt}:     log("alt g"),
				{Key: "f5", Mods: ui.Shift}:  log("shift f5"),
			}}, ui.TextField(ui.TextFieldProps{Value: value, OnSubmit: func() { submitted++ }})))
	})
	onMain(t, func() {
		f := find(h, ops.KindTextField)[0]
		SendMessage(f.ctl, EM_SETSEL, 2, 2)
		for _, vk := range []uintptr{VK_UP, VK_DOWN, VK_LEFT} {
			if pressKey(h, f.ctl, vk, 0, 0) != 1 {
				fatalf("%#x wasn't taken", vk)
			}
		}
		if s, e := caret(f.ctl); s != 2 || e != 2 {
			fatalf("the caret moved to %d, %d", s, e)
		}
		if pressKey(h, f.ctl, VK_RETURN, 0, 0) != 1 || submitted != 0 {
			fatalf("Enter: submitted %d times", submitted)
		}
		// What it didn't declare: the field's.
		if pressKey(h, f.ctl, VK_RIGHT, 0, 0) != 0 {
			fatalf("Right was taken")
		}
		if s, _ := caret(f.ctl); s != 3 {
			fatalf("Right: the caret is at %d", s)
		}
		if pressKey(h, f.ctl, 'X', 0, 0) != 0 || value.Peek() != "helxlo" {
			fatalf("X typed %q", value.Peek())
		}
		// Ctrl+D, ahead of the menu's; a declared Control is Ctrl.
		if pressKey(h, f.ctl, 'D', modPrimary, 0) != 1 || menuD != 0 {
			fatalf("Ctrl+D: the menu's ran %d times", menuD)
		}
		if pressKey(h, f.ctl, 'E', modPrimary, 0) != 1 {
			fatalf("Ctrl+E wasn't taken")
		}
		if pressKey(h, f.ctl, 'S', modPrimary, 0) != 0 || saved != 1 {
			fatalf("Ctrl+S: Save ran %d times", saved)
		}
		// Exactly its modifiers.
		if pressKey(h, f.ctl, VK_UP, modShift, 0) != 0 || pressKey(h, f.ctl, 'D', modPrimary|modShift, 0) != 0 {
			fatalf("taken with other modifiers")
		}
		if pressKey(h, f.ctl, VK_F5, modShift, 0) != 1 {
			fatalf("Shift+F5 wasn't taken")
		}
		// Held: each repeat.
		got = nil
		if n := pressKey(h, f.ctl, VK_DOWN, 0, 3); n != 4 || len(got) != 4 {
			fatalf("Down held: %d taken, %q", n, got)
		}
		for vk, waits := range h.keyUps {
			if waits {
				fatalf("%#x's letting go is still waited for", vk)
			}
		}
		// Alt+G: Alt's letting go is its too, not the menu bar's.
		if pressKey(h, f.ctl, 'G', modAlt, 0) != 1 {
			fatalf("Alt+G wasn't taken")
		}
		m := Msg{Hwnd: f.ctl, Message: WM_SYSKEYUP, WParam: VK_MENU, LParam: 1<<29 | 1<<30 | 1<<31}
		if !h.routeKey(&m) {
			fatalf("Alt's letting go after Alt+G went on")
		}
		if h.routeKey(&m) {
			fatalf("Alt's letting go taken twice")
		}
		// An input method's keys are its own.
		if pressKey(h, f.ctl, VK_PROCESSKEY, 0, 0) != 0 {
			fatalf("an input method's key was taken")
		}
	})
}

// The nearest view with the key takes it: a field's own Keys, then the
// views around it, then the window's (UseKeys), which takes keys too when
// no view has the focus.
func TestKeysNearestFirst(t *testing.T) {
	var got []string
	log := func(s string) func() { return func() { got = append(got, s) } }
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: log("window escape"), {Key: "space"}: log("window space")})
		return ui.View(ui.ViewProps{Keys: ui.KeyMap{{Key: "escape"}: log("outer escape"), {Key: "tab"}: log("outer tab")}},
			ui.TextField(ui.TextFieldProps{Value: signal.New(""), Keys: ui.KeyMap{{Key: "tab"}: log("field tab")}}))
	})
	onMain(t, func() {
		f := find(h, ops.KindTextField)[0]
		pressKey(h, f.ctl, VK_TAB, 0, 0)
		pressKey(h, f.ctl, VK_ESCAPE, 0, 0)
		pressKey(h, f.ctl, VK_SPACE, 0, 0)
		pressKey(h, h.window, VK_SPACE, 0, 0)  // nothing focused: the window's
		pressKey(h, h.window, VK_ESCAPE, 0, 0) // not the field's view's
		if pressKey(h, h.window, VK_TAB, 0, 0) != 0 {
			fatalf("Tab taken with nothing focused")
		}
		want := []string{"field tab", "outer escape", "window space", "window space", "window escape"}
		if len(got) != len(want) {
			fatalf("got %q, want %q", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				fatalf("got %q, want %q", got, want)
			}
		}
	})
}

// Keys in a Window are the Window's: its views', then its UseKeys', never
// the main window's.
func TestKeysInWindow(t *testing.T) {
	open := signal.New(true)
	var got []string
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: func() { got = append(got, "main") }, {Key: "left"}: func() { got = append(got, "main left") }})
		return ui.Window(ui.WindowProps{Open: open, Content: func() ui.Node {
			ui.UseKeys(ui.KeyMap{{Key: "escape"}: func() { got = append(got, "window") }})
			return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
		}})
	})
	onMain(t, func() {
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		win := h.wins[0].nav.window
		f := find(h, ops.KindTextField)[0]
		pressKey(h, win, VK_ESCAPE, 0, 0)
		pressKey(h, f.ctl, VK_ESCAPE, 0, 0)
		pressKey(h, h.window, VK_ESCAPE, 0, 0)
		if pressKey(h, f.ctl, VK_LEFT, 0, 0) != 0 {
			fatalf("the main window's key taken in a Window")
		}
		if len(got) != 3 || got[0] != "window" || got[1] != "window" || got[2] != "main" {
			fatalf("got %q", got)
		}
	})
}

// A panel's Escape is the app's when it declares it: the panel stays.
func TestKeysEscapeInPanel(t *testing.T) {
	boot.KeepRunning = true
	defer func() { boot.KeepRunning = false }()
	ui.SetWindowOptions(ui.WindowOptions{Panel: true, Width: 400, Height: 200})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	escapes := 0
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "escape"}: func() { escapes++ }})
		return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
	})
	onMain(t, func() {
		h.showMain()
		h.runPosted()
		f := find(h, ops.KindTextField)[0]
		if pressKey(h, f.ctl, VK_ESCAPE, 0, 0) != 1 || pressKey(h, h.window, VK_ESCAPE, 0, 0) != 1 || escapes != 2 {
			fatalf("Escape: %d", escapes)
		}
		if !IsWindowVisible(h.window) || h.quitting {
			fatalf("the panel closed")
		}
	})
}

// A list's rows are in it: a key in a row's field goes to the row's view,
// then out through the list to the views around it.
func TestKeysInListRow(t *testing.T) {
	var got []string
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(300)}, Keys: ui.KeyMap{{Key: "down"}: func() { got = append(got, "outer down") }}},
			ui.List(ui.ListProps{}, signal.Static([]int{1, 2, 3}), func(i int) int { return i },
				func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
					return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(40)}, Keys: ui.KeyMap{{Key: "enter"}: func() { got = append(got, "row enter") }}},
						ui.TextField(ui.TextFieldProps{Value: signal.New("")}))
				}))
	})
	onMain(t, func() {
		fields := find(h, ops.KindTextField)
		if len(fields) == 0 {
			fatalf("no rows bound")
		}
		f := fields[0]
		if pressKey(h, f.ctl, VK_DOWN, 0, 0) != 1 || pressKey(h, f.ctl, VK_RETURN, 0, 0) != 1 {
			fatalf("got %q", got)
		}
		if len(got) != 2 || got[0] != "outer down" || got[1] != "row enter" {
			fatalf("got %q", got)
		}
	})
}

// A header's search field is its screen's: a key there goes out from the
// screen.
func TestKeysInHeaderSearch(t *testing.T) {
	downs := 0
	h, _ := mount(t, func() ui.Node {
		ui.UseKeys(ui.KeyMap{{Key: "down"}: func() { downs++ }})
		return ui.Stack(func() ui.Node { return ui.StaticText("Home") }, ui.ScreenOptions{
			Title:  ui.Val("Home"),
			Search: &ui.Search{Text: signal.New("")},
		})
	})
	onMain(t, func() {
		f := find(h, ops.KindStack)[0].nav.bar.nav.field
		if f == nil {
			fatalf("no search field")
		}
		if pressKey(h, f.ctl, VK_DOWN, 0, 0) != 1 || downs != 1 {
			fatalf("Down in the search field: %d", downs)
		}
	})
}
