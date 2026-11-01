//go:build windows && amd64

package windows

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unsafe"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/ui"
)

// viewListening is the view listening for e (the first made, if many).
func viewListening(h *Host, e ops.EventKind) *node {
	var found *node
	for _, n := range h.nodes {
		if n.kind == ops.KindView && n.listening(e) && (found == nil || n.id < found.id) {
			found = n
		}
	}
	if found == nil {
		fatalf("no view listens for %v", e)
	}
	return found
}

// showHost shows the host's window without activating it: the mouse's
// capture and the views' visibility as in an app.
func showHost(h *Host) {
	ShowWindow(h.window, SW_SHOWNOACTIVATE)
	UpdateWindow(h.window)
}

// at is a point in a view's window, as a mouse message's lParam.
func at(x, y int32) uintptr { return MakeLong(uint16(int16(x)), uint16(int16(y))) }

func box(w, ht float32) ui.Style { return ui.Style{Width: ui.Pt(w), Height: ui.Pt(ht)} }

// A view listening for Pressed reports its press of the mouse as it
// changes: down, off it (with the button down), back on, and up, which
// fires Press between the last true and false, as a tap reports them.
func TestPressed(t *testing.T) {
	var log []string
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20)}, ui.View(ui.ViewProps{
			OnPress:         func() { log = append(log, "press") },
			OnPressedChange: func(on bool) { log = append(log, map[bool]string{true: "down", false: "up"}[on]) },
			Style:           box(100, 60),
		}))
	})
	onMain(t, func() {
		showHost(h)
		v := viewListening(h, ops.EventPressed)
		SendMessage(v.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(v.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(12, 11)) // a little: still a press
		SendMessage(v.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(-1, 11)) // off it
		SendMessage(v.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(5, 5))   // back on
		SendMessage(v.hwnd, WM_LBUTTONUP, 0, at(5, 5))
		if want := []string{"down", "up", "down", "press", "up"}; !slices.Equal(log, want) {
			fatalf("events %v, want %v", log, want)
		}
		// Let go of off the view: no Press.
		log = nil
		SendMessage(v.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(v.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(500, 10))
		SendMessage(v.hwnd, WM_LBUTTONUP, 0, at(500, 10))
		if want := []string{"down", "up"}; !slices.Equal(log, want) {
			fatalf("let go of outside: %v, want %v", log, want)
		}
		// Losing the capture cancels it.
		log = nil
		SendMessage(v.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(v.hwnd, WM_CAPTURECHANGED, 0, 0)
		SendMessage(v.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(11, 10))
		SendMessage(v.hwnd, WM_LBUTTONUP, 0, at(11, 10))
		if want := []string{"down", "up"}; !slices.Equal(log, want) {
			fatalf("capture lost: %v, want %v", log, want)
		}
		if Capture() == v.hwnd {
			ReleaseCapture()
		}
	})
}

// A pan, or a long press, takes the press: Pressed false, no Press.
func TestPressedCancelled(t *testing.T) {
	var log []string
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 20},
			ui.View(ui.ViewProps{
				OnPress:         func() { log = append(log, "press") },
				OnPan:           func(g ui.Gesture) { log = append(log, "pan") },
				OnPressedChange: func(on bool) { log = append(log, map[bool]string{true: "down", false: "up"}[on]) },
				Style:           box(100, 60),
			}),
			ui.View(ui.ViewProps{
				OnPress:         func() { log = append(log, "press") },
				OnLongPress:     func() { log = append(log, "long") },
				OnPressedChange: func(on bool) { log = append(log, map[bool]string{true: "down", false: "up"}[on]) },
				Style:           box(100, 60),
			}),
		)
	})
	onMain(t, func() {
		showHost(h)
		var pan, long *node
		for _, n := range h.nodes {
			switch {
			case n.listening(ops.EventPan):
				pan = n
			case n.listening(ops.EventLongPress):
				long = n
			}
		}
		SendMessage(pan.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(pan.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(60, 10))
		SendMessage(pan.hwnd, WM_LBUTTONUP, 0, at(60, 10))
		if len(log) < 3 || log[0] != "down" || log[1] != "up" || slices.Contains(log, "press") {
			fatalf("a pan: %v", log)
		}
		log = nil
		SendMessage(long.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(long.hwnd, WM_TIMER, timerLongPress, 0)
		SendMessage(long.hwnd, WM_LBUTTONUP, 0, at(10, 10))
		if want := []string{"down", "up", "long"}; !slices.Equal(log, want) {
			fatalf("a long press: %v, want %v", log, want)
		}
	})
}

// A view with Pressed and no Press tracks the mouse's presses, but isn't
// a tab stop, nor pressed by the keyboard.
func TestPressedOnly(t *testing.T) {
	var log []bool
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{OnPressedChange: func(on bool) { log = append(log, on) }, Style: box(100, 60)})
	})
	onMain(t, func() {
		showHost(h)
		v := viewListening(h, ops.EventPressed)
		if WindowLong(v.hwnd, GWL_STYLE)&WS_TABSTOP != 0 {
			fatalf("a tab stop")
		}
		if !v.interactive() {
			fatalf("it lets the mouse through")
		}
		SendMessage(v.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(v.hwnd, WM_LBUTTONUP, 0, at(10, 10))
		SendMessage(v.hwnd, WM_KEYDOWN, VK_SPACE, 0)
		SendMessage(v.hwnd, WM_KEYUP, VK_SPACE, 0)
		if !slices.Equal(log, []bool{true, false}) {
			fatalf("events %v", log)
		}
	})
}

// Space held on a pressable view presses it, its release fires Press;
// Enter presses at once.
func TestPressedBySpace(t *testing.T) {
	var log []string
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{
			OnPress:         func() { log = append(log, "press") },
			OnPressedChange: func(on bool) { log = append(log, map[bool]string{true: "down", false: "up"}[on]) },
			Style:           box(100, 60),
		})
	})
	onMain(t, func() {
		v := viewListening(h, ops.EventPressed)
		SendMessage(v.hwnd, WM_KEYDOWN, VK_SPACE, 0)
		SendMessage(v.hwnd, WM_KEYDOWN, VK_SPACE, 1<<30) // its repeat
		if want := []string{"down"}; !slices.Equal(log, want) {
			fatalf("held: %v", log)
		}
		SendMessage(v.hwnd, WM_KEYUP, VK_SPACE, 0)
		SendMessage(v.hwnd, WM_KEYDOWN, VK_RETURN, 0)
		if want := []string{"down", "press", "up", "press"}; !slices.Equal(log, want) {
			fatalf("events %v, want %v", log, want)
		}
		// Focus going while it's held: no Press.
		log = nil
		SendMessage(v.hwnd, WM_KEYDOWN, VK_SPACE, 0)
		SendMessage(v.hwnd, WM_KILLFOCUS, 0, 0)
		SendMessage(v.hwnd, WM_KEYUP, VK_SPACE, 0)
		if want := []string{"down", "up"}; !slices.Equal(log, want) {
			fatalf("focus lost: %v, want %v", log, want)
		}
	})
}

// ownPixels paints n alone (not what's behind it) into a transparent
// bitmap.
func ownPixels(h *Host, n *node) []byte {
	r := ClientRect(n.hwnd)
	dc := CreateCompatibleDC(0)
	bmp, px := DIB(r.W(), r.H())
	old := SelectObject(dc, bmp)
	h.paintOwn(n, n.hwnd, dc, r)
	out := slices.Clone(px)
	SelectObject(dc, old)
	DeleteObject(bmp)
	DeleteDC(dc)
	return out
}

// A pressable view shows the host's pressed fill; one that reports its
// pressed or hover state draws its own, and gets none.
func TestOwnStatesHaveNoFill(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Gap: 10},
			ui.View(ui.ViewProps{OnPress: func() {}, Style: box(40, 40)}),
			ui.View(ui.ViewProps{OnPress: func() {}, OnPressedChange: func(bool) {}, Style: box(40, 40)}),
			ui.View(ui.ViewProps{OnPress: func() {}, OnHoverChange: func(bool) {}, Style: box(40, 40)}),
		)
	})
	onMain(t, func() {
		var views []*node
		for _, n := range h.nodes {
			if n.kind == ops.KindView && n.listening(ops.EventPress) {
				views = append(views, n)
			}
		}
		if len(views) != 3 {
			fatalf("%d pressable views", len(views))
		}
		for _, v := range views {
			v.pressed, v.hot = true, true
			px := ownPixels(h, v)
			mid := (len(px)/8)*4 + 3 // a pixel's alpha, in the middle
			if filled := px[mid] != 0; filled == v.drawsStates() {
				fatalf("a view listening for Pressed %v, Hover %v: filled %v", v.listening(ops.EventPressed), v.listening(ops.EventHover), filled)
			}
			v.pressed, v.hot = false, false
		}
	})
}

// Hover follows the pointer by the views' rectangles: over a view and
// those it's in (views that let the mouse through too), and leaving them.
func TestHover(t *testing.T) {
	var log []string
	hover := func(name string) func(bool) {
		return func(on bool) { log = append(log, name+map[bool]string{true: "+", false: "-"}[on]) }
	}
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20)},
			ui.View(ui.ViewProps{OnHoverChange: hover("outer"), Style: ui.Style{Padding: ui.All(20), Width: ui.Pt(200)}},
				ui.View(ui.ViewProps{OnHoverChange: hover("inner"), Style: box(60, 60)}),
			),
		)
	})
	onMain(t, func() {
		showHost(h)
		var outer, inner *node
		for _, n := range h.nodes {
			if n.listening(ops.EventHover) {
				if inner == nil {
					inner = n
				} else {
					outer = n
				}
			}
		}
		if IsChild(inner.hwnd, outer.hwnd) {
			outer, inner = inner, outer
		}
		if outer.interactive() || inner.interactive() {
			fatalf("a view that only hovers takes the mouse")
		}
		in := func(n *node, x, y int32) Point { return ClientToScreen(n.hwnd, Point{X: x, Y: y}) }
		h.hoverAt(h.window, in(inner, 10, 10))
		if want := []string{"outer+", "inner+"}; !slices.Equal(log, want) {
			fatalf("over the inner view: %v, want %v", log, want)
		}
		h.hoverAt(h.window, in(outer, 150, 10))
		if want := []string{"outer+", "inner+", "inner-"}; !slices.Equal(log, want) {
			fatalf("over the outer view: %v, want %v", log, want)
		}
		// A hidden view isn't hovered.
		log = nil
		h.hoverAt(h.window, in(inner, 10, 10))
		ShowWindow(inner.hwnd, SW_HIDE)
		h.hoverAt(h.window, in(outer, 30, 30))
		ShowWindow(inner.hwnd, SW_SHOWNA)
		if want := []string{"inner+", "inner-"}; !slices.Equal(log, want) {
			fatalf("hidden: %v, want %v", log, want)
		}
		// Off the app's windows.
		log = nil
		h.hoverAt(0, Point{})
		if want := []string{"outer-"}; !slices.Equal(log, want) {
			fatalf("left: %v, want %v", log, want)
		}
	})
}

// dragOver is a drag's events at a drop target.
type dragLog struct {
	events []string
	drops  []ui.Drop
}

func (l *dragLog) target(name string, accept ...string) *ui.DropTarget {
	return &ui.DropTarget{
		Accept:       accept,
		OnOverChange: func(on bool) { l.events = append(l.events, name+map[bool]string{true: "+", false: "-"}[on]) },
		OnMove:       func(x, y float32) { l.events = append(l.events, name+" move") },
		OnDrop: func(d ui.Drop) {
			l.events = append(l.events, name+" drop")
			l.drops = append(l.drops, d)
		},
	}
}

// pointArg is a POINTL passed by value.
func pointArg(p Point) uintptr { return uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32 }

// Another app's drag, through the window's IDropTarget (its vtable, as
// OLE calls it): taken by the innermost view that takes one of its
// items, which gets those it takes.
func TestDropFromAnotherApp(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "photo.png")
	os.WriteFile(png, make([]byte, 1234), 0o644)
	zip := filepath.Join(dir, "archive.zip")
	os.WriteFile(zip, []byte("PK"), 0o644)
	var l dragLog
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20)},
			ui.View(ui.ViewProps{Drop: l.target("outer", "text/plain"), Style: ui.Style{Padding: ui.All(20), Width: ui.Pt(200)}},
				ui.View(ui.ViewProps{Drop: l.target("inner", "image/*"), Style: box(60, 60)}),
			),
		)
	})
	onMain(t, func() {
		showHost(h)
		var outer, inner *node
		for _, n := range h.nodes {
			switch {
			case slices.Contains(n.dropAccept, "text/plain"):
				outer = n
			case slices.Contains(n.dropAccept, "image/*"):
				inner = n
			}
		}
		if outer == nil || inner == nil || !outer.listening(ops.EventDragOver) || !inner.listening(ops.EventDrop) {
			fatalf("the drop targets aren't there")
		}
		dt := dropTargets[h.window]
		if dt == nil {
			fatalf("the window takes no drops")
		}
		obj := NewDropTargetObject(dt)
		defer Release(obj)
		in := func(n *node, x, y int32) uintptr { return pointArg(ClientToScreen(n.hwnd, Point{X: x, Y: y})) }
		call := func(method int, args ...uintptr) uint32 {
			// The effect, in and out: on the heap, where it stays put.
			effect := new(uint32)
			*effect = DROPEFFECT_COPY | DROPEFFECT_MOVE
			// DragEnter, Drop: data, keys, pt, effect; DragOver: keys, pt, effect.
			if hr := ComCall(obj, method, append(args, uintptr(unsafe.Pointer(effect)))...); hr != 0 {
				fatalf("method %d: HRESULT %#x", method, hr)
			}
			return *effect
		}

		// A photo and a text: the photo's target takes it.
		data := NewDataObject()
		if data == 0 {
			fatalf("no data object")
		}
		defer Release(data)
		if !SetData(data, CF_HDROP, HDrop([]string{png, zip})) || !SetData(data, CF_UNICODETEXT, UTF16Data("hello")) {
			fatalf("SetData failed")
		}
		if e := call(3, data, MK_LBUTTON, in(inner, 10, 10)); e != DROPEFFECT_COPY {
			fatalf("over the photo's target: effect %d", e)
		}
		call(4, MK_LBUTTON, in(inner, 20, 10))
		call(4, MK_LBUTTON, in(inner, 20, 10)) // not moved: nothing
		if e := call(6, data, 0, in(inner, 20, 10)); e != DROPEFFECT_COPY {
			fatalf("dropped: effect %d", e)
		}
		// The core ends the drag over a view as it's dropped on.
		if want := []string{"inner+", "inner move", "inner move", "inner-", "inner drop"}; !slices.Equal(l.events, want) {
			fatalf("events %v, want %v", l.events, want)
		}
		d := l.drops[0]
		if d.Local || d.Copied || len(d.Items) != 1 {
			fatalf("drop %+v", d)
		}
		it := d.Items[0]
		if it.Path != png || it.Name != "photo.png" || it.MIME != "image/png" || it.Size != 1234 || it.Kind != ui.SharedImage {
			fatalf("item %+v", it)
		}
		if d.X != 20/h.scale || d.Y != 10/h.scale {
			fatalf("dropped at %v, %v", d.X, d.Y)
		}

		// Over the photo's target, a text goes to the target around it.
		l = dragLog{}
		text := NewDataObject()
		defer Release(text)
		SetData(text, CF_UNICODETEXT, UTF16Data("line 1\r\nline 2"))
		call(3, text, MK_LBUTTON, in(inner, 10, 10))
		ComCall(obj, 5) // DragLeave
		if want := []string{"outer+", "outer move", "outer-"}; !slices.Equal(l.events, want) {
			fatalf("a text: %v, want %v", l.events, want)
		}
		l = dragLog{}
		call(3, text, MK_LBUTTON, in(outer, 150, 10))
		call(6, text, 0, in(outer, 150, 10))
		if len(l.drops) != 1 || len(l.drops[0].Items) != 1 || l.drops[0].Items[0].Text != "line 1\nline 2" || l.drops[0].Items[0].Kind != ui.SharedText {
			fatalf("a text dropped: %+v", l.drops)
		}

		// What nothing takes: no effect, no events.
		l = dragLog{}
		other := NewDataObject()
		defer Release(other)
		SetData(other, CF_HDROP, HDrop([]string{zip}))
		if e := call(3, other, MK_LBUTTON, in(inner, 10, 10)); e != DROPEFFECT_NONE {
			fatalf("an archive: effect %d", e)
		}
		ComCall(obj, 5)
		if len(l.events) != 0 {
			fatalf("an archive: %v", l.events)
		}
	})
}

// A drag of a view of the app's: a mouse drag past the drag distance
// cancels its press and starts it; a drop on the app's own target sends
// its source, and the core gives the target the items, Values and all.
func TestDragWithinTheApp(t *testing.T) {
	type card struct{ title string }
	c := &card{"Card"}
	var l dragLog
	var phases []ui.DragPhase
	var pressed []bool
	presses := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 20},
			ui.View(ui.ViewProps{
				OnPress:         func() { presses++ },
				OnPressedChange: func(on bool) { pressed = append(pressed, on) },
				Drag: &ui.DragSource{
					Items:   func() []ui.DragItem { return []ui.DragItem{{Value: c, MIME: "application/x-card", Text: c.title}} },
					OnPhase: func(p ui.DragPhase) { phases = append(phases, p) },
				},
				Style: box(100, 40),
			}),
			ui.View(ui.ViewProps{Drop: l.target("column", "application/x-card"), Style: box(200, 100)}),
		)
	})
	onMain(t, func() {
		showHost(h)
		source, target := viewListening(h, ops.EventDragging), viewListening(h, ops.EventDrop)
		if !source.draggable() || source.dragItems[0].MIME != "application/x-card" || !source.dragItems[0].Value {
			fatalf("drag items %+v", source.dragItems)
		}
		// The drag, as OLE would run it over the app's own target (no
		// failing in here: it runs inside a window procedure).
		var hr uintptr
		var effect, enterEffect, allowedEffects uint32
		var during []ui.DragPhase
		var text string
		doDragDrop = func(data uintptr, allowed uint32) (uintptr, uint32) {
			doDragDrop = DoDragDrop
			during = slices.Clone(phases)
			text, _ = GetText(data, CF_UNICODETEXT, false)
			allowedEffects = allowed
			dt := dropTargets[h.window]
			pt := ClientToScreen(target.hwnd, Point{X: 10, Y: 10})
			enterEffect = dt.DragEnter(data, MK_LBUTTON, pt, allowed)
			effect = dt.Drop(data, 0, pt, allowed)
			hr = DRAGDROP_S_DROP
			return hr, effect
		}
		SendMessage(source.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(10, 10))
		SendMessage(source.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(60, 10))
		SendMessage(source.hwnd, WM_LBUTTONUP, 0, at(60, 10)) // as if it came: no press
		if hr != DRAGDROP_S_DROP || effect != DROPEFFECT_MOVE || enterEffect != DROPEFFECT_MOVE || allowedEffects&DROPEFFECT_MOVE == 0 {
			fatalf("the drag: ran %v, effects %d, %d (allowed %d)", hr == DRAGDROP_S_DROP, enterEffect, effect, allowedEffects)
		}
		if !slices.Equal(during, []ui.DragPhase{ui.DragBegan}) || text != "Card" {
			fatalf("as the drag ran: phases %v, other apps get %q", during, text)
		}
		if !slices.Equal(pressed, []bool{true, false}) || presses != 0 {
			fatalf("pressed %v, %d presses", pressed, presses)
		}
		if !slices.Equal(phases, []ui.DragPhase{ui.DragBegan, ui.DragDropped}) {
			fatalf("phases %v", phases)
		}
		if want := []string{"column+", "column move", "column-", "column drop"}; !slices.Equal(l.events, want) {
			fatalf("target events %v, want %v", l.events, want)
		}
		d := l.drops[0]
		if !d.Local || len(d.Items) != 1 || d.Items[0].Value != c {
			fatalf("dropped %+v", d)
		}
	})
}

// A drag cancelled reports Cancelled.
func TestDragCancelled(t *testing.T) {
	var phases []ui.DragPhase
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{
			Drag: &ui.DragSource{
				Items:   func() []ui.DragItem { return []ui.DragItem{{Kind: ui.SharedLink, Text: "https://example.com"}} },
				OnPhase: func(p ui.DragPhase) { phases = append(phases, p) },
			},
			Style: box(100, 40),
		})
	})
	onMain(t, func() {
		showHost(h)
		source := viewListening(h, ops.EventDragging)
		var link string
		doDragDrop = func(data uintptr, allowed uint32) (uintptr, uint32) {
			doDragDrop = DoDragDrop
			link, _ = GetText(data, cfURLW(), false)
			return DRAGDROP_S_CANCEL, DROPEFFECT_NONE
		}
		SendMessage(source.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, at(5, 5))
		SendMessage(source.hwnd, WM_MOUSEMOVE, MK_LBUTTON, at(5, 50))
		if link != "https://example.com" {
			fatalf("the link: %q", link)
		}
		if !slices.Equal(phases, []ui.DragPhase{ui.DragBegan, ui.DragCancelled}) {
			fatalf("phases %v", phases)
		}
	})
}
