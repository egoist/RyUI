//go:build darwin && !ios

package darwin

import (
	"fmt"
	"math"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// Interaction states and drag and drop (DESIGN.md, "Interaction states,
// drag and drop").
//
// Presses are the views' own mouse tracking (RyUIView's mouseDown:,
// mouseDragged:, mouseUp:), not a click recognizer: Pressed and Press
// then come from the same events and agree. The recognizers the views
// have (long press, pan, …) don't delay the mouse's events (their
// delaysPrimaryMouseButtonEvents is off), so the view sees the press as
// it happens; one that begins cancels it. Only the innermost pressable
// view under the mouse is pressed: a control (a button, a field) takes
// its clicks itself and never passes them on.
//
// Hover is an NSTrackingArea per view; drops come to RyUIView as an
// NSDraggingDestination, drags from it as an NSDraggingSource.

// dragType is the private pasteboard type of the app's own drags: its
// process's ID and the source view's, "pid:id".
const dragType = "dev.ryui.drag"

// dragDistance is how far the mouse moves with the button down, in
// points, before a draggable view's drag begins (AppKit's own views
// begin theirs at about this distance).
const dragDistance = 3

// AppKit's constants.
const (
	nsEventModifierControl = 1 << 18

	nsDragOperationNone    = 0
	nsDragOperationCopy    = 1
	nsDragOperationLink    = 2
	nsDragOperationGeneric = 4
	nsDragOperationMove    = 16

	nsTrackingMouseEnteredAndExited = 0x01
	nsTrackingActiveInActiveApp     = 0x40
	nsTrackingInVisibleRect         = 0x200
	nsTrackingEnabledDuringDrag     = 0x400

	nsDraggingContextOutsideApplication = 0
)

// mouseState is the primary mouse button's press, from its down to its up.
type mouseState struct {
	active bool
	target *node // the innermost pressable view, nil once cancelled
	inside bool  // the mouse is on target (it's pressed)
	source *node // the innermost draggable view
	down   Point // where it went down, in window coordinates
}

// pressable reports whether a click on n is n's: it presses, long
// presses, reports its pressed state or opens a menu.
func (n *node) pressable() bool {
	return n.listening(ops.EventPress) || n.listening(ops.EventLongPress) || n.listening(ops.EventPressed) ||
		n.menu != 0 && n.menuTrigger == int32(ops.MenuTriggerTap)
}

// alive reports whether n is still one of the core's views.
func (h *Host) alive(n *node) bool { return n != nil && h.nodes[n.id] == n && n.view != 0 }

// mouseDown starts tracking a press on view (the view the click hit, or
// one it passed the click to); false: no view there takes it.
func (h *Host) mouseDown(view, ev ID) bool {
	h.cancelPress()
	h.mouse = mouseState{}
	if uint(Send(ev, "modifierFlags"))&nsEventModifierControl != 0 {
		return false // a secondary click: the context menu
	}
	var target, source *node
	for v := view; v != 0 && (target == nil || source == nil); v = Send(v, "superview") {
		n := h.byView[v]
		if n == nil {
			continue
		}
		if target == nil && n.pressable() {
			target = n
		}
		if source == nil && len(n.dragItems) > 0 {
			source = n
		}
	}
	if target == nil && source == nil {
		return false
	}
	h.mouse = mouseState{active: true, target: target, source: source, down: MsgPoint(ev, Sel("locationInWindow"))}
	if target != nil {
		h.mouse.inside = true
		h.emit(target, ops.Event{Kind: ops.EventPressed, Bool: true})
	}
	return true
}

func (h *Host) mouseDragged(ev ID) bool {
	m := &h.mouse
	if !m.active {
		return false
	}
	loc := MsgPoint(ev, Sel("locationInWindow"))
	if m.source != nil && math.Hypot(loc.X-m.down.X, loc.Y-m.down.Y) >= dragDistance {
		source := m.source
		h.cancelPress()
		h.mouse = mouseState{}
		if h.alive(source) {
			h.beginDrag(source, ev)
		}
		return true
	}
	if t := m.target; t != nil {
		if !h.alive(t) {
			m.target = nil
			return true
		}
		if in := h.mouseIn(t, loc); in != m.inside {
			m.inside = in
			h.emit(t, ops.Event{Kind: ops.EventPressed, Bool: in})
		}
	}
	return true
}

func (h *Host) mouseUp(ev ID) bool {
	m := h.mouse
	if !m.active {
		return false
	}
	h.mouse = mouseState{}
	t := m.target
	if t == nil || !m.inside || !h.alive(t) {
		return true
	}
	if h.mouseIn(t, MsgPoint(ev, Sel("locationInWindow"))) {
		if t.menu != 0 && t.menuTrigger == int32(ops.MenuTriggerTap) && !t.listening(ops.EventPress) {
			h.emit(t, ops.Event{Kind: ops.EventPressed, Bool: false})
			h.popUp(t)
			return true
		}
		h.emit(t, ops.Event{Kind: ops.EventPress})
	}
	if h.alive(t) {
		h.emit(t, ops.Event{Kind: ops.EventPressed, Bool: false})
	}
	return true
}

// mouseIn reports whether a point in the window is on n's view.
func (h *Host) mouseIn(n *node, loc Point) bool {
	p := MsgPointFromID(n.view, Sel("convertPoint:fromView:"), loc, 0)
	b := MsgRect(n.view, Sel("bounds"))
	return p.X >= b.Origin.X && p.Y >= b.Origin.Y && p.X < b.Origin.X+b.Size.Width && p.Y < b.Origin.Y+b.Size.Height
}

// cancelPress ends the press under way without a Press: a gesture, a
// drag or the app going away took the mouse. A drag may still begin.
func (h *Host) cancelPress() {
	m := &h.mouse
	t := m.target
	if t == nil {
		return
	}
	m.target = nil
	if m.inside && h.alive(t) {
		h.emit(t, ops.Event{Kind: ops.EventPressed, Bool: false})
	}
	m.inside = false
}

// viewLeft is called as a view leaves its window or is hidden: it isn't
// pressed or hovered any more.
func (h *Host) viewLeft(n *node) {
	if h.mouse.target == n {
		h.cancelPress()
	}
	h.setHover(n, false)
}

// Hover.

// hoverProbe is where the mouse is in window w, in its coordinates, if
// it's over w in the active app. Tests replace it.
var hoverProbe = func(w ID) (Point, bool) {
	app := Send(Class("NSApplication"), "sharedApplication")
	if w == 0 || !SendBool(app, "isActive") || !SendBool(w, "isVisible") {
		return Point{}, false
	}
	p := MsgPoint(Class("NSEvent"), Sel("mouseLocation"))
	if MsgPointIntInt(Class("NSWindow"), Sel("windowNumberAtPoint:belowWindowWithWindowNumber:"), p, 0) != SendInt(w, "windowNumber") {
		return Point{}, false
	}
	return MsgPointToPoint(w, Sel("convertPointFromScreen:"), p), true
}

// listenHover gives n's view a tracking area: entered and exited, over
// its visible rect, in the active app, during drags too.
func (h *Host) listenHover(n *node) {
	if n.hoverArea != 0 {
		return
	}
	opts := uint(nsTrackingMouseEnteredAndExited | nsTrackingActiveInActiveApp | nsTrackingInVisibleRect | nsTrackingEnabledDuringDrag)
	a := MsgInitTracking(Send(Class("NSTrackingArea"), "alloc"), Sel("initWithRect:options:owner:userInfo:"), Rect{}, opts, n.view, 0)
	Send(n.view, "addTrackingArea:", uintptr(a))
	Release(a)
	n.hoverArea = a
	h.checkHover(n)
}

// setHover reports n hovered, or not, if that changed.
func (h *Host) setHover(n *node, on bool) {
	if n.hovered == on || !n.listening(ops.EventHover) && !n.hovered {
		return
	}
	n.hovered = on
	if h.alive(n) {
		h.emit(n, ops.Event{Kind: ops.EventHover, Bool: on})
	}
}

// checkHover checks, once the core has returned, whether the mouse is
// over n: a view that appears, moves or shows under a still pointer
// gets no entered event.
func (h *Host) checkHover(n *node) {
	if n.hovered || n.hoverArea == 0 {
		return
	}
	h.hoverCheck = append(h.hoverCheck, n)
	if !h.hoverQueued {
		h.hoverQueued = true
		h.afterCore(h.runHoverCheck)
	}
}

func (h *Host) runHoverCheck() {
	h.hoverQueued = false
	q := h.hoverCheck
	h.hoverCheck = nil
	for _, n := range q {
		if n.hovered || !h.alive(n) || !n.listening(ops.EventHover) || SendBool(n.view, "isHiddenOrHasHiddenAncestor") {
			continue
		}
		p, ok := hoverProbe(Send(n.view, "window"))
		if !ok {
			continue
		}
		v := MsgPointFromID(n.view, Sel("convertPoint:fromView:"), p, 0)
		r := MsgRect(n.view, Sel("visibleRect"))
		if v.X >= r.Origin.X && v.Y >= r.Origin.Y && v.X < r.Origin.X+r.Size.Width && v.Y < r.Origin.Y+r.Size.Height {
			h.setHover(n, true)
		}
	}
}

// appActive follows the app's activation: leaving it, nothing is pressed
// or hovered; back, the views under the pointer are.
func (h *Host) appActive(active bool) {
	if !active {
		h.cancelPress()
		h.mouse = mouseState{}
	}
	for _, n := range h.nodes {
		if n.hoverArea == 0 {
			continue
		}
		if active {
			h.checkHover(n)
		} else {
			h.setHover(n, false)
		}
	}
}

// Drop targets.

var draggedTypes ID // the types drop targets register for (retained)

// dragTypes are the types a drop target takes: files, file promises,
// links, texts, images, and the app's own drags.
func dragTypes() ID {
	if draggedTypes != 0 {
		return draggedTypes
	}
	arr := Send(Class("NSMutableArray"), "array")
	for _, t := range []string{"public.file-url", "public.url", "public.utf8-plain-text", dragType} {
		Send(arr, "addObject:", uintptr(String(t)))
	}
	Send(arr, "addObjectsFromArray:", uintptr(Send(Class("NSImage"), "imageTypes")))
	if HasClass("NSFilePromiseReceiver") {
		Send(arr, "addObjectsFromArray:", uintptr(Send(Class("NSFilePromiseReceiver"), "readableDraggedTypes")))
	}
	draggedTypes = Retain(arr)
	return draggedTypes
}

func (h *Host) setDropAccept(n *node, b []byte) {
	n.dropAccept = ops.DecodeStrings(b)
	if len(n.dropAccept) > 0 {
		Send(n.view, "registerForDraggedTypes:", uintptr(dragTypes()))
	} else {
		Send(n.view, "unregisterDraggedTypes")
	}
}

// rep is a way to read a dragged item: as a file, a file promise, image
// data, a link or a text.
type rep struct {
	kind     repKind
	mime     string
	pbType   string // image data's, a text's
	path     string // a file's
	promise  ID     // a file promise's NSFilePromiseReceiver
	item     ID     // its NSPasteboardItem
	linkText bool   // a link, read as text
}

type repKind uint8

const (
	repFile repKind = iota
	repPromise
	repImage
	repLink
	repText
)

// dropState is the drag over the app's drop targets.
type dropState struct {
	seq    int        // the drag's sequence number
	read   bool       // its items' types are read
	local  ops.NodeID // its source view, for the app's own drag
	mimes  [][]string // per item, the types it can be read as
	target *node      // the drop target it's over
	at     Point      // where, last reported
}

// session reads the drag's types, once per drag; a new drag ends the
// last one's time over a target.
func (h *Host) session(info ID) *dropState {
	d := &h.over
	seq := SendInt(info, "draggingSequenceNumber")
	if d.read && d.seq == seq {
		return d
	}
	h.leaveTarget()
	*d = dropState{seq: seq, read: true}
	pb := Send(info, "draggingPasteboard")
	if src, ok := localSource(pb); ok {
		d.local = src
		var items []ops.Item
		if h.drag != nil && h.drag.id == src {
			items = h.drag.items
		} else if n := h.nodes[src]; n != nil {
			items = n.dragItems
		}
		for _, it := range items {
			d.mimes = append(d.mimes, []string{it.MIME})
		}
		return d
	}
	for _, reps := range readDrag(pb) {
		var ms []string
		for _, r := range reps {
			ms = append(ms, r.mime)
		}
		d.mimes = append(d.mimes, ms)
	}
	return d
}

// localSource is the source view of a drag from this process.
func localSource(pb ID) (ops.NodeID, bool) {
	s := GoString(Send(pb, "stringForType:", uintptr(String(dragType))))
	pid, id, ok := strings.Cut(s, ":")
	if !ok || pid != strconv.Itoa(os.Getpid()) {
		return 0, false
	}
	n, err := strconv.ParseUint(id, 10, 32)
	return ops.NodeID(n), err == nil && n != 0
}

// readDrag is another app's dragged items, each the ways it can be read,
// the best first: a file, a file promise, image data, a link, a text.
func readDrag(pb ID) [][]rep {
	var promises []ID
	if HasClass("NSFilePromiseReceiver") {
		promises = ArrayItems(Send(pb, "readObjectsForClasses:options:", uintptr(Array(Class("NSFilePromiseReceiver"))), 0))
	}
	promiseTypes := map[string]bool{}
	if HasClass("NSFilePromiseReceiver") {
		for _, t := range ArrayItems(Send(Class("NSFilePromiseReceiver"), "readableDraggedTypes")) {
			promiseTypes[GoString(t)] = true
		}
	}
	var out [][]rep
	for _, item := range ArrayItems(Send(pb, "pasteboardItems")) {
		types := map[string]bool{}
		var order []string
		for _, t := range ArrayItems(Send(item, "types")) {
			s := GoString(t)
			types[s] = true
			order = append(order, s)
		}
		var reps []rep
		if types["public.file-url"] {
			u := Send(Class("NSURL"), "URLWithString:", uintptr(Send(item, "stringForType:", uintptr(String("public.file-url")))))
			if u != 0 && SendBool(u, "isFileURL") {
				path := GoString(Send(Send(u, "filePathURL"), "path"))
				if path == "" {
					path = GoString(Send(u, "path"))
				}
				reps = append(reps, rep{kind: repFile, mime: urlMIME(u, path), path: path, item: item})
			}
			out = append(out, reps)
			continue
		}
		promised := false
		for _, t := range order {
			if promiseTypes[t] {
				promised = true
				break
			}
		}
		if promised && len(promises) > 0 {
			p := promises[0]
			promises = promises[1:]
			m := "application/octet-stream"
			if fts := ArrayItems(Send(p, "fileTypes")); len(fts) > 0 {
				m = utiMIME(GoString(fts[0]))
			}
			reps = append(reps, rep{kind: repPromise, mime: m, promise: p, item: item})
		}
		for _, t := range order {
			if isImageType(t) {
				reps = append(reps, rep{kind: repImage, mime: utiMIME(t), pbType: t, item: item})
				break
			}
		}
		if types["public.url"] {
			reps = append(reps, rep{kind: repLink, mime: "text/uri-list", pbType: "public.url", item: item})
		}
		if types["public.utf8-plain-text"] {
			reps = append(reps, rep{kind: repText, mime: "text/plain", pbType: "public.utf8-plain-text", item: item})
		} else if types["public.url"] {
			reps = append(reps, rep{kind: repLink, mime: "text/plain", pbType: "public.url", item: item, linkText: true})
		}
		if len(reps) > 0 {
			out = append(out, reps)
		}
	}
	return out
}

// takes reports whether n takes a drag of items of these types.
func (n *node) takes(mimes [][]string) bool {
	for _, ms := range mimes {
		for _, m := range ms {
			if ops.MatchMIME(n.dropAccept, m) {
				return true
			}
		}
	}
	return false
}

// dropTarget is the innermost drop target from view up that takes the
// drag. AppKit gives a drag to the innermost view registered for its
// types, and doesn't pass it on if that view refuses it: the targets
// above it get it here.
func (h *Host) dropTarget(view ID, d *dropState) *node {
	for v := view; v != 0; v = Send(v, "superview") {
		if n := h.byView[v]; n != nil && len(n.dropAccept) > 0 && n.takes(d.mimes) && !SendBool(v, "isHiddenOrHasHiddenAncestor") {
			return n
		}
	}
	return nil
}

// dragOver is draggingEntered: and draggingUpdated: on view.
func (h *Host) dragOver(view, info ID) uint {
	d := h.session(info)
	t := h.dropTarget(view, d)
	loc := MsgPoint(info, Sel("draggingLocation"))
	if t != d.target {
		h.leaveTarget()
		if t != nil {
			d.target = t
			d.at = loc
			h.emitDragOver(t, ops.GestureStateBegan, loc)
		}
	} else if t != nil && loc != d.at {
		d.at = loc
		h.emitDragOver(t, ops.GestureStateChanged, loc)
	}
	if t == nil {
		return nsDragOperationNone
	}
	return dragOperation(d.local != 0, uint(Send(info, "draggingSourceOperationMask")))
}

// dragOperation is the operation a drop target shows: a move for the
// app's own drags, a copy for other apps', or what the source allows.
func dragOperation(local bool, mask uint) uint {
	want := []uint{nsDragOperationCopy, nsDragOperationGeneric, nsDragOperationMove, nsDragOperationLink}
	if local {
		want = []uint{nsDragOperationMove, nsDragOperationGeneric, nsDragOperationCopy, nsDragOperationLink}
	}
	for _, op := range want {
		if mask&op != 0 {
			return op
		}
	}
	return nsDragOperationNone
}

// dragExited is draggingExited: on view. AppKit sends it as the drag
// goes to another view, before that view's draggingEntered:; a drag
// going from a target's view into one of its descendants that hands it
// on to the same target stays over it.
func (h *Host) dragExited(view, info ID) {
	d := &h.over
	if !d.read || d.seq != SendInt(info, "draggingSequenceNumber") || d.target == nil {
		return
	}
	if w := Send(view, "window"); w != 0 {
		frame := Send(Send(w, "contentView"), "superview")
		hit := MsgPointID(frame, Sel("hitTest:"), MsgPoint(info, Sel("draggingLocation")))
		for v := hit; v != 0; v = Send(v, "superview") {
			if SendInt(Send(v, "registeredDraggedTypes"), "count") == 0 {
				continue
			}
			if h.byView[v] != nil && h.dropTarget(v, d) == d.target {
				return
			}
			break
		}
	}
	h.leaveTarget()
}

// leaveTarget reports the drag leaving the target it's over.
func (h *Host) leaveTarget() {
	d := &h.over
	t := d.target
	if t == nil {
		return
	}
	d.target = nil
	if h.alive(t) {
		h.emitDragOver(t, ops.GestureStateCancelled, d.at)
	}
}

func (h *Host) emitDragOver(t *node, s ops.GestureState, loc Point) {
	p := MsgPointFromID(t.view, Sel("convertPoint:fromView:"), loc, 0)
	h.emit(t, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: s, X: float32(p.X), Y: float32(p.Y), Scale: 1}})
}

// performDrop is performDragOperation: on view: the app's own drag drops
// at once, its items the core's; another app's items the target takes
// are read (file promises received into the caches) and then dropped.
func (h *Host) performDrop(view, info ID) bool {
	d := h.session(info)
	t := d.target
	if t == nil {
		t = h.dropTarget(view, d)
	}
	if t == nil || !h.alive(t) {
		return false
	}
	loc := MsgPoint(info, Sel("draggingLocation"))
	p := MsgPointFromID(t.view, Sel("convertPoint:fromView:"), loc, 0)
	drop := ops.Drop{X: float32(p.X), Y: float32(p.Y)}
	d.target = t
	if d.local != 0 {
		drop.Source = d.local
		h.emit(t, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &drop)})
		h.leaveTarget()
		return true
	}
	accept := t.dropAccept
	items, promised := h.readItems(Send(info, "draggingPasteboard"), accept)
	if len(items) == 0 {
		h.leaveTarget()
		return false
	}
	if len(promised) == 0 {
		drop.Items, drop.Copied = finishItems(items)
		h.emit(t, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &drop)})
		h.leaveTarget()
		return true
	}
	h.leaveTarget()
	h.receivePromises(t, drop, items, promised)
	return true
}

// dropItem is a dropped item being read; copied: its file is the app's
// copy.
type dropItem struct {
	ops.Item
	copied bool
	ok     bool
}

// readItems reads the dragged items accept takes, each the first way it
// matches; promised are the indexes of file promises still to receive.
func (h *Host) readItems(pb ID, accept []string) (items []dropItem, promised []promiseItem) {
	for _, reps := range readDrag(pb) {
		for _, r := range reps {
			if !ops.MatchMIME(accept, r.mime) {
				continue
			}
			it := dropItem{Item: ops.Item{MIME: r.mime, Size: -1}, ok: true}
			switch r.kind {
			case repFile:
				it.Kind = fileKind(r.mime)
				it.Path = r.path
				it.Name = filepath.Base(r.path)
				if st, err := os.Stat(r.path); err == nil {
					it.Size = st.Size()
				}
			case repPromise:
				it.ok = false
				promised = append(promised, promiseItem{index: len(items), receiver: r.promise})
			case repImage:
				data := Bytes(Send(r.item, "dataForType:", uintptr(String(r.pbType))))
				path, err := writeDropFile("Image"+utiExt(r.pbType), data)
				if err != nil {
					continue
				}
				it.Kind = ops.ShareKindImage
				it.Path, it.Name, it.Size, it.copied = path, filepath.Base(path), int64(len(data)), true
			case repLink:
				it.Kind = ops.ShareKindLink
				if r.linkText {
					it.Kind = ops.ShareKindText
				}
				it.Text = GoString(Send(r.item, "stringForType:", uintptr(String(r.pbType))))
			case repText:
				it.Kind = ops.ShareKindText
				it.Text = GoString(Send(r.item, "stringForType:", uintptr(String(r.pbType))))
			}
			items = append(items, it)
			break
		}
	}
	return items, promised
}

type promiseItem struct {
	index    int
	receiver ID
}

// finishItems are the items read, and whether their files are all the
// app's copies (only then may the app remove them).
func finishItems(items []dropItem) ([]ops.Item, bool) {
	var out []ops.Item
	files, copies := 0, 0
	for _, it := range items {
		if !it.ok {
			continue
		}
		if it.Path != "" {
			files++
			if it.copied {
				copies++
			}
		}
		out = append(out, it.Item)
	}
	return out, files > 0 && copies == files
}

// receivePromises receives the file promises into the caches, then drops
// the items on t.
func (h *Host) receivePromises(t *node, drop ops.Drop, items []dropItem, promised []promiseItem) {
	dir, err := dropDir()
	if err != nil {
		drop.Items, drop.Copied = finishItems(items)
		h.emit(t, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &drop)})
		return
	}
	pending := len(promised)
	var blocks []objc.Block
	done := func() {
		pending--
		if pending > 0 {
			return
		}
		for _, b := range blocks {
			b.Release()
		}
		if !h.alive(t) {
			return
		}
		drop.Items, drop.Copied = finishItems(items)
		if len(drop.Items) > 0 {
			h.emit(t, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &drop)})
		}
	}
	queue := Send(Class("NSOperationQueue"), "mainQueue")
	for _, p := range promised {
		it := &items[p.index]
		files := len(ArrayItems(Send(p.receiver, "fileTypes")))
		if files == 0 {
			files = 1
		}
		got := 0
		b := NewBlock(func(_ objc.Block, url, errorOrNil ID) {
			WithPool(func() {
				if errorOrNil == 0 && url != 0 && !it.ok {
					path := GoString(Send(url, "path"))
					it.Path, it.Name = path, filepath.Base(path)
					it.MIME = urlMIME(url, path)
					it.Kind = fileKind(it.MIME)
					if st, err := os.Stat(path); err == nil {
						it.Size = st.Size()
					}
					it.copied, it.ok = true, true
				}
				got++
				if got == files {
					done()
				}
			})
		})
		blocks = append(blocks, b)
		Send(p.receiver, "receivePromisedFilesAtDestination:options:operationQueue:reader:",
			uintptr(FileURL(dir)), uintptr(Dict()), uintptr(queue), uintptr(b))
	}
}

// dropDir is a new directory in the app's caches for a drop's files.
func dropDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	app := GoString(Send(Send(Class("NSBundle"), "mainBundle"), "bundleIdentifier"))
	if app == "" {
		app = theHost.appName
	}
	dropSeq++
	dir := filepath.Join(base, app, "Drops", fmt.Sprintf("%d-%d", time.Now().UnixNano(), dropSeq))
	return dir, os.MkdirAll(dir, 0o755)
}

var dropSeq int

func writeDropFile(name string, data []byte) (string, error) {
	dir, err := dropDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, data, 0o644)
}

// MIME types.

func loadUTType() bool {
	if !HasClass("UTType") {
		Dlopen("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers")
	}
	return HasClass("UTType")
}

// utiMIME is a uniform type's MIME type (unknown: application/octet-stream).
func utiMIME(uti string) string {
	if loadUTType() {
		if t := Send(Class("UTType"), "typeWithIdentifier:", uintptr(String(uti))); t != 0 {
			if m := GoString(Send(t, "preferredMIMEType")); m != "" {
				return m
			}
		}
	}
	return "application/octet-stream"
}

// utiExt is a uniform type's file extension, with its dot.
func utiExt(uti string) string {
	if loadUTType() {
		if t := Send(Class("UTType"), "typeWithIdentifier:", uintptr(String(uti))); t != 0 {
			if e := GoString(Send(t, "preferredFilenameExtension")); e != "" {
				return "." + e
			}
		}
	}
	return ""
}

func isImageType(uti string) bool {
	if !loadUTType() {
		return false
	}
	t := Send(Class("UTType"), "typeWithIdentifier:", uintptr(String(uti)))
	img := Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.image")))
	return t != 0 && img != 0 && SendBool(t, "conformsToType:", uintptr(img))
}

// urlMIME is a file's MIME type: its content type's, else its
// extension's (unknown: application/octet-stream).
func urlMIME(u ID, path string) string {
	if loadUTType() {
		vals := Send(u, "resourceValuesForKeys:error:", uintptr(Array(Const(Foundation(), "NSURLContentTypeKey"))), 0)
		if t := Send(vals, "objectForKey:", uintptr(Const(Foundation(), "NSURLContentTypeKey"))); t != 0 {
			if m := GoString(Send(t, "preferredMIMEType")); m != "" {
				return m
			}
		}
		if ext := strings.TrimPrefix(filepath.Ext(path), "."); ext != "" {
			if t := Send(Class("UTType"), "typeWithFilenameExtension:", uintptr(String(ext))); t != 0 {
				if m := GoString(Send(t, "preferredMIMEType")); m != "" {
					return m
				}
			}
		}
	}
	if m := mime.TypeByExtension(filepath.Ext(path)); m != "" {
		if i := strings.IndexByte(m, ';'); i >= 0 {
			m = m[:i]
		}
		return m
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

// dragSource is the app's drag under way.
type dragSource struct {
	id    ops.NodeID
	view  ID // retained until it ends: the session's source
	items []ops.Item
	began bool
}

// beginDrag begins dragging n's items, its image under the mouse: other
// apps get each item's text, link or file, the app's drop targets its
// source view in the private type.
func (h *Host) beginDrag(n *node, ev ID) {
	h.endDrag(nsDragOperationNone) // one that never ended
	items := n.dragItems
	b := MsgRect(n.view, Sel("bounds"))
	img := snapshotView(n.view, b)
	own := String(fmt.Sprintf("%d:%d", os.Getpid(), n.id))
	var dis []ID
	for i, it := range items {
		pbi := Autorelease(New("NSPasteboardItem"))
		switch {
		case it.Path != "":
			Send(pbi, "setString:forType:", uintptr(Send(FileURL(it.Path), "absoluteString")), uintptr(String("public.file-url")))
		case it.Kind == ops.ShareKindLink && it.Text != "":
			Send(pbi, "setString:forType:", uintptr(String(it.Text)), uintptr(String("public.url")))
			Send(pbi, "setString:forType:", uintptr(String(it.Text)), uintptr(String("public.utf8-plain-text")))
		case it.Text != "":
			Send(pbi, "setString:forType:", uintptr(String(it.Text)), uintptr(String("public.utf8-plain-text")))
		}
		Send(pbi, "setString:forType:", uintptr(own), uintptr(String(dragType)))
		di := Autorelease(Send(Send(Class("NSDraggingItem"), "alloc"), "initWithPasteboardWriter:", uintptr(pbi)))
		var contents ID
		if i == 0 {
			contents = img
		}
		MsgRectIDVoid(di, Sel("setDraggingFrame:contents:"), b, contents)
		dis = append(dis, di)
	}
	if len(dis) == 0 {
		return
	}
	h.drag = &dragSource{id: n.id, view: Retain(n.view), items: items}
	s := beginSession(n.view, Array(dis...), ev)
	if s == 0 {
		h.endDrag(nsDragOperationNone)
		return
	}
	Send(s, "setAnimatesToStartingPositionsOnCancelOrFail:", 1)
}

// beginSession begins a dragging session from view, its own source.
// Tests replace it: a session needs the window server.
var beginSession = func(view, items, ev ID) ID {
	return Send(view, "beginDraggingSessionWithItems:event:source:", uintptr(items), uintptr(ev), uintptr(view))
}

// snapshotView is an image of a view as it shows.
func snapshotView(v ID, b Rect) ID {
	if b.Size.Width <= 0 || b.Size.Height <= 0 {
		return 0
	}
	rep := MsgRectToID(v, Sel("bitmapImageRepForCachingDisplayInRect:"), b)
	if rep == 0 {
		return 0
	}
	MsgRectIDVoid(v, Sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
	img := Autorelease(MsgSizeID(Send(Class("NSImage"), "alloc"), Sel("initWithSize:"), b.Size))
	Send(img, "addRepresentation:", uintptr(rep))
	return img
}

// dragMask is the operations the app's drag allows: within the app a
// move, a copy or whatever the target does; outside it a copy, or none
// if every item is only the app's value.
func (h *Host) dragMask(context int) uint {
	if context != nsDraggingContextOutsideApplication {
		return nsDragOperationMove | nsDragOperationCopy | nsDragOperationGeneric
	}
	if h.drag != nil {
		for _, it := range h.drag.items {
			if it.Text != "" || it.Path != "" {
				return nsDragOperationCopy
			}
		}
	}
	return nsDragOperationNone
}

func (h *Host) dragBegan() {
	d := h.drag
	if d == nil || d.began {
		return
	}
	d.began = true
	if n := h.nodes[d.id]; n != nil {
		h.emit(n, ops.Event{Kind: ops.EventDragging, Int: int64(ops.DragPhaseBegan)})
	}
}

// endDrag ends the app's drag: Dropped if a target took it, else
// Cancelled (each once, after Began).
func (h *Host) endDrag(op uint) {
	d := h.drag
	if d == nil {
		return
	}
	h.drag = nil
	defer Release(d.view)
	if !d.began {
		return
	}
	phase := ops.DragPhaseCancelled
	if op != nsDragOperationNone {
		phase = ops.DragPhaseDropped
	}
	if n := h.nodes[d.id]; n != nil {
		h.emit(n, ops.Event{Kind: ops.EventDragging, Int: int64(phase)})
	}
}

// interactionMethods are RyUIView's methods for presses, hover and drag
// and drop.
func interactionMethods() []objc.MethodDef {
	h := func() *Host { return theHost }
	return []objc.MethodDef{
		Method("mouseDown:", func(self ID, _ SEL, ev ID) {
			if !h().mouseDown(self, ev) {
				SendSuper(self, "RyUIView", "mouseDown:", uintptr(ev))
			}
		}),
		Method("mouseDragged:", func(self ID, _ SEL, ev ID) {
			if !h().mouseDragged(ev) {
				SendSuper(self, "RyUIView", "mouseDragged:", uintptr(ev))
			}
		}),
		Method("mouseUp:", func(self ID, _ SEL, ev ID) {
			if !h().mouseUp(ev) {
				SendSuper(self, "RyUIView", "mouseUp:", uintptr(ev))
			}
		}),
		Method("mouseEntered:", func(self ID, _ SEL, ev ID) {
			if n := h().byView[self]; n != nil && n.hoverArea != 0 {
				h().setHover(n, true)
			}
		}),
		Method("mouseExited:", func(self ID, _ SEL, ev ID) {
			if n := h().byView[self]; n != nil && n.hoverArea != 0 {
				h().setHover(n, false)
			}
		}),
		Method("viewDidMoveToWindow", func(self ID, _ SEL) {
			SendSuper(self, "RyUIView", "viewDidMoveToWindow")
			n := h().byView[self]
			if n == nil {
				return
			}
			if Send(self, "window") == 0 {
				h().viewLeft(n)
			} else {
				h().checkHover(n)
			}
		}),
		Method("viewDidHide", func(self ID, _ SEL) {
			SendSuper(self, "RyUIView", "viewDidHide")
			if n := h().byView[self]; n != nil {
				h().viewLeft(n)
			}
		}),
		Method("viewDidUnhide", func(self ID, _ SEL) {
			SendSuper(self, "RyUIView", "viewDidUnhide")
			if n := h().byView[self]; n != nil {
				h().checkHover(n)
			}
		}),

		// NSDraggingDestination.
		Method("draggingEntered:", func(self ID, _ SEL, info ID) uint { return h().dragOver(self, info) }),
		Method("draggingUpdated:", func(self ID, _ SEL, info ID) uint { return h().dragOver(self, info) }),
		Method("draggingExited:", func(self ID, _ SEL, info ID) { h().dragExited(self, info) }),
		Method("wantsPeriodicDraggingUpdates", func(self ID, _ SEL) bool { return false }),
		Method("prepareForDragOperation:", func(self ID, _ SEL, info ID) bool {
			d := h().session(info)
			return d.target != nil || h().dropTarget(self, d) != nil
		}),
		Method("performDragOperation:", func(self ID, _ SEL, info ID) bool { return h().performDrop(self, info) }),
		Method("draggingEnded:", func(self ID, _ SEL, info ID) {
			if d := &h().over; d.read && d.seq == SendInt(info, "draggingSequenceNumber") {
				h().leaveTarget()
			}
		}),

		// NSDraggingSource.
		Method("draggingSession:sourceOperationMaskForDraggingContext:", func(self ID, _ SEL, s ID, context int) uint {
			return h().dragMask(context)
		}),
		Method("draggingSession:willBeginAtPoint:", func(self ID, _ SEL, s ID, p Point) { h().dragBegan() }),
		Method("draggingSession:endedAtPoint:operation:", func(self ID, _ SEL, s ID, p Point, op uint) {
			h().dragBegan() // ended before it began: it did begin
			h().endDrag(op)
		}),
	}
}

// dropInteraction forgets n as it goes: the core reports it leaving its
// states.
func (h *Host) dropInteraction(n *node) {
	if h.mouse.target == n {
		h.mouse.target, h.mouse.inside = nil, false
	}
	if h.mouse.source == n {
		h.mouse.source = nil
	}
	if h.over.target == n {
		h.over.target = nil
	}
	n.hovered = false
	n.hoverArea = 0
}
