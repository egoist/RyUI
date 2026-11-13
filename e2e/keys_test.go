//go:build e2e

package e2e

import (
	"encoding/json"
	"runtime"
	"testing"
)

// TestKeysWeb runs e2e/testdata/keys in headless Chrome, its keys
// pressed as a keyboard presses them (DevTools' Input.dispatchKeyEvent,
// trusted events, with the browser's own default actions):
//
//   - in a search field, Up, Down and Enter go to the view around it
//     (a selection moves, a result opens): the field's caret and text
//     don't move, and its OnSubmit doesn't run; a key no view has (Right)
//     goes on to the field, and its own Escape comes first;
//   - the window's keys (UseKeys) take ⌘S (Ctrl+S) from the field, and a
//     Space, held, with nothing focused;
//   - a panel's keys end at its Window: its UseKeys take Escape (it
//     stays open: the declared Escape comes before the host closing it),
//     and a Space typed in its field is the field's, not the main
//     window's.
func TestKeysWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	pageURL := webServer(t, "e2e/testdata/keys")
	c := launchCDP(t, browser)
	var created struct{ TargetID string }
	if err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"}, &created, ""); err != nil {
		t.Fatal(err)
	}
	page := c.attach(t, created.TargetID)
	var res json.RawMessage
	if err := c.call(page, "Page.navigate", map[string]any{"url": pageURL}, &res, ""); err != nil {
		t.Fatal(err)
	}
	s := &webScreen{t: t, url: pageURL}
	showsLine(t, s, "status: ready")

	// DevTools' modifiers: 2 Ctrl, 4 Meta (⌘), 8 Shift.
	primary := 2
	if runtime.GOOS == "darwin" {
		primary = 4
	}
	type key struct {
		key, code string
		vk        int
		text      string
		mods      int
		repeat    bool
		command   string // a Mac's editing command (DevTools' commands): what the key does in a field
	}
	press := func(k key) {
		t.Helper()
		down := map[string]any{"type": "rawKeyDown", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "modifiers": k.mods, "autoRepeat": k.repeat}
		if k.text != "" {
			down["type"] = "keyDown"
			down["text"] = k.text
			down["unmodifiedText"] = k.text
		}
		if k.command != "" && runtime.GOOS == "darwin" {
			down["commands"] = []string{k.command}
		}
		up := map[string]any{"type": "keyUp", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "modifiers": k.mods}
		for _, p := range []map[string]any{down, up} {
			var r json.RawMessage
			if err := c.call(page, "Input.dispatchKeyEvent", p, &r, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	arrowDown := key{key: "ArrowDown", code: "ArrowDown", vk: 40, command: "moveDown"}
	arrowUp := key{key: "ArrowUp", code: "ArrowUp", vk: 38, command: "moveUp"}
	arrowRight := key{key: "ArrowRight", code: "ArrowRight", vk: 39, command: "moveRight"}
	enter := key{key: "Enter", code: "Enter", vk: 13, text: "\r", command: "insertNewline"}
	escape := key{key: "Escape", code: "Escape", vk: 27, command: "cancelOperation"}
	space := key{key: " ", code: "Space", vk: 32, text: " "}
	save := key{key: "s", code: "KeyS", vk: 83, mods: primary}

	// The search field, with text and its caret after the first letter.
	const search = `document.querySelector("input.asuka-field[placeholder=Search]")`
	c.eval(t, page, search+`.focus(), "ok"`)
	if err := c.call(page, "Input.insertText", map[string]any{"text": "abc"}, &res, ""); err != nil {
		t.Fatal(err)
	}
	c.eval(t, page, search+`.setSelectionRange(1, 1), "ok"`)
	field := func() string {
		return c.eval(t, page, `(() => { const f = `+search+`; return [f.value, f.selectionStart, f.selectionEnd, document.activeElement === f].join(" "); })()`)
	}
	if got := field(); got != "abc 1 1 true" {
		t.Fatalf("the field: %q", got)
	}

	// Down, Down, Up: the selection moves, the caret doesn't.
	showsLine(t, s, "selected: apple")
	press(arrowDown)
	showsLine(t, s, "selected: banana")
	press(arrowDown)
	showsLine(t, s, "selected: cherry")
	press(arrowUp)
	showsLine(t, s, "selected: banana")
	if got := field(); got != "abc 1 1 true" {
		t.Fatalf("Up and Down moved the field's caret: %q", got)
	}
	// Enter opens the selection; the field's submit doesn't come.
	press(enter)
	showsLine(t, s, "opened: banana")
	showsLine(t, s, "submitted: 0")
	// Held: each repeat moves it.
	press(key{key: "ArrowDown", code: "ArrowDown", vk: 40, repeat: true, command: "moveDown"})
	showsLine(t, s, "selected: cherry")
	// A key no view has is the field's: its caret moves.
	press(arrowRight)
	if got := field(); got != "abc 2 2 true" {
		t.Fatalf("Right in the field: %q", got)
	}
	// The field's own Escape first; the field keeps the keyboard.
	press(escape)
	showsLine(t, s, "field escape: 1")
	showsLine(t, s, "window escape: 0")
	if got := field(); got != "abc 2 2 true" {
		t.Fatalf("after Escape: %q", got)
	}
	// The window's keys, from the field.
	press(save)
	showsLine(t, s, "window save: 1")
	if got := field(); got != "abc 2 2 true" {
		t.Fatalf("after a shortcut: %q", got)
	}

	// Nothing focused: the window's keys first; held, each repeat.
	c.eval(t, page, `document.activeElement.blur(), String(document.activeElement === document.body)`)
	press(space)
	showsLine(t, s, "window space: 1")
	press(key{key: " ", code: "Space", vk: 32, text: " ", repeat: true})
	showsLine(t, s, "window space: 2")
	press(escape)
	showsLine(t, s, "window escape: 1")

	// The panel: its keys are its UseKeys', and go no further.
	s.tap("Open panel")
	const inPanel = `document.querySelector(".asuka-window input.asuka-field")`
	until(t, func() bool { return c.eval(t, page, `String(!!`+inPanel+`)`) == "true" }, func() string { return "no panel" })
	c.eval(t, page, inPanel+`.focus(), "ok"`)
	press(escape)
	showsLine(t, s, "panel keys: escape")
	press(arrowDown)
	showsLine(t, s, "panel keys: escape down")
	press(space)
	until(t, func() bool { return c.eval(t, page, inPanel+`?.value`) == " " }, func() string { return "the panel's field: " + c.eval(t, page, inPanel+`?.value`) })
	showsLine(t, s, "window space: 2")
	showsLine(t, s, "window escape: 1")
	if got := c.eval(t, page, `String(!!`+inPanel+`?.isConnected && document.activeElement === `+inPanel+`)`); got != "true" {
		t.Fatalf("the panel closed on a declared Escape (or lost the keyboard): %s", got)
	}
}
