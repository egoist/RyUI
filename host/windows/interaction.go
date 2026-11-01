//go:build windows && amd64

package windows

import (
	"fmt"
	"math"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// Interaction states and drag and drop (DESIGN.md, "Interaction states,
// drag and drop"):
//
//   - Pressed: a view's press with the mouse (views.go: mouseDown,
//     mouseMove, mouseUp) or Space, reported as it changes (setPressed).
//   - Hover: tracked from the message loop. Each mouse move finds the
//     views under the pointer by their rectangles, in the app's window
//     under it (views that let the mouse through count), and
//     TrackMouseEvent's WM_MOUSELEAVE, or a timer where it can't watch,
//     clears them as the pointer leaves the app's windows.
//   - Drop targets: each top-level window of the app is an OLE drop
//     target, which finds the innermost view under the drag that takes
//     it.
//   - Drag sources: a mouse drag of a view with DragItems runs an OLE
//     drag (DoDragDrop) of the shell's data object.

const timerHover = 6

// tracksPress: the mouse's primary button down on it presses it (a view
// with Press, LongPress or a menu, or one listening for Pressed).
func (n *node) tracksPress() bool { return n.pressable() || n.listening(ops.EventPressed) }

// draggable: a drag of it with the mouse starts a drag and drop.
func (n *node) draggable() bool { return len(n.dragItems) > 0 }

// drawsStates: it reports its pressed or hover state, and the app draws
// them: the host's hover and pressed fills are left off.
func (n *node) drawsStates() bool {
	return n.listening(ops.EventPressed) || n.listening(ops.EventHover)
}

// setPressed sets whether n shows pressed, and reports each change.
func (h *Host) setPressed(n *node, on bool) {
	if n.pressed == on {
		return
	}
	n.pressed = on
	if !n.drawsStates() {
		h.redraw(n)
	}
	h.emit(n, ops.Event{Kind: ops.EventPressed, Bool: on})
}

// cancelPress ends a press of the mouse without a Press: a pan, a long
// press or a drag took it, or it lost the capture.
func (h *Host) cancelPress(n *node) {
	n.tracking = false
	if n.hwnd != 0 {
		KillTimer(n.hwnd, timerLongPress)
	}
	h.setPressed(n, false)
}

// endPress ends a press that fires (Press, then Pressed false, as a tap
// reports them) or not. A menu the press shows comes after.
func (h *Host) endPress(n *node, hwnd uintptr, fire bool) {
	if !fire || len(n.menu) > 0 && n.menuTrigger == int32(ops.MenuTriggerTap) && !n.listening(ops.EventPress) {
		h.setPressed(n, false)
	}
	if fire {
		h.press(n, hwnd)
	}
	h.setPressed(n, false)
}

// Hover.

// windowAt is the deepest visible window at pt (on the screen) in top,
// found by the windows' rectangles, not by hit-testing: views that let
// the mouse through count.
func windowAt(top uintptr, pt Point) uintptr {
	w := top
	for {
		next := uintptr(0)
		for c := NextWindow(w, GW_CHILD); c != 0; c = NextWindow(c, GW_HWNDNEXT) {
			if WindowLong(c, GWL_STYLE)&WS_VISIBLE == 0 {
				continue
			}
			if r := WindowRect(c); pt.X >= r.Left && pt.X < r.Right && pt.Y >= r.Top && pt.Y < r.Bottom {
				next = c
				break
			}
		}
		if next == 0 {
			return w
		}
		w = next
	}
}

// viewsAt are the views at pt in top, innermost first: the window there
// and its ancestors. The slice is reused by the next call.
func (h *Host) viewsAt(top uintptr, pt Point) []*node {
	h.chain = h.chain[:0]
	for w := windowAt(top, pt); w != 0; w = Parent(w) {
		if n := h.byHwnd[w]; n != nil && n.hwnd == w && h.nodes[n.id] == n {
			h.chain = append(h.chain, n)
		}
		if w == top {
			break
		}
	}
	return h.chain
}

// hoverAt finds the views listening for Hover under the pointer, at pt in
// top (0: not over the app's windows), and reports the changes: those it
// left (innermost first), then those it came over (outermost first).
// Called again while it reports (a report's flush moved views under the
// pointer), it goes again once they're reported.
func (h *Host) hoverAt(top uintptr, pt Point) {
	h.hoverWin, h.hoverPt = top, pt
	if h.hovering {
		h.hoverAgain = true
		return
	}
	h.hovering = true
	defer func() { h.hovering = false }()
	for range 8 { // a style that moves a view off the pointer as it's hovered, and back as it isn't, would go on
		h.hoverAgain = false
		h.hoverOnce()
		if !h.hoverAgain {
			return
		}
	}
}

func (h *Host) hoverOnce() {
	if h.hoverViews == 0 && len(h.hovered) == 0 {
		return
	}
	top, pt := h.hoverWin, h.hoverPt
	now := h.hoverNext[:0]
	if top != 0 && IsWindow(top) && h.keyWindow(top) == top {
		for _, n := range h.viewsAt(top, pt) {
			if n.listening(ops.EventHover) {
				now = append(now, n)
			}
		}
	}
	h.hoverNext = now
	if slices.Equal(now, h.hovered) {
		return
	}
	was, cur := h.hovered, slices.Clone(now)
	h.hovered = cur
	for _, n := range was {
		if !slices.Contains(cur, n) {
			h.emit(n, ops.Event{Kind: ops.EventHover, Bool: false})
		}
	}
	for i := len(cur) - 1; i >= 0; i-- {
		if n := cur[i]; !slices.Contains(was, n) {
			h.emit(n, ops.Event{Kind: ops.EventHover, Bool: true})
		}
	}
}

// pointerMoved follows the pointer at pt (on the screen; hwnd the window
// of the app's the move was for) for Hover. With track, the window under
// it is watched for the pointer leaving (TrackMouseEvent); where it
// can't be (the mouse captured, another thread's window: a web view's),
// a timer checks now and then.
func (h *Host) pointerMoved(hwnd uintptr, pt Point, track bool) {
	h.hoverRefreshes = 0
	if h.hoverViews == 0 && len(h.hovered) == 0 {
		h.hoverWin, h.hoverPt = h.keyWindow(hwnd), pt
		return
	}
	under := WindowFromPoint(pt)
	h.hoverAt(h.keyWindow(under), pt)
	if len(h.hovered) == 0 {
		KillTimer(h.msgWin, timerHover)
		return
	}
	if track && Capture() == 0 && under != 0 && WindowThread(under) == CurrentThreadID() {
		if under != h.tracked {
			TrackLeave(under)
			h.tracked = under
		}
		KillTimer(h.msgWin, timerHover)
		return
	}
	SetTimer(h.msgWin, timerHover, 100)
}

// mouseLeft is a WM_MOUSELEAVE: the pointer left a window of the app's
// (for another of them, or another app's).
func (h *Host) mouseLeft(hwnd uintptr) {
	if h.tracked != 0 && hwnd != h.tracked {
		return // a window left before the one watched now
	}
	h.tracked = 0
	h.pointerMoved(hwnd, CursorPos(), false)
}

// refreshHover finds the views under the pointer again once the core has
// returned: views shown, hidden, moved or made under it.
// A few times between moves of the pointer: a style that moves a view off
// the pointer as it's hovered, and back as it isn't, would go on.
func (h *Host) refreshHover() {
	if h.hoverQueued || h.hoverViews == 0 && len(h.hovered) == 0 || h.hoverRefreshes >= 8 {
		return
	}
	h.hoverQueued = true
	h.afterCore(func() {
		h.hoverQueued = false
		h.hoverRefreshes++
		h.hoverAt(h.hoverWin, h.hoverPt)
	})
}

// forgetInteraction drops a destroyed view from the states the host
// follows (the core reports leaving them itself).
func (h *Host) forgetInteraction(n *node) {
	if n.listening(ops.EventHover) {
		h.hoverViews--
	}
	if i := slices.Index(h.hovered, n); i >= 0 {
		// A new slice: hoverOnce may be going through the old one.
		h.hovered = slices.Delete(slices.Clone(h.hovered), i, i+1)
	}
	for _, t := range dropTargets {
		if t.target == n {
			t.target = nil
		}
	}
}

// Drop targets.

var (
	cfSource = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("RyUI Drag Source") })
	cfURLW   = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("UniformResourceLocatorW") })
	cfURL    = sync.OnceValue(func() uint16 { return RegisterClipboardFormat("UniformResourceLocator") })
)

// dropTargets are the app's top-level windows' drop targets, by window:
// revoked as the window is destroyed.
var dropTargets = map[uintptr]*dropTarget{}

// acceptDrops makes a top-level window of the app's take drops.
func (h *Host) acceptDrops(win uintptr) {
	t := &dropTarget{h: h, win: win}
	if RegisterDropTarget(win, t) {
		dropTargets[win] = t
	}
}

// windowDestroyed revokes a window's drop target, and stops watching it
// for the pointer.
func windowDestroyed(hwnd uintptr) {
	if h := theHost; h != nil && h.tracked == hwnd {
		h.tracked = 0
	}
	if t := dropTargets[hwnd]; t != nil {
		t.end()
		RevokeDropTarget(hwnd)
		delete(dropTargets, hwnd)
	}
}

// dropTarget is a top-level window's IDropTarget.
type dropTarget struct {
	h      *Host
	win    uintptr
	data   uintptr // the drag over the window's data object (a reference)
	drag   dragInfo
	target *node      // the view it's over that takes it
	at     [2]float32 // its place in target, in points, last reported
}

// dragInfo is what a drag carries, as far as taking it goes.
type dragInfo struct {
	source ops.NodeID // a drag from the app: the view it's from
	mimes  []string   // its items' MIME types
}

func (t *dropTarget) DragEnter(data uintptr, keys uint32, pt Point, allowed uint32) uint32 {
	t.end()
	AddRef(data)
	t.data = data
	t.drag = t.h.describeDrag(data)
	e := t.over(pt, allowed)
	t.h.dropHelper().Enter(t.win, data, pt, e)
	return e
}

func (t *dropTarget) DragOver(keys uint32, pt Point, allowed uint32) uint32 {
	e := t.over(pt, allowed)
	t.h.dropHelper().Over(pt, e)
	return e
}

func (t *dropTarget) DragLeave() {
	t.h.dropHelper().Leave()
	t.end()
}

func (t *dropTarget) Drop(data uintptr, keys uint32, pt Point, allowed uint32) uint32 {
	if t.data == 0 {
		// A drop without DragEnter: what it carries, now.
		AddRef(data)
		t.data = data
		t.drag = t.h.describeDrag(data)
	}
	e := t.over(pt, allowed)
	t.h.dropHelper().Drop(data, pt, e)
	if n := t.target; n != nil && e != DROPEFFECT_NONE {
		d := ops.Drop{X: t.at[0], Y: t.at[1], Source: t.drag.source}
		if d.Source == 0 {
			d.Items, d.Copied = t.h.droppedItems(data, n.dropAccept)
		}
		t.h.emit(n, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
	}
	t.end()
	return e
}

// end ends the drag over the window: Cancelled for the view it was
// over, and its data let go.
func (t *dropTarget) end() {
	t.setTarget(nil, Point{})
	if t.data != 0 {
		Release(t.data)
		t.data = 0
	}
	t.drag = dragInfo{}
}

// over finds the view the drag at pt is over, reports it, and returns
// the effect of a drop there.
func (t *dropTarget) over(pt Point, allowed uint32) uint32 {
	n := t.h.dropTargetAt(t.win, pt, t.drag.mimes)
	t.setTarget(n, pt)
	if t.target == nil {
		return DROPEFFECT_NONE
	}
	// A move for the app's own drags, a copy for other apps'; else what
	// the source allows.
	prefer := [3]uint32{DROPEFFECT_COPY, DROPEFFECT_MOVE, DROPEFFECT_LINK}
	if t.drag.source != 0 {
		prefer = [3]uint32{DROPEFFECT_MOVE, DROPEFFECT_COPY, DROPEFFECT_LINK}
	}
	for _, e := range prefer {
		if allowed&e != 0 {
			return e
		}
	}
	return DROPEFFECT_NONE
}

// setTarget reports the drag over n (nil: none) at pt: Cancelled for the
// view it leaves, Began for the one it comes over, Changed as it moves.
func (t *dropTarget) setTarget(n *node, pt Point) {
	h := t.h
	if n != t.target {
		if old := t.target; old != nil {
			t.target = nil
			h.emit(old, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: ops.GestureStateCancelled, X: t.at[0], Y: t.at[1], Scale: 1}})
		}
		if n == nil || h.nodes[n.id] != n || n.hwnd == 0 {
			return
		}
		t.target = n
		t.at = h.pointIn(n, pt)
		h.emit(n, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: ops.GestureStateBegan, X: t.at[0], Y: t.at[1], Scale: 1}})
		return
	}
	if n == nil || n.hwnd == 0 {
		return
	}
	if at := h.pointIn(n, pt); at != t.at {
		t.at = at
		h.emit(n, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: ops.GestureStateChanged, X: at[0], Y: at[1], Scale: 1}})
	}
}

// pointIn is a point on the screen in n, in points.
func (h *Host) pointIn(n *node, pt Point) [2]float32 {
	p := ScreenToClient(n.hwnd, pt)
	return [2]float32{h.pt(n, p.X), h.pt(n, p.Y)}
}

// dropTargetAt is the innermost view at pt in top that takes a drag of
// items of these MIME types.
func (h *Host) dropTargetAt(top uintptr, pt Point, mimes []string) *node {
	if len(mimes) == 0 {
		return nil
	}
	for _, n := range h.viewsAt(top, pt) {
		if len(n.dropAccept) > 0 && takes(n.dropAccept, mimes) {
			return n
		}
	}
	return nil
}

// takes reports whether any of the MIME types matches the patterns.
func takes(accept, mimes []string) bool {
	for _, m := range mimes {
		if ops.MatchMIME(accept, m) {
			return true
		}
	}
	return false
}

// dropHelper is the shell's drop target helper, which shows a drag's
// image over the app's windows.
func (h *Host) dropHelper() DropHelper {
	if !h.dropHelperMade {
		h.dropHelperMade = true
		h.dropHelp = NewDropHelper()
	}
	return h.dropHelp
}

// describeDrag finds what a drag's data carries: the items of the view
// it's from, for the app's own drag (the private format, with this
// process's ID); another app's files (CF_HDROP, or virtual files), text
// and link.
func (h *Host) describeDrag(data uintptr) dragInfo {
	var d dragInfo
	if src, ok := localSource(data); ok {
		d.source = src
		items := h.dragSnapshot
		if src != h.dragSource {
			if n := h.nodes[src]; n != nil {
				items = n.dragItems
			} else {
				items = nil
			}
		}
		for _, it := range items {
			d.mimes = append(d.mimes, it.MIME)
		}
		return d
	}
	files := DroppedFiles(data)
	for _, p := range files {
		d.mimes = append(d.mimes, fileMIME(p))
	}
	if len(files) == 0 {
		for _, f := range VirtualFiles(data) {
			if !f.Dir {
				d.mimes = append(d.mimes, fileMIME(f.Name))
			}
		}
	}
	if HasData(data, CF_UNICODETEXT) {
		d.mimes = append(d.mimes, "text/plain")
	}
	if HasData(data, cfURLW()) || HasData(data, cfURL()) {
		d.mimes = append(d.mimes, "text/uri-list")
	}
	return d
}

// localSource is the view a drag of the app's is from: the private
// format, with this process's ID.
func localSource(data uintptr) (ops.NodeID, bool) {
	b, ok := GetData(data, cfSource())
	if !ok || len(b) < 8 || le.Uint32(b) != uint32(os.Getpid()) {
		return 0, false
	}
	return ops.NodeID(le.Uint32(b[4:])), true
}

// droppedItems loads the items of another app's drop that a target
// accepting accept takes: its files (the files themselves; virtual files
// copied into the app's caches, copied then true), its text and its
// link.
func (h *Host) droppedItems(data uintptr, accept []string) (items []ops.Item, copied bool) {
	files := DroppedFiles(data)
	for _, p := range files {
		m := fileMIME(p)
		if !ops.MatchMIME(accept, m) {
			continue
		}
		size := int64(-1)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			size = st.Size()
		}
		items = append(items, ops.Item{Kind: fileKind(m), Path: p, Name: filepath.Base(p), MIME: m, Size: size})
	}
	if len(files) == 0 {
		var dir string
		for i, f := range VirtualFiles(data) {
			m := fileMIME(f.Name)
			if f.Dir || !ops.MatchMIME(accept, m) {
				continue
			}
			if dir == "" {
				if dir = h.dropDir(); dir == "" {
					break
				}
			}
			name := filepath.Base(strings.ReplaceAll(f.Name, "/", `\`))
			path := filepath.Join(dir, name)
			if _, err := os.Lstat(path); err == nil {
				path = filepath.Join(dir, fmt.Sprintf("%d %s", i, name))
			}
			size, err := SaveVirtualFile(data, i, f.Size, path)
			if err != nil {
				os.Remove(path)
				continue
			}
			items = append(items, ops.Item{Kind: fileKind(m), Path: path, Name: name, MIME: m, Size: size})
			copied = true
		}
	}
	var link string
	if ops.MatchMIME(accept, "text/uri-list") {
		u, ok := GetText(data, cfURLW(), false)
		if !ok {
			u, _ = GetText(data, cfURL(), true)
		}
		if link = strings.TrimSpace(u); link != "" {
			items = append(items, ops.Item{Kind: ops.ShareKindLink, Text: link, MIME: "text/uri-list", Size: -1})
		}
	}
	if ops.MatchMIME(accept, "text/plain") {
		// A browser's link comes as its text too: once.
		if s, ok := GetText(data, CF_UNICODETEXT, false); ok && s != "" && strings.TrimSpace(s) != link {
			items = append(items, ops.Item{Kind: ops.ShareKindText, Text: strings.ReplaceAll(s, "\r\n", "\n"), MIME: "text/plain", Size: -1})
		}
	}
	return items, copied && len(items) > 0
}

// dropDir is a new folder in the app's caches for a drop's copies, or "".
func (h *Host) dropDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	base = filepath.Join(base, h.appName, "Drops")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return ""
	}
	dir, err := os.MkdirTemp(base, "drop")
	if err != nil {
		return ""
	}
	return dir
}

// fileMIME is a file's MIME type from its name.
func fileMIME(path string) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		return strings.TrimSpace(t)
	}
	return "application/octet-stream"
}

// fileKind is the ShareKind of a file of a MIME type.
func fileKind(m string) ops.ShareKind {
	switch {
	case strings.HasPrefix(m, "image/"):
		return ops.ShareKindImage
	case strings.HasPrefix(m, "video/"):
		return ops.ShareKindVideo
	case strings.HasPrefix(m, "audio/"):
		return ops.ShareKindAudio
	}
	return ops.ShareKindFile
}

// Drag sources.

// doDragDrop runs an OLE drag (a modal loop); tests replace it.
var doDragDrop = DoDragDrop

// startDrag starts dragging n, the mouse having moved past the system's
// drag distance with the button down: the press is cancelled, and the
// drag runs once the core has returned.
func (h *Host) startDrag(n *node, hwnd uintptr) {
	h.cancelPress(n)
	n.panning = false
	if Capture() == hwnd {
		n.releasing = true
		ReleaseCapture() // DoDragDrop captures the mouse itself
		n.releasing = false
	}
	id, at := n.id, n.pressAt
	h.afterCore(func() { h.drag(id, at) })
}

// drag runs a drag of view id, grabbed at at (pixels, in it): Dragging
// Began, the drag (DoDragDrop), then Dropped or Cancelled.
func (h *Host) drag(id ops.NodeID, at Point) {
	n := h.nodes[id]
	if n == nil || n.hwnd == 0 || !n.draggable() {
		return
	}
	items := slices.Clone(n.dragItems)
	data, files := h.dragData(id, items)
	if data == 0 {
		return
	}
	defer Release(data)
	h.dragImage(n, data, at)
	// The files the app shares aren't moved away from it.
	allowed := uint32(DROPEFFECT_COPY | DROPEFFECT_MOVE | DROPEFFECT_LINK)
	if files {
		allowed = DROPEFFECT_COPY | DROPEFFECT_LINK
	}
	h.dragSource, h.dragSnapshot = id, items
	h.emit(n, ops.Event{Kind: ops.EventDragging, Int: int64(ops.DragPhaseBegan)})
	h.modal++
	hr, effect := doDragDrop(data, allowed)
	h.modal--
	h.dragSource, h.dragSnapshot = 0, nil
	phase := ops.DragPhaseCancelled
	if hr == DRAGDROP_S_DROP && effect != DROPEFFECT_NONE {
		phase = ops.DragPhaseDropped
	}
	if n := h.nodes[id]; n != nil { // the drop may have removed it
		h.emit(n, ops.Event{Kind: ops.EventDragging, Int: int64(phase)})
	}
	p := CursorPos()
	h.pointerMoved(WindowFromPoint(p), p, true)
}

// dragData is the data object of a drag of view id's items: its files
// (CF_HDROP), its texts (CF_UNICODETEXT; its links' URLs if it has no
// text), its first link (UniformResourceLocatorW), and the private
// format with the process's ID and the view's, which the app's own
// drops know it by. An item that's only a value goes nowhere else.
func (h *Host) dragData(id ops.NodeID, items []ops.Item) (data uintptr, files bool) {
	data = NewDataObject()
	if data == 0 {
		return 0, false
	}
	var paths, texts, links []string
	for _, it := range items {
		switch {
		case it.Path != "":
			paths = append(paths, it.Path)
		case it.Kind == ops.ShareKindLink && it.Text != "":
			links = append(links, it.Text)
		case it.Text != "":
			texts = append(texts, it.Text)
		}
	}
	if len(paths) > 0 {
		SetData(data, CF_HDROP, HDrop(paths))
	}
	text := strings.Join(texts, "\n")
	if text == "" {
		text = strings.Join(links, "\n")
	}
	if text != "" {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
		SetData(data, CF_UNICODETEXT, UTF16Data(text))
	}
	if len(links) > 0 {
		SetData(data, cfURLW(), UTF16Data(links[0]))
		SetData(data, cfURL(), append([]byte(links[0]), 0))
	}
	src := make([]byte, 8)
	le.PutUint32(src, uint32(os.Getpid()))
	le.PutUint32(src[4:], uint32(id))
	SetData(data, cfSource(), src)
	return data, len(paths) > 0
}

// dragImage gives a drag the view's image (as it draws, its rounded
// corners left out), grabbed at at.
func (h *Host) dragImage(n *node, data uintptr, at Point) {
	r := ClientRect(n.hwnd)
	w, ht := r.W(), r.H()
	if w <= 0 || ht <= 0 {
		return
	}
	dc := CreateCompatibleDC(0)
	bmp, px := DIB(w, ht)
	if bmp == 0 {
		DeleteDC(dc)
		return
	}
	old := SelectObject(dc, bmp)
	h.paint(n, n.hwnd, dc, r)
	var kids []uintptr
	for c := NextWindow(n.hwnd, GW_CHILD); c != 0; c = NextWindow(c, GW_HWNDNEXT) {
		if IsWindowVisible(c) {
			kids = append(kids, c)
		}
	}
	for i := len(kids) - 1; i >= 0; i-- {
		h.paintTree(kids[i], n.hwnd, dc)
	}
	SelectObject(dc, old)
	DeleteDC(dc)
	// Opaque (GDI's text leaves alpha 0), but for the rounded corners;
	// premultiplied.
	radius := min(n.cornerRadius*h.scaleOf(n), float32(w)/2, float32(ht)/2)
	for y := range ht {
		for x := range w {
			i := (int(y)*int(w) + int(x)) * 4
			if i+3 >= len(px) {
				break
			}
			if a := cornerAlpha(float32(x)+0.5, float32(y)+0.5, float32(w), float32(ht), radius); a < 255 {
				px[i] = byte(uint32(px[i]) * uint32(a) / 255)
				px[i+1] = byte(uint32(px[i+1]) * uint32(a) / 255)
				px[i+2] = byte(uint32(px[i+2]) * uint32(a) / 255)
				px[i+3] = a
			} else {
				px[i+3] = 255
			}
		}
	}
	if !SetDragImage(data, bmp, w, ht, at) {
		DeleteObject(bmp)
	}
}

// cornerAlpha is the coverage of the pixel centered at x, y by a w by ht
// rectangle with corners of radius r.
func cornerAlpha(x, y, w, ht, r float32) byte {
	if r <= 0 {
		return 255
	}
	cx, cy := x, y
	switch {
	case x < r:
		cx = r
	case x > w-r:
		cx = w - r
	}
	switch {
	case y < r:
		cy = r
	case y > ht-r:
		cy = ht - r
	}
	dx, dy := x-cx, y-cy
	if dx == 0 && dy == 0 {
		return 255
	}
	d := float32(math.Sqrt(float64(dx*dx+dy*dy))) - r // past the edge, in pixels
	switch {
	case d <= -0.5:
		return 255
	case d >= 0.5:
		return 0
	}
	return byte((0.5 - d) * 255)
}
