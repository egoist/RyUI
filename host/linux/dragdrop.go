//go:build linux && !android

package linux

import (
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Drag and drop (DESIGN.md, "Interaction states, drag and drop"), with
// GTK 4's own:
//
//   - A view with DragItems has a GtkDragSource: a move with the button
//     down past the drag threshold (a touchscreen's too) starts a drag of
//     a content provider of its items: its files (a GdkFileList, which
//     other apps get as text/uri-list), its texts (a string; its links'
//     URLs if it has no text), its links (text/uri-list), and a private
//     type of the app's, the process's and the view's IDs, so a drag of
//     only values is offered anyway, but no other app takes it. The drag
//     shows the view's image as it lifts. Dragging: Began as it starts,
//     then Dropped (a target took it) or Cancelled.
//   - A view with DropAccept has a GtkDropTargetAsync; GTK offers a drag
//     to the innermost widget under the pointer whose target accepts it,
//     then its ancestors'. A drag is taken if one of its items' MIME types
//     matches a pattern: the app's own drag (gdk_drop_get_drag is the drag
//     the app started) by its items' types; another app's by its text
//     (text/plain), and its URIs, read as it comes over the app (a
//     GdkFileList): files by their names' types, others as links
//     (text/uri-list). DragOver as it comes, moves and leaves; Drop with
//     the source view and no items for the app's own drop (the core has
//     them), with the items the target takes for another app's (its
//     files the files themselves), once they're read.

type dragSrc struct {
	source    Ptr
	items     []ops.Item
	at        [2]float64 // where the drag was grabbed, in the view
	cancelled bool
}

type dropTgt struct {
	target Ptr
	accept []string
	over   Ptr        // the GdkDrop over it (0: none)
	at     [2]float32 // its place in the view, last reported
}

// dropRead is what a drag over the app carries, read from its GdkDrop.
type dropRead struct {
	drop    Ptr // a reference
	local   bool
	mimes   []string // its items' MIME types, as far as known
	hasText bool
	hasURIs bool
	loading bool // its URIs are being read
	files   []string
	links   []string
	waiting []func() // a drop waiting for the URIs
}

var (
	dragSources = map[*node]*dragSrc{}
	dropTargets = map[*node]*dropTgt{}
	dropReads   = map[Ptr]*dropRead{}

	// The app's drag under way.
	appDrag struct {
		drag   Ptr
		source ops.NodeID
		items  []ops.Item
	}
)

// dragTypeLocal is the private type the app's drags carry.
const dragTypeLocal = "application/x-ryui-drag"

func init() {
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if n.kind != ops.KindView {
			return false // another kind's prop of the same number (Split's Items)
		}
		switch p {
		case ops.PropViewDragItems:
			h.setDragItems(n, ops.DecodeDragItems(v.Bytes))
		case ops.PropViewDropAccept:
			h.setDropAccept(n, ops.DecodeStrings(v.Bytes))
		default:
			return false
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		delete(dragSources, n)
		if t := dropTargets[n]; t != nil {
			t.over = 0
			delete(dropTargets, n)
		}
	})
}

// draggable reports whether a drag of n starts a drag and drop (which
// comes before a pan).
func draggable(n *node) bool { return dragSources[n] != nil }

// Drag sources.

func (h *Host) setDragItems(n *node, items []ops.Item) {
	s := dragSources[n]
	if len(items) == 0 || n.widget == 0 {
		if s != nil {
			WidgetRemoveController(n.widget, s.source)
			delete(dragSources, n)
		}
		return
	}
	if s == nil {
		s = &dragSrc{source: Draw.DragSourceNew()}
		dragSources[n] = s
		src := s.source
		Connect(src, "prepare", func(x, y float64) uintptr {
			if dragSources[n] != s {
				return 0
			}
			s.at = [2]float64{x, y}
			return dragContent(n.id, s.items)
		})
		Connect(src, "drag-begin", func(drag uintptr) {
			h.dragBegan(n, s, drag)
		})
		Connect(src, "drag-cancel", func(drag, reason uintptr) bool {
			s.cancelled = true
			return false
		})
		Connect(src, "drag-end", func(drag, deleteData uintptr) {
			h.dragEnded(n, s, drag)
		})
		WidgetAddController(n.widget, src)
	}
	s.items = items
	// The files the app shares aren't moved away from it.
	actions := uint32(ActionCopy | ActionMove | ActionLink)
	for _, it := range items {
		if it.Path != "" {
			actions = ActionCopy | ActionLink
			break
		}
	}
	Draw.DragSourceSetActions(s.source, actions)
}

func (h *Host) dragBegan(n *node, s *dragSrc, drag Ptr) {
	appDrag.drag = drag
	appDrag.source = n.id
	appDrag.items = slices.Clone(s.items)
	s.cancelled = false
	// The view's image as it is now (its Dragging style comes after).
	if n.widget != 0 {
		live := WidgetPaintableNew(n.widget)
		img := Draw.PaintableGetCurrentImage(live)
		ObjectUnref(live)
		if img != 0 {
			Draw.DragSourceSetIcon(s.source, img, int32(s.at[0]), int32(s.at[1]))
			ObjectUnref(img)
		}
	}
	h.endPress(n) // the drag takes the press
	if p := &n.ev().pan; p.active {
		p.active = false
		h.emitPan(n, ops.GestureStateCancelled, p.lastDX, p.lastDY)
	}
	h.emit(n, ops.Event{Kind: ops.EventDragging, Int: int64(ops.DragPhaseBegan)})
}

func (h *Host) dragEnded(n *node, s *dragSrc, drag Ptr) {
	phase := ops.DragPhaseDropped
	if s.cancelled || Draw.DragGetSelectedAction(drag) == 0 {
		phase = ops.DragPhaseCancelled
	}
	if appDrag.drag == drag {
		appDrag.drag, appDrag.source, appDrag.items = 0, 0, nil
	}
	if h.nodes[n.id] == n { // the drop may have removed it
		h.emit(n, ops.Event{Kind: ops.EventDragging, Int: int64(phase)})
	}
}

// dragContent is the content of a drag of view id's items (a
// GdkContentProvider, full): its files, its texts (its links' if it has
// none), its links, and the app's private type. An item that's only a
// value goes nowhere else.
func dragContent(id ops.NodeID, items []ops.Item) Ptr {
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
	var ps []Ptr
	if len(paths) > 0 {
		files := make([]Ptr, len(paths))
		for i, p := range paths {
			files[i] = Draw.FileNewForPath(p)
		}
		list := Draw.FileListNewFromArray(&files[0], uintptr(len(files)))
		for _, f := range files {
			ObjectUnref(f)
		}
		var v Value
		ValueInit(&v, Draw.FileListGetType())
		Draw.ValueTakeBoxed(&v, list)
		ps = append(ps, Draw.ContentProviderNewForValue(&v))
		ValueUnset(&v)
	}
	if len(links) > 0 && len(paths) == 0 {
		ps = append(ps, providerBytes("text/uri-list", []byte(strings.Join(links, "\r\n")+"\r\n")))
	}
	text := strings.Join(texts, "\n")
	if text == "" {
		text = strings.Join(links, "\n")
	}
	if text != "" {
		var v Value
		ValueInit(&v, TypeString)
		ValueSetString(&v, text)
		ps = append(ps, Draw.ContentProviderNewForValue(&v))
		ValueUnset(&v)
	}
	ps = append(ps, providerBytes(dragTypeLocal, []byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(int(id)))))
	return Draw.ContentProviderNewUnion(&ps[0], uintptr(len(ps)))
}

func providerBytes(mimeType string, b []byte) Ptr {
	gb := BytesNew(&b[0], uintptr(len(b)))
	p := Draw.ContentProviderNewForBytes(mimeType, gb)
	BytesUnref(gb)
	return p
}

// Drop targets.

func (h *Host) setDropAccept(n *node, accept []string) {
	t := dropTargets[n]
	if len(accept) == 0 || n.widget == 0 {
		if t != nil {
			h.leaveDrop(n, t)
			WidgetRemoveController(n.widget, t.target)
			delete(dropTargets, n)
		}
		return
	}
	if t == nil {
		t = &dropTgt{target: Draw.DropTargetAsyncNew(0, ActionCopy|ActionMove|ActionLink)}
		dropTargets[n] = t
		tg := t.target
		Connect(tg, "accept", func(drop uintptr) bool {
			if dropTargets[n] != t {
				return false
			}
			r := h.readDrop(drop)
			if r.takes(t.accept) || r.loading {
				return true
			}
			forgetDropSoon(drop)
			return false
		})
		Connect(tg, "drag-enter", func(drop uintptr, x, y float64) uintptr {
			return uintptr(h.dragOver(n, t, drop, x, y))
		})
		Connect(tg, "drag-motion", func(drop uintptr, x, y float64) uintptr {
			return uintptr(h.dragOver(n, t, drop, x, y))
		})
		Connect(tg, "drag-leave", func(drop uintptr) {
			h.leaveDrop(n, t)
			forgetDropSoon(drop)
		})
		Connect(tg, "drop", func(drop uintptr, x, y float64) bool {
			return h.dropped(n, t, drop, x, y)
		})
		WidgetAddController(n.widget, tg)
	}
	t.accept = accept
}

// readDrop is what a drag carries: the app's own drag's items' types, or
// another app's formats, its URIs read as it comes.
func (h *Host) readDrop(drop Ptr) *dropRead {
	if r := dropReads[drop]; r != nil {
		return r
	}
	r := &dropRead{drop: ObjectRef(drop)}
	dropReads[drop] = r
	formats := Draw.DropGetFormats(drop)
	if d := Draw.DropGetDrag(drop); appDrag.drag != 0 && (d == appDrag.drag || d == 0 && Draw.ContentFormatsContainMIMEType(formats, dragTypeLocal)) {
		r.local = true
		for _, it := range appDrag.items {
			r.mimes = append(r.mimes, it.MIME)
		}
		return r
	}
	for _, m := range MIMETypes(formats) {
		switch {
		case strings.HasPrefix(m, "text/plain"):
			r.hasText = true
		case m == "text/uri-list":
			r.hasURIs = true
		}
	}
	if Draw.ContentFormatsContainGType(formats, TypeString) {
		r.hasText = true
	}
	if Draw.ContentFormatsContainGType(formats, Draw.FileListGetType()) {
		r.hasURIs = true
	}
	r.describe()
	if r.hasURIs {
		r.loading = true
		Async(func(c AsyncCall) {
			Draw.DropReadValueAsync(drop, Draw.FileListGetType(), PriorityDefault, 0, c.Callback, c.Data)
		}, func(source, res Ptr) {
			var gerr Ptr
			v := Draw.DropReadValueFinish(drop, res, &gerr)
			if gerr != 0 {
				ErrorFree(gerr)
			}
			if v != 0 {
				if list := ValueGetBoxed(At[Value](v)); list != 0 {
					l := Draw.FileListGetFiles(list)
					for _, f := range SList(l) {
						if p := GoStringFree(Draw.FileGetPath(f)); p != "" {
							r.files = append(r.files, p)
						} else if u := GoStringFree(Draw.FileGetURI(f)); u != "" {
							r.links = append(r.links, u)
						}
					}
					Draw.SListFree(l)
				}
			}
			r.loading = false
			r.describe()
			w := r.waiting
			r.waiting = nil
			for _, f := range w {
				f()
			}
		})
	}
	return r
}

// describe sets another app's drag's MIME types from what's known.
func (r *dropRead) describe() {
	r.mimes = r.mimes[:0]
	for _, p := range r.files {
		r.mimes = append(r.mimes, fileMIMEType(p))
	}
	if len(r.links) > 0 {
		r.mimes = append(r.mimes, "text/uri-list")
	}
	if r.hasText {
		r.mimes = append(r.mimes, "text/plain")
	}
}

// takes reports whether a target accepting these patterns takes the
// drag.
func (r *dropRead) takes(accept []string) bool {
	for _, m := range r.mimes {
		if ops.MatchMIME(accept, m) {
			return true
		}
	}
	return false
}

// forgetDropSoon forgets what a drag carried once no target has it (it
// left them all, or was dropped).
func forgetDropSoon(drop Ptr) {
	IdleAdd(PriorityDefaultIdle, func() bool {
		for _, t := range dropTargets {
			if t.over == drop {
				return false
			}
		}
		if r := dropReads[drop]; r != nil && !r.loading && len(r.waiting) == 0 {
			delete(dropReads, drop)
			ObjectUnref(r.drop)
		}
		return false
	})
}

// dragOver reports a drag over n at x, y (Began as it comes, Changed as
// it moves) and returns the action a drop there would take: a move for
// the app's own drags, a copy for other apps', else what the source
// allows; none if n doesn't take it.
func (h *Host) dragOver(n *node, t *dropTgt, drop Ptr, x, y float64) uint32 {
	r := h.readDrop(drop)
	if dropTargets[n] != t || !r.takes(t.accept) {
		h.leaveDrop(n, t)
		return 0
	}
	at := [2]float32{float32(x), float32(y)}
	switch {
	case t.over != drop:
		t.over, t.at = drop, at
		h.emit(n, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: ops.GestureStateBegan, X: at[0], Y: at[1], Scale: 1}})
	case at != t.at:
		t.at = at
		h.emit(n, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: ops.GestureStateChanged, X: at[0], Y: at[1], Scale: 1}})
	}
	return dropAction(r, Draw.DropGetActions(drop))
}

func dropAction(r *dropRead, allowed uint32) uint32 {
	prefer := [3]uint32{ActionCopy, ActionMove, ActionLink}
	if r.local {
		prefer = [3]uint32{ActionMove, ActionCopy, ActionLink}
	}
	for _, a := range prefer {
		if allowed&a != 0 {
			return a
		}
	}
	return 0
}

// leaveDrop ends the drag over n: Cancelled.
func (h *Host) leaveDrop(n *node, t *dropTgt) {
	if t.over == 0 {
		return
	}
	t.over = 0
	h.emit(n, ops.Event{Kind: ops.EventDragOver, G: ops.Gesture{State: ops.GestureStateCancelled, X: t.at[0], Y: t.at[1], Scale: 1}})
}

// dropped takes a drop on n at x, y: the app's own at once, another
// app's once what n takes of it is read.
func (h *Host) dropped(n *node, t *dropTgt, drop Ptr, x, y float64) bool {
	r := h.readDrop(drop)
	if dropTargets[n] != t {
		return false
	}
	finish := func() bool {
		if !r.takes(t.accept) {
			h.leaveDrop(n, t)
			Draw.DropFinish(drop, 0)
			forgetDropSoon(drop)
			return false
		}
		action := dropAction(r, Draw.DropGetActions(drop))
		t.at = [2]float32{float32(x), float32(y)}
		h.leaveDrop(n, t)
		d := ops.Drop{X: float32(x), Y: float32(y)}
		if r.local {
			d.Source = appDrag.source
			Draw.DropFinish(drop, action)
			forgetDropSoon(drop)
			if h.nodes[n.id] == n {
				h.emit(n, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
			}
			return true
		}
		h.droppedItems(r, t.accept, func(items []ops.Item) {
			Draw.DropFinish(drop, action)
			forgetDropSoon(drop)
			d.Items = items
			if h.nodes[n.id] == n {
				h.emit(n, ops.Event{Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
			}
		})
		return true
	}
	if r.loading {
		r.waiting = append(r.waiting, func() { finish() })
		return true
	}
	return finish()
}

// droppedItems loads the items of another app's drop that a target
// accepting accept takes: its files, its links and its text (read now),
// then calls done with them.
func (h *Host) droppedItems(r *dropRead, accept []string, done func([]ops.Item)) {
	var items []ops.Item
	for _, p := range r.files {
		m := fileMIMEType(p)
		if !ops.MatchMIME(accept, m) {
			continue
		}
		size := int64(-1)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			size = st.Size()
		}
		items = append(items, ops.Item{Kind: fileShareKind(m), Path: p, Name: filepath.Base(p), MIME: m, Size: size})
	}
	if ops.MatchMIME(accept, "text/uri-list") {
		for _, l := range r.links {
			items = append(items, ops.Item{Kind: ops.ShareKindLink, Text: l, MIME: "text/uri-list", Size: -1})
		}
	}
	if !r.hasText || !ops.MatchMIME(accept, "text/plain") {
		done(items)
		return
	}
	drop := r.drop
	Async(func(c AsyncCall) {
		Draw.DropReadValueAsync(drop, TypeString, PriorityDefault, 0, c.Callback, c.Data)
	}, func(source, res Ptr) {
		var gerr Ptr
		v := Draw.DropReadValueFinish(drop, res, &gerr)
		if gerr != 0 {
			ErrorFree(gerr)
		}
		if v != 0 {
			s := GoString(ValueGetString(At[Value](v)))
			// A browser's link comes as its text too: once.
			if s != "" && !slices.Contains(r.links, strings.TrimSpace(s)) && !(len(r.files) > 0 && isFileURIs(s)) {
				items = append(items, ops.Item{Kind: ops.ShareKindText, Text: strings.ReplaceAll(s, "\r\n", "\n"), MIME: "text/plain", Size: -1})
			}
		}
		done(items)
	})
}

// isFileURIs reports whether a text is a file manager's list of the
// files it drags, which it offers as text too.
func isFileURIs(s string) bool {
	for _, l := range strings.Fields(s) {
		if !strings.HasPrefix(l, "file://") && !strings.HasPrefix(l, "/") {
			return false
		}
	}
	return true
}

// fileMIMEType is a file's MIME type from its name.
func fileMIMEType(path string) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		return strings.TrimSpace(t)
	}
	return "application/octet-stream"
}

// fileShareKind is the ShareKind of a file of a MIME type.
func fileShareKind(m string) ops.ShareKind {
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
