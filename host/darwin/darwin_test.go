//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// AppKit runs on the main thread: TestMain keeps it, and the tests send
// their work there.
var mainQ = make(chan func())

func TestMain(m *testing.M) {
	done := make(chan int)
	go func() { done <- m.Run() }()
	for {
		select {
		case f := <-mainQ:
			f()
		case code := <-done:
			os.Exit(code)
		}
	}
}

// onMain runs f on the main thread. A failure in f (fatal) ends f,
// not the main thread's loop, which t.Fatal's Goexit would.
func onMain(f func()) {
	ok := make(chan struct{})
	mainQ <- func() {
		defer close(ok)
		defer func() {
			if r := recover(); r != nil && r != errFatal {
				panic(r)
			}
		}()
		WithPool(f)
	}
	<-ok
}

var errFatal = new(int)

// fatal fails the test and ends the onMain func it's called in.
func fatal(t *testing.T, args ...any) {
	t.Helper()
	t.Error(args...)
	panic(errFatal)
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
	panic(errFatal)
}

// mount mounts root on a new host (a window of its own), as Run does but
// without AppKit's event loop: work the host defers runs at once.
func mount(t *testing.T, root func() ui.Node) (*Host, *ui.Root) {
	var h *Host
	var r *ui.Root
	onMain(func() {
		h = newHost()
		h.enter(func() { r = ui.Mount(h, root) })
	})
	t.Cleanup(func() {
		onMain(func() {
			h.enter(r.Dispose)
			Send(h.window, "close")
		})
	})
	return h, r
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

func text(n *node) string {
	if n.tracking != 0 {
		return GoString(Send(n.tracking, "string"))
	}
	return GoString(Send(n.view, "stringValue"))
}

func TestCounter(t *testing.T) {
	count := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Update(func(n int) int { return n + 1 }) }}),
		)
	})
	onMain(func() {
		texts, buttons := find(h, ops.KindText), find(h, ops.KindButton)
		if len(texts) != 1 || len(buttons) != 1 {
			fatalf(t, "%d texts, %d buttons", len(texts), len(buttons))
		}
		label, button := texts[0], buttons[0]
		if s := text(label); s != "Count: 0" {
			fatalf(t, "text %q", s)
		}
		// A real click, through the button's target and action.
		Send(button.view, "performClick:", 0)
		if s := text(label); s != "Count: 1" {
			fatalf(t, "after a click: %q", s)
		}
		f := MsgRect(label.view, Sel("frame"))
		if f.Origin.X != 16 || f.Origin.Y != 16 || f.Size.Width <= 0 || f.Size.Height <= 0 {
			fatalf(t, "text frame %+v", f)
		}
		if Send(label.view, "window") != h.window {
			fatal(t, "the text isn't in the window")
		}
	})
}

func TestTextWraps(t *testing.T) {
	long := "The quick brown fox jumps over the lazy dog, again and again, until the line has to wrap."
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Width: ui.Pt(120)}, ui.StaticText(long))
	})
	onMain(func() {
		f := MsgRect(find(h, ops.KindText)[0].view, Sel("frame"))
		if f.Size.Width > 120 || f.Size.Height < 40 {
			fatalf(t, "a long text in 120pt: %+v", f.Size)
		}
	})
}

func TestTextField(t *testing.T) {
	value := signal.New("")
	submitted := 0
	h, _ := mount(t, func() ui.Node {
		return ui.TextField(ui.TextFieldProps{Value: value, OnSubmit: func() { submitted++ }})
	})
	onMain(func() {
		f := find(h, ops.KindTextField)[0]
		Send(f.view, "setStringValue:", uintptr(String("hello")))
		h.textChanged(f.view)
		if value.Peek() != "hello" {
			fatalf(t, "the signal is %q", value.Peek())
		}
		h.textCommand(f.view, 0, Sel("insertNewline:"))
		if submitted != 1 {
			fatalf(t, "submitted %d times", submitted)
		}
		// The app's write reaches the field.
		h.enter(func() { ui.Post(func() { value.Set("set") }) })
		h.runPosted()
		if s := text(f); s != "set" {
			fatalf(t, "the field shows %q", s)
		}
	})
}

func TestListBindsOnlyWhatShows(t *testing.T) {
	items := make([]int, 10000)
	for i := range items {
		items[i] = i
	}
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{}, signal.Static(items), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(40)}},
					ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) })))
			})
	})
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		if len(l.shown) == 0 || len(l.shown) > 60 {
			fatalf(t, "%d rows shown of 10,000", len(l.shown))
		}
		if total := l.tops[len(l.tops)-1]; total < 10000*40-1 {
			fatalf(t, "the content is %.0f tall", total)
		}
		// Scrolled to the middle, the rows there are bound.
		h.scrollToOffset(l.n, 0, 200000, false)
		h.scrolledList(Send(l.scroll, "contentView"))
		found := false
		for r, rv := range l.shown {
			if r == 5000 {
				found = true
				if s := text(firstText(rv.cells[0].n)); s != "Row 5000" {
					fatalf(t, "row 5000 shows %q", s)
				}
			}
		}
		if !found {
			fatal(t, "row 5000 isn't shown at 200,000pt")
		}
	})
}

func TestStackPushAndBack(t *testing.T) {
	var nav *ui.Navigator
	disposed := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.StaticText("Home")
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	onMain(func() {
		h.enter(func() {
			ui.Post(func() {
				nav.Push(func() ui.Node {
					signal.OnCleanup(func() { disposed++ })
					return ui.StaticText("Detail")
				}, ui.ScreenOptions{Title: ui.Val("Detail")})
			})
		})
		h.runPosted()
		if s := GoString(Send(h.window, "title")); s != "Detail" {
			fatalf(t, "the window's title is %q", s)
		}
		if !h.main.chrome.back || h.main.chrome.toolbar == 0 {
			fatal(t, "no back button")
		}
		// The toolbar's back button: Popped, and the core disposes the screen.
		h.back(h.main)
		if disposed != 1 {
			fatalf(t, "the popped screen's owner was disposed %d times", disposed)
		}
		if s := GoString(Send(h.window, "title")); s != "Home" {
			fatalf(t, "after back, the title is %q", s)
		}
		if nav.Depth() != 1 {
			fatalf(t, "depth %d", nav.Depth())
		}
	})
}

func TestDisposeDestroysViews(t *testing.T) {
	var h *Host
	var r *ui.Root
	onMain(func() {
		h = newHost()
		h.enter(func() {
			r = ui.Mount(h, func() ui.Node {
				return ui.Column(ui.Style{}, ui.StaticText("a"), ui.Button(ui.ButtonProps{Title: ui.Val("b")}))
			})
		})
		if len(h.nodes) == 0 {
			fatal(t, "no views")
		}
		h.enter(r.Dispose)
		if len(h.nodes) != 0 || len(h.byView) != 0 {
			fatalf(t, "%d nodes, %d views left", len(h.nodes), len(h.byView))
		}
		Send(h.window, "close")
	})
}

func firstText(n *node) *node {
	if n.kind == ops.KindText {
		return n
	}
	for _, c := range n.children {
		if t := firstText(c); t != nil {
			return t
		}
	}
	return nil
}

func TestModules(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		call := func(module, m string, args []byte) ([]byte, error) {
			var out []byte
			var err error
			h.module(module)[m](args, func(b []byte, e error) { out, err = b, e })
			return out, err
		}
		b, err := call("device", "info", nil)
		if err != nil || !strings.Contains(string(b), `"system":"macOS"`) {
			fatalf(t, "device.info: %s, %v", b, err)
		}
		key := fmt.Sprintf("nui-test-%d", time.Now().UnixNano())
		if b, err := call("secureStore", "get", []byte(key)); err != nil || len(b) != 1 || b[0] != 0 {
			fatalf(t, "a missing key: %v, %v", b, err)
		}
		args := binary.LittleEndian.AppendUint32(nil, uint32(len(key)))
		args = append(append(args, key...), "secret"...)
		if _, err := call("secureStore", "set", args); err != nil {
			fatal(t, err)
		}
		if b, err := call("secureStore", "get", []byte(key)); err != nil || string(b) != "\x01secret" {
			fatalf(t, "get after set: %q, %v", b, err)
		}
		if _, err := call("secureStore", "delete", []byte(key)); err != nil {
			fatal(t, err)
		}
		if b, _ := call("secureStore", "get", []byte(key)); len(b) != 1 || b[0] != 0 {
			fatalf(t, "after delete: %q", b)
		}
	})
}

func TestAccessibility(t *testing.T) {
	did, by := "", 0
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{
			Label:    "Volume",
			Actions:  []ui.A11yAction{{Name: "Mute", Do: func() { did = "Mute" }}},
			OnAdjust: func(n int) { by += n },
		}, ui.StaticText("x"))
	})
	onMain(func() {
		var v *node
		for _, n := range find(h, ops.KindView) {
			if n.a11yLabel == "Volume" {
				v = n
			}
		}
		if v == nil {
			fatal(t, "no labeled view")
		}
		if s := GoString(Send(v.view, "accessibilityLabel")); s != "Volume" {
			fatalf(t, "label %q", s)
		}
		actions := ArrayItems(Send(v.view, "accessibilityCustomActions"))
		if len(actions) != 1 || GoString(Send(actions[0], "name")) != "Mute" {
			fatalf(t, "%d actions", len(actions))
		}
		// VoiceOver takes the action: its target and selector.
		Send(Send(actions[0], "target"), "a11yAction:", uintptr(actions[0]))
		if did != "Mute" {
			fatal(t, "the action didn't run")
		}
		Send(v.view, "accessibilityPerformIncrement")
		Send(v.view, "accessibilityPerformIncrement")
		Send(v.view, "accessibilityPerformDecrement")
		if by != 1 {
			fatalf(t, "adjusted by %d", by)
		}
	})
}

func TestTransitions(t *testing.T) {
	shown := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Show(shown, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(40), Background: ui.Red, Transition: ui.SlideUp}},
					ui.StaticText("toast"))
			}))
	})
	onMain(func() {
		toggle := func(v bool) {
			h.enter(func() { ui.Post(func() { ui.Animate(func() { shown.Set(v) }) }) })
			h.runPosted()
		}
		toggle(true)
		var toast *node
		for _, n := range find(h, ops.KindView) {
			if n.transition != nil {
				toast = n
			}
		}
		if toast == nil {
			fatal(t, "no toast")
		}
		v := Retain(toast.view)
		defer Release(v)
		if toast.entering || !toast.placed {
			fatal(t, "the toast didn't come in")
		}
		toggle(false)
		// It's gone from the core's tree, and stays on screen while it goes.
		if h.nodes[toast.id] != nil {
			fatal(t, "the toast's node is still there")
		}
		if !h.exiting[v] || Send(v, "superview") == 0 {
			fatal(t, "the toast left at once")
		}
	})
}

func TestWindow(t *testing.T) {
	open := signal.New(false)
	closed := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Title: ui.Val("Inspector"), Width: 300, Height: 200,
			OnClose: func() { closed++ },
			Content: func() ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("width ", ui.UseEnvironment().Get().Width) }))
			}})
	})
	onMain(func() {
		h.enter(func() { ui.Post(func() { open.Set(true) }) })
		h.runPosted()
		if len(h.wins) != 1 {
			fatalf(t, "%d windows", len(h.wins))
		}
		var w *win
		for _, x := range h.wins {
			w = x
		}
		if s := GoString(Send(w.window, "title")); s != "Inspector" {
			fatalf(t, "title %q", s)
		}
		if b := MsgRect(w.content, Sel("bounds")); b.Size.Width != 300 || b.Size.Height != 200 {
			fatalf(t, "content %+v", b.Size)
		}
		// Its content lays out at the window's size.
		if s := text(firstText(w.root)); s != "width 300" {
			fatalf(t, "the content says %q", s)
		}
		// The user closes it: the core closes it.
		if SendBool(h.target, "windowShouldClose:", uintptr(w.window)) {
			fatal(t, "closed without the core")
		}
		if open.Peek() || closed != 1 || len(h.wins) != 0 {
			fatalf(t, "open %v closed %d windows %d", open.Peek(), closed, len(h.wins))
		}
	})
}

func TestMenuBar(t *testing.T) {
	saved := 0
	h, _ := mount(t, func() ui.Node {
		return ui.MenuBar(signal.Static([]ui.MenuBarMenu{
			{Title: "File", Items: []ui.MenuItem{{Title: "Save", Shortcut: ui.Cmd("s"), OnPress: func() { saved++ }}}},
			{Title: "Tools", Items: []ui.MenuItem{{Title: "Lint", Disabled: true}}},
		}))
	})
	onMain(func() {
		bar := Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu")
		find := func(menu, title string) ID {
			m := Send(Send(bar, "itemWithTitle:", uintptr(String(menu))), "submenu")
			if m == 0 {
				fatalf(t, "no %s menu", menu)
			}
			return Send(m, "itemWithTitle:", uintptr(String(title)))
		}
		save := find("File", "Save")
		if save == 0 || GoString(Send(save, "keyEquivalent")) != "s" || SendInt(save, "keyEquivalentModifierMask") != modCommand {
			fatal(t, "no Save with ⌘S in File")
		}
		if find("File", "Close Window") == 0 {
			fatal(t, "File lost Close Window")
		}
		// Clicked, as AppKit sends it: the item's OnPress runs.
		Send(Send(save, "target"), "appMenuItem:", uintptr(save))
		if saved != 1 {
			fatal(t, "Save didn't run")
		}
		lint := find("Tools", "Lint")
		if lint == 0 || h.validateMenuItem(lint) {
			fatal(t, "Lint isn't there and off")
		}
	})
}

func TestWindowOptions(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Title: "Notes", Width: 420, Height: 500, MinWidth: 400, MinHeight: 300})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	// No size the user left the window at: the options' is the window's.
	onMain(func() {
		Send(Send(Class("NSUserDefaults"), "standardUserDefaults"), "removeObjectForKey:", uintptr(String("NSWindow Frame nui "+appName())))
	})
	h, _ := mount(t, func() ui.Node {
		return ui.Text(signal.Func(func() string { return fmt.Sprint("width ", ui.UseEnvironment().Get().Width) }))
	})
	onMain(func() {
		// The core lays out at the size the options gave the window (unless
		// the user's is remembered).
		if s := text(find(h, ops.KindText)[0]); s != "width 420" {
			fatalf(t, "the root laid out at %q", s)
		}
		if s := GoString(Send(h.window, "title")); s != "Notes" {
			fatalf(t, "title %q", s)
		}
		if m := MsgSize(h.window, Sel("contentMinSize")); m.Width != 400 || m.Height != 300 {
			fatalf(t, "min %+v", m)
		}
	})
}
