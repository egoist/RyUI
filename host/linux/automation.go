//go:build linux && !android

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// Debug automation, as on macOS and Windows: ASUKA_SCRIPT runs steps
// (tap:<text>, type:<text>, submit, back, escape, scroll:<y>,
// resize:<w>,<h>, key:<shortcut>, menu:<item title>, tab:<title>,
// close, wait:<d>, snap:<file>), then ASUKA_SNAPSHOT=file.png writes the window as a PNG
// (ASUKA_DUMP=1: its view tree beside it) and quits.

func (h *Host) automation() {
	h.commands()
	script := os.Getenv("ASUKA_SCRIPT")
	snap := os.Getenv("ASUKA_SNAPSHOT")
	if script == "" && snap == "" {
		return
	}
	var steps []string
	if script != "" {
		steps = strings.Split(script, ";")
	}
	go func() {
		time.Sleep(1200 * time.Millisecond) // the window shows, images load
		for _, s := range steps {
			s = strings.TrimSpace(s)
			if d, ok := strings.CutPrefix(s, "wait:"); ok {
				if dur, err := time.ParseDuration(d); err == nil {
					time.Sleep(dur)
				}
				continue
			}
			done := make(chan struct{})
			h.Post(func() {
				defer close(done)
				if err := h.step(s); err != nil {
					fmt.Fprintln(os.Stderr, "asuka: script:", s+":", err)
				}
			})
			<-done
			time.Sleep(400 * time.Millisecond) // animations settle
		}
		if snap == "" {
			return
		}
		done := make(chan struct{})
		h.Post(func() {
			h.snapshot(snap)
			if os.Getenv("ASUKA_DUMP") != "" {
				os.WriteFile(snap+".txt", []byte(h.dump()), 0o644)
			}
			close(done)
		})
		<-done
		if os.Getenv("ASUKA_SNAPSHOT_STAY") == "" {
			os.Exit(0)
		}
	}()
}

func (h *Host) step(s string) error {
	verb, arg, _ := strings.Cut(s, ":")
	switch verb {
	case "tap":
		n := h.findText(arg)
		if n == nil {
			// A header bar's button, a sidebar's row, a dialog's button: GTK's
			// own widgets, by their label or tooltip.
			if h.tapWidget(arg) {
				return nil
			}
			return fmt.Errorf("nothing shows %q", arg)
		}
		h.tap(n)
	case "key":
		return h.key(arg)
	case "type":
		n := h.focusedField()
		if n == nil {
			return fmt.Errorf("no field has the focus")
		}
		h.insertText(n, arg)
	case "submit":
		if n := h.focusedField(); n != nil {
			h.emit(n, ops.Event{Kind: ops.EventSubmit})
		}
	case "back":
		var c chrome
		w := h.activeWin
		if w == nil {
			w = h.main
		}
		h.find(w.root, &c)
		if !h.goBack(c.stack) {
			return fmt.Errorf("no screen to go back from")
		}
	case "escape":
		h.escape()
	case "scroll":
		y, _ := strconv.ParseFloat(arg, 64)
		for _, n := range h.nodes {
			if n.widget == 0 || !WidgetGetMapped(n.widget) {
				continue
			}
			if n.scroll != nil {
				h.scrollTo(n, 0, y, false)
				break
			}
			if n.kind == ops.KindList {
				h.listScrollToOffset(n, 0, y, false)
				break
			}
		}
	case "resize":
		ws, hs, _ := strings.Cut(arg, ",")
		width, _ := strconv.Atoi(ws)
		height, _ := strconv.Atoi(hs)
		WindowSetDefaultSize(h.main.window, int32(width), int32(height)+headerHeight)
	case "menu":
		return h.chooseMenu(arg)
	case "close":
		w := h.activeWin
		if w == nil {
			w = h.main
		}
		WindowClose(w.window)
	case "snap":
		h.snapshot(arg)
	case "tab":
		return h.chooseTab(arg)
	default:
		return fmt.Errorf("unknown step")
	}
	return nil
}

// findText is the node on screen that shows text: a text, a button's
// title, a screen's bar button, a field's placeholder.
func (h *Host) findText(text string) *node {
	var found *node
	for _, n := range h.nodes {
		if n.widget == 0 || !WidgetGetMapped(n.widget) {
			continue
		}
		if n.text == text || n.title == text && n.kind == ops.KindButton || n.placeholder == text || n.a11yLabel == text {
			if found == nil || n.id > found.id {
				found = n
			}
		}
	}
	return found
}

// tap presses the node, or the nearest one around it that presses.
func (h *Host) tap(n *node) {
	for p := n; p != nil; p = p.parent {
		switch {
		case p.kind == ops.KindButton:
			WidgetActivate(p.inner)
			return
		case p.kind == ops.KindTextField:
			h.focusField(p)
			return
		case p.kind == ops.KindToggle:
			WidgetActivate(p.inner)
			return
		case p.listening(ops.EventPress) || p.menu != nil:
			h.press(p)
			return
		}
	}
}

func (h *Host) focusedField() *node {
	for _, w := range h.wins {
		f := WindowGetFocus(w.window)
		for f != 0 {
			for _, n := range h.nodes {
				if n.kind == ops.KindTextField && (n.inner == f || n.field != nil && (n.field.editable == f || n.field.view == f)) {
					return n
				}
			}
			f = WidgetGetParent(f)
		}
	}
	return nil
}

// escape closes the top dialog, or goes back.
func (h *Host) escape() {
	for _, n := range h.nodes {
		if n.kind == ops.KindSheet && n.nav.dialog != 0 && !n.nav.gone {
			AdwDialogClose(n.nav.dialog)
			return
		}
		if n.kind == ops.KindAlert && n.nav.dialog != 0 && !n.nav.gone {
			AdwDialogClose(n.nav.dialog)
			return
		}
	}
}

// chooseMenu activates the app menu's item with a title.
func (h *Host) chooseMenu(title string) error {
	for _, name := range h.appActions {
		_ = name
	}
	return h.activateAppItem(title)
}

// snapshot writes the window (its header bar and content) as a PNG.
func (h *Host) snapshot(path string) {
	w := h.activeWin
	if w == nil {
		w = h.main
	}
	if strings.Contains(path, ".window") || w == nil {
		w = h.main
	}
	h.snapshotWindow(w.window, path)
	// Windows of the app's, and dialogs over them.
	i := 0
	for _, x := range h.wins {
		if x == h.main {
			continue
		}
		i++
		suffix := ".window.png"
		if i > 1 {
			suffix = fmt.Sprintf(".window%d.png", i)
		}
		h.snapshotWindow(x.window, strings.TrimSuffix(path, ".png")+suffix)
	}
}

func (h *Host) snapshotWindow(window Ptr, path string) {
	paint := WidgetPaintableNew(window)
	defer ObjectUnref(paint)
	width := Call(FnWidgetGetWidth, window)
	height := Call(FnWidgetGetHeight, window)
	s := SnapshotNew()
	PaintableSnapshot(paint, s, float64(width), float64(height))
	node := SnapshotFreeToNode(s)
	if node == 0 {
		return
	}
	defer RenderNodeUnref(node)
	r := NativeGetRenderer(window)
	vp := Rect{W: float32(width), H: float32(height)}
	t := RendererRenderTexture(r, node, &vp)
	if t == 0 {
		return
	}
	TextureSaveToPNG(t, path)
	ObjectUnref(t)
}

// dump describes the view tree: each node's kind, frame and what it
// shows.
func (h *Host) dump() string {
	var b strings.Builder
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		b.WriteString(strings.Repeat("  ", depth))
		fmt.Fprintf(&b, "%d %s [%.0f,%.0f %.0fx%.0f]", n.id, n.kind, n.frame[0], n.frame[1], n.frame[2], n.frame[3])
		if n.hidden {
			b.WriteString(" hidden")
		}
		if n.widget != 0 && !WidgetGetMapped(n.widget) {
			b.WriteString(" unmapped")
		}
		if n.rtl {
			b.WriteString(" rtl")
		}
		if n.text != "" {
			fmt.Fprintf(&b, " %q", n.text)
		}
		if n.title != "" {
			fmt.Fprintf(&b, " title=%q", n.title)
		}
		b.WriteByte('\n')
		for _, c := range n.children {
			walk(c, depth+1)
		}
		h.dumpMore(n, depth, &b, walk)
	}
	if h.root != nil {
		walk(h.root, 0)
	}
	for _, n := range h.nodes {
		if n.parent == nil && n != h.root && (n.kind == ops.KindSheet || n.kind == ops.KindWindow) {
			walk(n, 0)
		}
	}
	return b.String()
}

// dumpHooks add what an area shows (a list's rows).
var dumpHooks []func(h *Host, n *node, depth int, b *strings.Builder, walk func(*node, int))

func (h *Host) dumpMore(n *node, depth int, b *strings.Builder, walk func(*node, int)) {
	for _, f := range dumpHooks {
		f(h, n, depth, b, walk)
	}
}

// commands drives the app from outside while it runs, as the iOS and
// Android debug hosts' automation does (the e2e tests): with
// ASUKA_COMMANDS=dir, commands written to dir/asuka-commands.txt, one a
// line, are run and the file removed: tap <text>, type <text> (in the
// focused field, else the first), submit, back, escape, tab <title>,
// scroll <y>, and dump, which writes what the screen shows (each text,
// field's text, button's title or accessibility label, a line each, in
// creation order) to dir/asuka-dump.txt.
func (h *Host) commands() {
	dir := os.Getenv("ASUKA_COMMANDS")
	if dir == "" {
		return
	}
	file := filepath.Join(dir, "asuka-commands.txt")
	go func() {
		for {
			time.Sleep(50 * time.Millisecond)
			b, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			os.Remove(file)
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				verb, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
				done := make(chan struct{})
				h.Post(func() {
					defer close(done)
					var err error
					switch verb {
					case "dump":
						err = writeFileAtomic(filepath.Join(dir, "asuka-dump.txt"), h.screenTexts())
					case "type":
						if h.focusedField() == nil {
							if f := h.firstField(); f != nil {
								h.focusField(f)
							}
						}
						err = h.step("type:" + arg)
					default:
						err = h.step(verb + ":" + arg)
					}
					if err != nil {
						fmt.Fprintln(os.Stderr, "asuka: automation:", line+":", err)
					}
				})
				<-done
				if verb != "dump" {
					// As the script's steps: a button takes no second
					// activation while it shows the first.
					time.Sleep(400 * time.Millisecond)
				}
			}
		}
	}()
}

// firstField is the first text field on screen.
func (h *Host) firstField() *node {
	var found *node
	for _, n := range h.nodes {
		if n.kind == ops.KindTextField && n.widget != 0 && WidgetGetMapped(n.widget) && (found == nil || n.id < found.id) {
			found = n
		}
	}
	return found
}

// chooseTab selects the shown tab view's tab with a title, as clicking
// it in the switcher does.
func (h *Host) chooseTab(title string) error {
	for _, n := range h.nodes {
		if n.kind != ops.KindTabs || n.nav == nil || n.widget == 0 || !WidgetGetMapped(n.widget) {
			continue
		}
		for _, c := range n.children {
			if c.title == title && c.widget != 0 {
				AdwViewStackSetVisibleChild(n.nav.view, c.widget)
				return nil
			}
		}
	}
	return fmt.Errorf("no tab %q", title)
}

// screenTexts is what the screen shows: each mapped node's text (a
// field's too), else a button's title, else its accessibility label, a
// line each, in creation order.
func (h *Host) screenTexts() []byte {
	var shown []*node
	for _, n := range h.nodes {
		if n.widget == 0 || n.hidden || !WidgetGetMapped(n.widget) {
			continue
		}
		if n.text != "" || n.kind == ops.KindButton && n.title != "" || n.a11yLabel != "" {
			shown = append(shown, n)
		}
	}
	sort.Slice(shown, func(i, j int) bool { return shown[i].id < shown[j].id })
	var b strings.Builder
	for _, n := range shown {
		t := n.text
		if t == "" && n.kind == ops.KindButton {
			t = n.title
		}
		if t == "" {
			t = n.a11yLabel
		}
		b.WriteString(strings.ReplaceAll(t, "\n", " "))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// writeFileAtomic writes a file whole, then moves it in place: a reader
// never sees half of it.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".part"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// tapWidget activates the GTK widget in a window of the app's (or a
// dialog over one) whose label or tooltip is text: a button, a list
// box's row, a check button, the nearest of them around a label.
func (h *Host) tapWidget(text string) bool {
	tops := WindowGetToplevels() // a GListModel of the toplevels (not ours to unref)
	var found []Ptr
	for i := uint32(0); i < ListModelGetNItems(tops); i++ {
		w := ListModelGetItem(tops, i)
		if w != 0 && WidgetGetMapped(w) {
			found = findWidgets(w, text, found)
		}
		if w != 0 {
			ObjectUnref(w)
		}
	}
	button := Call(Sym("gtk_button_get_type"))
	row := Call(Sym("gtk_list_box_row_get_type"))
	others := []Ptr{Call(Sym("gtk_check_button_get_type")), Call(Sym("gtk_switch_get_type"))}
	for _, f := range found {
		for w := f; w != 0; w = WidgetGetParent(w) {
			switch {
			case TypeCheckInstanceIsA(w, button):
				s := CString("clicked")
				Call(Sym("g_signal_emit_by_name"), w, Addr(s))
				return true
			case TypeCheckInstanceIsA(w, row):
				if Call(Sym("gtk_list_box_row_get_activatable"), w) == 0 {
					w = 0 // a section's title: not this one
					break
				}
				WidgetActivate(w)
				return true
			}
			if w == 0 {
				break
			}
			for _, t := range others {
				if TypeCheckInstanceIsA(w, t) {
					WidgetActivate(w)
					return true
				}
			}
		}
	}
	return false
}

// findWidgets appends the mapped widgets under w labeled text (a label's
// text, a tooltip), in the order they're drawn.
func findWidgets(w Ptr, text string, out []Ptr) []Ptr {
	if !WidgetGetMapped(w) {
		return out
	}
	if TypeCheckInstanceIsA(w, Call(Sym("gtk_label_get_type"))) && GoString(Call(Sym("gtk_label_get_text"), w)) == text {
		out = append(out, w)
	} else if t := Call(Sym("gtk_widget_get_tooltip_text"), w); t != 0 && GoString(t) == text {
		out = append(out, w)
	}
	for c := WidgetGetFirstChild(w); c != 0; c = WidgetGetNextSibling(c) {
		out = findWidgets(c, text, out)
	}
	return out
}

// key presses a shortcut (ctrl+shift+k, f5, alt+left): a view's Keys
// that have it, else an action of the app's that has it as its
// accelerator (the app's menus), else the key itself through xdotool, on
// X11.
func (h *Host) key(shortcut string) error {
	mods, key := uint8(0), shortcut
	for {
		m, rest, ok := strings.Cut(key, "+")
		if !ok || rest == "" {
			break
		}
		switch strings.ToLower(m) {
		case "ctrl", "control", "cmd", "primary":
			mods |= 1
		case "shift":
			mods |= 2
		case "alt", "option":
			mods |= 4
		}
		key = rest
	}
	// A view's Keys first, as a key from the keyboard (keys.go).
	if h.keyToNodes(strings.ToLower(key), mods) {
		return nil
	}
	if accel := accelerator(strings.ToLower(key), mods); accel != "" && h.app != 0 {
		list := ApplicationGetActionsForAccel(h.app, accel)
		if list != 0 {
			first := *At[Ptr](list)
			name := GoString(first)
			StrfreeV(list)
			if name != "" {
				_, action, _ := strings.Cut(name, ".")
				ActionGroupActivateAction(h.app, action, 0)
				return nil
			}
		}
	}
	return xdotoolKey(shortcut)
}
