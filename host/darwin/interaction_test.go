//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// AppKit's event types.
const (
	evLeftDown    = 1
	evLeftUp      = 2
	evLeftDragged = 6
)

// sendMouse sends the window a mouse event at x, y in n's view, as the
// window server would.
func sendMouse(h *Host, typ uint, n *node, x, y float64) {
	p := MsgPointFromID(n.view, Sel("convertPoint:toView:"), Point{X: x, Y: y}, 0)
	w := Send(n.view, "window")
	if !SendBool(w, "isVisible") {
		Send(w, "orderFront:", 0) // a window off screen takes no clicks
	}
	ev := MsgMouseEvent(Class("NSEvent"), Sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
		typ, p, 0, float64(time.Now().UnixNano())/1e9, SendInt(w, "windowNumber"), 0, 0, 1, 1)
	Send(w, "sendEvent:", uintptr(ev))
}

// viewSized is the View node of a size.
func viewSized(t *testing.T, h *Host, w, ht float64) *node {
	t.Helper()
	for _, n := range find(h, ops.KindView) {
		if n.frame.Size.Width == w && n.frame.Size.Height == ht {
			return n
		}
	}
	fatalf(t, "no %vx%v view", w, ht)
	return nil
}

// A click presses the innermost pressable view: Pressed, Press, then
// Pressed off; moving off and back on, it's released and pressed again;
// released off the view, it doesn't press; a pan that takes the mouse
// cancels it.
func TestPressed(t *testing.T) {
	var log []string
	rec := func(s string) func(bool) { return func(on bool) { log = append(log, fmt.Sprint(s, " ", on)) } }
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 20, Align: ui.AlignStart},
			ui.View(ui.ViewProps{
				OnPress:         func() { log = append(log, "press") },
				OnPressedChange: rec("outer"),
				Style:           ui.Style{Width: ui.Pt(200), Height: ui.Pt(100), Padding: ui.All(10), Gap: 10, Align: ui.AlignStart},
			},
				ui.View(ui.ViewProps{OnPressedChange: rec("inner"), Style: ui.Style{Width: ui.Pt(50), Height: ui.Pt(30)}}),
				ui.Button(ui.ButtonProps{Title: ui.Val("B"), OnPress: func() { log = append(log, "button") }}),
			),
			ui.View(ui.ViewProps{
				OnPress:         func() { log = append(log, "pan press") },
				OnPressedChange: rec("pannable"),
				OnPan:           func(g ui.Gesture) { log = append(log, fmt.Sprint("pan ", g.State)) },
				Style:           ui.Style{Width: ui.Pt(201), Height: ui.Pt(100)},
			}),
		)
	})
	onMain(func() {
		outer, inner, pannable := viewSized(t, h, 200, 100), viewSized(t, h, 50, 30), viewSized(t, h, 201, 100)
		expect := func(what string, want ...string) {
			t.Helper()
			if !slices.Equal(log, want) {
				t.Errorf("%s: %q, want %q", what, log, want)
			}
			log = nil
		}
		sendMouse(h, evLeftDown, outer, 150, 80)
		sendMouse(h, evLeftUp, outer, 150, 80)
		expect("a click", "outer true", "press", "outer false")

		sendMouse(h, evLeftDown, outer, 150, 80)
		sendMouse(h, evLeftDragged, outer, 150, 90)
		sendMouse(h, evLeftDragged, outer, 150, 130) // off
		sendMouse(h, evLeftDragged, outer, 150, 95)  // back on
		sendMouse(h, evLeftUp, outer, 150, 95)
		expect("off and back on", "outer true", "outer false", "outer true", "press", "outer false")

		sendMouse(h, evLeftDown, outer, 150, 80)
		sendMouse(h, evLeftDragged, outer, 250, 80)
		sendMouse(h, evLeftUp, outer, 250, 80)
		expect("released off", "outer true", "outer false")

		// The inner pressable view takes its clicks: its ancestor isn't pressed.
		sendMouse(h, evLeftDown, inner, 10, 10)
		sendMouse(h, evLeftUp, inner, 10, 10)
		expect("the inner view", "inner true", "inner false")

		// A pan takes the mouse: no Press.
		sendMouse(h, evLeftDown, pannable, 20, 50)
		for i := 1; i <= 8; i++ {
			sendMouse(h, evLeftDragged, pannable, 20+float64(i)*4, 50)
		}
		sendMouse(h, evLeftUp, pannable, 52, 50)
		if len(log) < 3 || log[0] != "pannable true" || log[1] != "pannable false" || log[2] != fmt.Sprint("pan ", ops.GestureStateBegan) || slices.Contains(log, "pan press") {
			t.Errorf("a pan: %q", log)
		}
		log = nil

		// The accessibility press still presses.
		Send(outer.view, "accessibilityPerformPress")
		expect("VoiceOver", "press")
	})
}

// Hover: a tracking area's entered and exited; nested views both stay
// hovered; leaving the app, nothing is; a view that appears under the
// pointer is hovered.
func TestHover(t *testing.T) {
	var log []string
	rec := func(s string) func(bool) { return func(on bool) { log = append(log, fmt.Sprint(s, " ", on)) } }
	shown := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 20, Align: ui.AlignStart},
			ui.View(ui.ViewProps{OnHoverChange: rec("outer"), Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(100), Padding: ui.All(10)}},
				ui.View(ui.ViewProps{OnHoverChange: rec("inner"), Style: ui.Style{Width: ui.Pt(50), Height: ui.Pt(30)}}),
			),
			ui.Show(shown, func() ui.Node {
				return ui.View(ui.ViewProps{OnHoverChange: rec("new"), Style: ui.Style{Width: ui.Pt(60), Height: ui.Pt(60)}})
			}),
		)
	})
	onMain(func() {
		outer, inner := viewSized(t, h, 200, 100), viewSized(t, h, 50, 30)
		areas := ArrayItems(Send(outer.view, "trackingAreas"))
		if len(areas) != 1 || uint(Send(areas[0], "options")) != 0x641 {
			fatalf(t, "tracking areas %d", len(areas))
		}
		Send(outer.view, "mouseEntered:", 0)
		Send(inner.view, "mouseEntered:", 0)
		Send(inner.view, "mouseExited:", 0)
		Send(inner.view, "mouseEntered:", 0)
		if want := []string{"outer true", "inner true", "inner false", "inner true"}; !slices.Equal(log, want) {
			fatalf(t, "%q, want %q", log, want)
		}
		log = nil
		Send(h.target, "applicationDidResignActive:", 0)
		slices.Sort(log)
		if want := []string{"inner false", "outer false"}; !slices.Equal(log, want) {
			fatalf(t, "leaving the app: %q", log)
		}
		log = nil

		// A view that appears under a still pointer.
		saved := hoverProbe
		defer func() { hoverProbe = saved }()
		hoverProbe = func(w ID) (Point, bool) {
			// The new view's center, below the outer view.
			p := MsgPointFromID(h.root.view, Sel("convertPoint:toView:"), Point{X: 50, Y: 170}, 0)
			return p, w == h.window
		}
		h.enter(func() { ui.Post(func() { shown.Set(true) }) })
		h.runPosted()
		if want := []string{"new true"}; !slices.Equal(log, want) {
			fatalf(t, "appearing under the pointer: %q", log)
		}
		log = nil
		// Hidden: not hovered.
		n := viewSized(t, h, 60, 60)
		Send(n.view, "setHidden:", 1)
		if want := []string{"new false"}; !slices.Equal(log, want) {
			fatalf(t, "hidden: %q", log)
		}
	})
}

// testDrag is a fake NSDraggingInfo: a pasteboard, a place, a session.
type testDrag struct {
	pb   ID
	at   Point
	seq  int
	mask uint
}

var testDrags = map[ID]*testDrag{}

func newDragInfo(pb ID, seq int) (ID, *testDrag) {
	if !HasClass("RyUITestDraggingInfo") {
		DefineClass("RyUITestDraggingInfo", "NSObject", nil, []objc.MethodDef{
			Method("draggingPasteboard", func(self ID, _ SEL) ID { return testDrags[self].pb }),
			Method("draggingLocation", func(self ID, _ SEL) Point { return testDrags[self].at }),
			Method("draggingSequenceNumber", func(self ID, _ SEL) int { return testDrags[self].seq }),
			Method("draggingSourceOperationMask", func(self ID, _ SEL) uint { return testDrags[self].mask }),
			Method("draggingSource", func(self ID, _ SEL) ID { return 0 }),
		})
	}
	info := New("RyUITestDraggingInfo")
	d := &testDrag{pb: pb, seq: seq, mask: ^uint(0)}
	testDrags[info] = d
	return info, d
}

// pasteboard is a uniquely named pasteboard with items, each a map of
// types to strings.
func pasteboard(items ...map[string]string) ID {
	pb := Send(Class("NSPasteboard"), "pasteboardWithUniqueName")
	Send(pb, "clearContents")
	var objs []ID
	for _, it := range items {
		pbi := Autorelease(New("NSPasteboardItem"))
		for k, v := range it {
			Send(pbi, "setString:forType:", uintptr(String(v)), uintptr(String(k)))
		}
		objs = append(objs, pbi)
	}
	Send(pb, "writeObjects:", uintptr(Array(objs...)))
	return pb
}

// Drops: AppKit gives a drag to the innermost registered view, which
// hands it to the innermost target above it that takes it; DragOver
// follows it; a text, a file and the app's own drag drop as the core
// expects.
func TestDrop(t *testing.T) {
	var log []string
	var drops []ui.Drop
	target := func(name string, accept ...string) *ui.DropTarget {
		return &ui.DropTarget{
			Accept:       accept,
			OnOverChange: func(on bool) { log = append(log, fmt.Sprint(name, " over ", on)) },
			OnDrop:       func(d ui.Drop) { log = append(log, name+" drop"); drops = append(drops, d) },
		}
	}
	type card struct{ title string }
	c := &card{"Card"}
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 20, Align: ui.AlignStart},
			ui.View(ui.ViewProps{Drop: target("outer", "text/plain", "application/x-card"), Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(100), Padding: ui.All(10)}},
				ui.View(ui.ViewProps{Drop: target("inner", "image/*"), Style: ui.Style{Width: ui.Pt(50), Height: ui.Pt(30)}}),
			),
			ui.View(ui.ViewProps{Drop: target("files", "*/*"), Style: ui.Style{Width: ui.Pt(201), Height: ui.Pt(50)}}),
			ui.View(ui.ViewProps{
				Drag: &ui.DragSource{
					Items: func() []ui.DragItem { return []ui.DragItem{{Value: c, MIME: "application/x-card", Text: c.title}} },
				},
				Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(40), Background: ui.Accent},
			}),
		)
	})
	onMain(func() {
		outer, inner, files, source := viewSized(t, h, 200, 100), viewSized(t, h, 50, 30), viewSized(t, h, 201, 50), viewSized(t, h, 80, 40)
		if n := SendInt(Send(inner.view, "registeredDraggedTypes"), "count"); n < 4 {
			fatalf(t, "the inner target registered %d types", n)
		}
		at := func(n *node, x, y float64) Point {
			return MsgPointFromID(n.view, Sel("convertPoint:toView:"), Point{X: x, Y: y}, 0)
		}

		// A text over the inner image target: the outer takes it.
		pb := pasteboard(map[string]string{"public.utf8-plain-text": "hello"})
		defer Send(pb, "releaseGlobally")
		info, d := newDragInfo(pb, 1001)
		d.at = at(inner, 10, 10)
		if op := uint(Send(inner.view, "draggingEntered:", uintptr(info))); op != nsDragOperationCopy {
			fatalf(t, "entered: operation %d", op)
		}
		d.at = at(inner, 12, 10)
		Send(inner.view, "draggingUpdated:", uintptr(info))
		// Out of the inner view onto the outer one, the same target: still over.
		d.at = at(outer, 150, 80)
		Send(inner.view, "draggingExited:", uintptr(info))
		Send(outer.view, "draggingEntered:", uintptr(info))
		if !SendBool(outer.view, "prepareForDragOperation:", uintptr(info)) || !SendBool(outer.view, "performDragOperation:", uintptr(info)) {
			fatal(t, "the drop was refused")
		}
		Send(outer.view, "draggingEnded:", uintptr(info))
		if want := []string{"outer over true", "outer over false", "outer drop"}; !slices.Equal(log, want) {
			fatalf(t, "a text: %q, want %q", log, want)
		}
		if dr := drops[0]; dr.Local || len(dr.Items) != 1 || dr.Items[0].Text != "hello" || dr.Items[0].Kind != ui.SharedText || dr.X != 150 || dr.Y != 80 {
			fatalf(t, "a text: %+v", dr)
		}
		log, drops = nil, nil

		// A file from Finder: the file itself, its type and size.
		dir := t.TempDir()
		path := filepath.Join(dir, "notes.pdf")
		os.WriteFile(path, []byte("some notes"), 0o644)
		pb2 := pasteboard(map[string]string{"public.file-url": GoString(Send(FileURL(path), "absoluteString"))})
		defer Send(pb2, "releaseGlobally")
		info2, d2 := newDragInfo(pb2, 1002)
		d2.at = at(inner, 10, 10)
		// Neither the inner nor the outer target takes a PDF.
		if op := uint(Send(inner.view, "draggingEntered:", uintptr(info2))); op != nsDragOperationNone || len(log) != 0 {
			fatalf(t, "a file over targets that don't take it: %d, %q", op, log)
		}
		Send(inner.view, "draggingExited:", uintptr(info2))
		d2.at = at(files, 10, 10)
		Send(files.view, "draggingEntered:", uintptr(info2))
		Send(files.view, "performDragOperation:", uintptr(info2))
		if want := []string{"files over true", "files over false", "files drop"}; !slices.Equal(log, want) {
			fatalf(t, "a file: %q, want %q", log, want)
		}
		if dr := drops[0]; dr.Copied || len(dr.Items) != 1 || !strings.HasSuffix(dr.Items[0].Path, "notes.pdf") || dr.Items[0].MIME != "application/pdf" ||
			dr.Items[0].Size != 10 || dr.Items[0].Name != "notes.pdf" || dr.Items[0].Kind != ui.SharedFile {
			fatalf(t, "a file: %+v", dr)
		}
		log, drops = nil, nil

		// Image data (no file): written to the caches, the app's copy.
		png := Bytes(Send(Send(Send(Class("NSBitmapImageRep"), "alloc"), "initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:",
			0, 2, 2, 8, 4, 1, 0, uintptr(String("NSDeviceRGBColorSpace")), 0, 0), "representationUsingType:properties:", 4, uintptr(Dict())))
		pb4 := Send(Class("NSPasteboard"), "pasteboardWithUniqueName")
		defer Send(pb4, "releaseGlobally")
		Send(pb4, "clearContents")
		img := Autorelease(New("NSPasteboardItem"))
		Send(img, "setData:forType:", uintptr(Data(png)), uintptr(String("public.png")))
		Send(pb4, "writeObjects:", uintptr(Array(img)))
		info4, d4 := newDragInfo(pb4, 1004)
		d4.at = at(inner, 5, 5)
		Send(inner.view, "draggingEntered:", uintptr(info4))
		Send(inner.view, "performDragOperation:", uintptr(info4))
		if want := []string{"inner over true", "inner over false", "inner drop"}; !slices.Equal(log, want) {
			fatalf(t, "image data: %q, want %q", log, want)
		}
		if dr := drops[0]; !dr.Copied || len(dr.Items) != 1 || dr.Items[0].MIME != "image/png" || dr.Items[0].Kind != ui.SharedImage || dr.Items[0].Size != int64(len(png)) {
			fatalf(t, "image data: %+v", dr)
		} else {
			if b, err := os.ReadFile(dr.Items[0].Path); err != nil || len(b) != len(png) {
				fatalf(t, "the image's copy: %v", err)
			}
			os.RemoveAll(filepath.Dir(dr.Items[0].Path))
		}
		log, drops = nil, nil

		// The app's own drag: a move, dropped at once with its Values.
		h.enter(func() {
			h.core.Dispatch(ops.Event{ID: source.id, Kind: ops.EventDragging, Int: int64(ops.DragPhaseBegan)})
		})
		pb3 := pasteboard(map[string]string{dragType: fmt.Sprintf("%d:%d", os.Getpid(), source.id), "public.utf8-plain-text": "Card"})
		defer Send(pb3, "releaseGlobally")
		info3, d3 := newDragInfo(pb3, 1003)
		d3.at = at(files, 20, 20)
		if op := uint(Send(files.view, "draggingEntered:", uintptr(info3))); op != nsDragOperationMove {
			fatalf(t, "a drag within the app: operation %d", op)
		}
		Send(files.view, "draggingExited:", uintptr(info3))
		d3.at = at(outer, 100, 50)
		Send(outer.view, "draggingEntered:", uintptr(info3))
		Send(outer.view, "performDragOperation:", uintptr(info3))
		if want := []string{"files over true", "files over false", "outer over true", "outer over false", "outer drop"}; !slices.Equal(log, want) {
			fatalf(t, "within the app: %q, want %q", log, want)
		}
		if dr := drops[0]; !dr.Local || len(dr.Items) != 1 || dr.Items[0].Value != c {
			fatalf(t, "within the app: %+v", dr)
		}
	})
}

// A move with the button down past the drag distance drags a draggable
// view: its press is cancelled, its items go on the pasteboard, and the
// session reports Began, then Dropped or Cancelled.
func TestDragSource(t *testing.T) {
	var log []string
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Align: ui.AlignStart},
			ui.View(ui.ViewProps{
				OnPress:         func() { log = append(log, "press") },
				OnPressedChange: func(on bool) { log = append(log, fmt.Sprint("pressed ", on)) },
				Drag: &ui.DragSource{
					Items: func() []ui.DragItem {
						return []ui.DragItem{{Text: "hello"}, {Kind: ui.SharedLink, Text: "https://example.com"}, {Value: 1, MIME: "application/x-n"}}
					},
					OnPhase: func(p ui.DragPhase) { log = append(log, fmt.Sprint("drag ", p)) },
				},
				Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(40), Background: ui.Accent},
			}),
		)
	})
	onMain(func() {
		v := viewSized(t, h, 80, 40)
		var session ID
		saved := beginSession
		defer func() { beginSession = saved }()
		beginSession = func(view, items, ev ID) ID {
			// What AppKit gets; the session itself needs the window server.
			for _, it := range ArrayItems(items) {
				pbi := Send(it, "item")
				var types []string
				for _, t := range ArrayItems(Send(pbi, "types")) {
					types = append(types, GoString(t))
				}
				log = append(log, strings.Join(types, ","))
			}
			session = view
			if !HasClass("RyUITestSession") {
				DefineClass("RyUITestSession", "NSObject", nil, []objc.MethodDef{
					Method("setAnimatesToStartingPositionsOnCancelOrFail:", func(self ID, _ SEL, on bool) {}),
				})
			}
			return Autorelease(New("RyUITestSession"))
		}
		sendMouse(h, evLeftDown, v, 10, 10)
		sendMouse(h, evLeftDragged, v, 11, 10) // within the drag distance
		sendMouse(h, evLeftDragged, v, 15, 10)
		want := []string{"pressed true", "pressed false",
			"public.utf8-plain-text," + dragType,
			"public.url,public.utf8-plain-text," + dragType,
			dragType}
		if !slices.Equal(log, want) || session != v.view {
			fatalf(t, "%q, want %q", log, want)
		}
		log = nil
		if h.drag == nil || h.drag.id != v.id {
			fatal(t, "no drag under way")
		}
		if m := uint(Send(v.view, "draggingSession:sourceOperationMaskForDraggingContext:", 0, 0)); m != nsDragOperationCopy {
			fatalf(t, "outside the app: mask %d", m)
		}
		if m := uint(Send(v.view, "draggingSession:sourceOperationMaskForDraggingContext:", 0, 1)); m != 21 {
			fatalf(t, "within the app: mask %d", m)
		}
		// A drag of only values stays in the app.
		items := h.drag.items
		h.drag.items = []ops.Item{{MIME: "application/x-n", Value: true}}
		if m := h.dragMask(0); m != nsDragOperationNone {
			fatalf(t, "values outside the app: mask %d", m)
		}
		h.drag.items = items
		h.dragBegan()
		h.endDrag(nsDragOperationCopy)
		h.endDrag(nsDragOperationNone)
		if want := []string{fmt.Sprint("drag ", ui.DragBegan), fmt.Sprint("drag ", ui.DragDropped)}; !slices.Equal(log, want) {
			fatalf(t, "%q, want %q", log, want)
		}
		// The mouse's up after the drag presses nothing.
		log = nil
		sendMouse(h, evLeftUp, v, 15, 10)
		if len(log) != 0 {
			fatalf(t, "after the drag: %q", log)
		}
	})
}
