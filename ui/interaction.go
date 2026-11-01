package ui

import (
	"bytes"
	mathbits "math/bits"
	"mime"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// A View's interaction states (pressed, hovered, dragged, a drag over
// it) are reported by its host as they change, and a style follows them
// three ways, from the shortest:
//
//   - its own style's variants (CSS's :active and :hover): Style.Pressed,
//     Hovered, Dragging and DragOver are laid over the rest while it's in
//     them;
//   - an Interaction, the View's states as values, for other views'
//     styles (Tailwind's group-hover and peer-hover) and anything else
//     that follows them: Style.When(it.Hovered, ...) or it.Pressed() in a
//     StyleFunc, a Text, a Show;
//   - the handlers (OnPressedChange, OnHoverChange, a DropTarget's
//     OnOverChange, a DragSource's OnPhase), for what isn't a style: a
//     hold to record.
//
//	card := ui.NewInteraction()
//	ui.View(ui.ViewProps{
//		OnPress:     open,
//		Interaction: card,
//		Style: ui.Style{Padding: ui.All(16), CornerRadius: 12, Background: ui.SecondaryBackground,
//			Hovered: &ui.Style{BorderColor: ui.Accent},
//			Pressed: &ui.Style{Background: ui.Separator}},
//	}, ui.Text(title, ui.Style{}.When(card.Hovered, ui.Style{Color: ui.Accent})))

// Interaction is a View's interaction states (ViewProps.Interaction), as
// values that styles, texts and effects read and follow: Compose's
// InteractionSource, Flutter's WidgetStatesController. Reading one is
// what makes the View track it, from the first read on: a View whose
// Pressed nothing reads doesn't report its pressed state, and keeps the
// platform's highlight. The zero Interaction is ready to use.
//
// Given to several Views, a state is theirs together: pressed while any
// of them is. A View that goes away leaves its states.
type Interaction struct {
	sig   [stateCount]*signal.Signal[bool]
	n     [stateCount]int // the views in each state
	want  uint8           // the states read so far
	views []*element      // the live views it's given to
}

// NewInteraction returns an Interaction for a View.
func NewInteraction() *Interaction { return &Interaction{} }

// Pressed reports whether the View is pressed (CSS's :active): a finger,
// or the mouse's primary button, down on it (OnPressedChange).
func (it *Interaction) Pressed() bool { return it.get(statePressed) }

// Hovered reports whether a pointer is over the View or its children
// (CSS's :hover; OnHoverChange). A touch screen alone never hovers.
func (it *Interaction) Hovered() bool { return it.get(stateHovered) }

// Dragging reports whether the View is being dragged (its Drag), from
// its lift to its drop or cancel.
func (it *Interaction) Dragging() bool { return it.get(stateDragging) }

// DragOver reports whether a drag the View's Drop takes is over it.
func (it *Interaction) DragOver() bool { return it.get(stateOver) }

// get reads a state, tracked by the caller's effect, and has the views
// listen for it from now on.
func (it *Interaction) get(bit uint8) bool {
	if it.want&bit == 0 {
		it.want |= bit
		for _, e := range it.views {
			e.listenState(bit)
		}
	}
	return it.signal(bit).Get()
}

func (it *Interaction) signal(bit uint8) *signal.Signal[bool] {
	i := stateIndex(bit)
	if it.sig[i] == nil {
		it.sig[i] = signal.New(false)
	}
	return it.sig[i]
}

// report counts a view coming into a state or leaving it.
func (it *Interaction) report(bit uint8, on bool) {
	i := stateIndex(bit)
	if on {
		it.n[i]++
	} else if it.n[i] > 0 {
		it.n[i]--
	}
	it.signal(bit).Set(it.n[i] > 0)
}

// apply lays s's variants for the states it's in over s, and drops them.
func (it *Interaction) apply(s Style) Style {
	variants := [...]struct {
		bit uint8
		s   *Style
	}{{stateHovered, s.Hovered}, {statePressed, s.Pressed}, {stateDragging, s.Dragging}, {stateOver, s.DragOver}}
	for _, v := range variants {
		if v.s != nil && it.get(v.bit) {
			s.overlay(v.s)
		}
	}
	s.Hovered, s.Pressed, s.Dragging, s.DragOver = nil, nil, nil, nil
	return s
}

// attach gives it e's states: those e is in now, and from now on.
func (e *element) attach(it *Interaction) {
	h := e.handlers()
	if h.ia == it {
		return
	}
	h.ia = it
	it.views = append(it.views, e)
	e.hookLeave()
	for _, bit := range [...]uint8{statePressed, stateHovered} {
		if it.want&bit != 0 {
			e.listenState(bit)
		}
	}
	for bit := uint8(1); bit < 1<<stateCount; bit <<= 1 {
		if h.state&bit != 0 {
			it.report(bit, true)
		}
	}
}

// interaction is e's Interaction, given (it, if e has none yet) or its
// own, made a view to report its states if it was a flattened one.
func (e *element) interaction(it *Interaction) *Interaction {
	if e.h != nil && e.h.ia != nil {
		return e.h.ia
	}
	if !e.real {
		e.makeReal()
	}
	if it == nil {
		it = NewInteraction()
	}
	e.attach(it)
	return it
}

// listenState has e's view report a state the core reads: pressed or
// hovered (a drag's states come with its Drag and Drop).
func (e *element) listenState(bit uint8) {
	h := e.handlers()
	if e.dead || !e.real || h.listens&bit != 0 {
		return
	}
	switch bit {
	case statePressed:
		e.listen(ops.EventPressed)
	case stateHovered:
		e.listen(ops.EventHover)
	default:
		return
	}
	h.listens |= bit
}

// variantStyle is a View's style with its variants laid on for the
// states it's in: as is if it has none, and an effect that follows them
// if it has (or a StyleFunc's may).
func (e *element) variantStyle(st Styler, it *Interaction) Styler {
	if st == nil {
		return nil
	}
	s, f := st.styler()
	if f == nil {
		if !s.hasVariants() {
			return s
		}
		ia := e.interaction(it)
		return StyleFunc(func() Style { return ia.apply(s) })
	}
	e.hookLeave()
	return StyleFunc(func() Style {
		s := f()
		if !s.hasVariants() {
			return s
		}
		return e.interaction(it).apply(s)
	})
}

// hookLeave has e leave its interaction states when its owner disposes
// it, once.
func (e *element) hookLeave() {
	if e.hooked {
		return
	}
	e.hooked = true
	// A view that goes away while pressed, hovered or under a drag
	// leaves the state: what follows it elsewhere (a group's or a
	// peer's style) doesn't stay stuck.
	signal.OnCleanup(func() {
		if e.h != nil {
			e.h.leave(e)
		}
	})
}

// DragPhase is where a view's drag is (DragSource.OnPhase).
type DragPhase = ops.DragPhase

const (
	// DragBegan: the view lifted and the drag began.
	DragBegan = ops.DragPhaseBegan
	// DragDropped: a drop target took it, the app's or another app's.
	DragDropped = ops.DragPhaseDropped
	// DragCancelled: it ended without a target taking it.
	DragCancelled = ops.DragPhaseCancelled
)

// DragItem is a thing a drag carries: a text, a link, a file, or a value
// of the app's.
type DragItem struct {
	// Kind is what it is. A file's (one with a Path) left SharedText is
	// worked out from its type: SharedImage, SharedVideo, SharedAudio or
	// SharedFile.
	Kind ShareKind
	// Text is a text's text, or a link's URL.
	Text string
	// Path is a file's. In a drop from another app on iOS and Android,
	// it's the app's own copy, in its caches (Drop.Copied): move it
	// where the app keeps it, or remove it when done. Otherwise it's the
	// file itself, where the user keeps it.
	Path string
	// Name is the file's name where it came from ("IMG_0042.HEIC").
	Name string
	// MIME is its type, which drop targets accept it by: "text/plain"
	// for a text and "text/uri-list" for a link unless set, a file's
	// from its name. Give a Value one of the app's own
	// ("application/x-myapp-card"), so its targets take only it.
	MIME string
	// Size is a dropped file's size in bytes, -1 if it isn't known.
	Size int64
	// Value is the app's own, for a drag within the app (a card to
	// another column): the app's drops get it as is. Other apps get the
	// item's text, link or file, if it has one, and nothing else.
	Value any
}

// IsFile reports whether the item is a file, at Path.
func (d DragItem) IsFile() bool { return d.Kind >= SharedImage }

// DragSource makes a view draggable: a long press, then a move, lifts it
// on a touch screen (the long press then fires no OnLongPress); a move
// with the mouse button down does with a mouse (instead of OnPan). The
// platform drags the view's image under the finger or pointer, within
// the app and to other apps.
type DragSource struct {
	// Items are what the drag carries. It runs in an effect: what it
	// reads, changing, changes them. No items: not draggable.
	Items func() []DragItem
	// OnPhase reports the drag: DragBegan as it lifts (a dragged look),
	// then DragDropped (a target took it: for a move, remove what was
	// dragged) or DragCancelled.
	OnPhase func(DragPhase)
}

// DropTarget makes a view take drops: of the app's own drags, and of
// other apps' (files from Finder or Files, a photo from Photos, a link
// from a browser).
type DropTarget struct {
	// Accept are the MIME types it takes: exact ("text/plain"), a
	// family ("image/*"), anything ("*/*"), or the app's own types for
	// its Values. A drag with no item of them never comes over it.
	Accept []string
	// OnOverChange reports a drag it takes coming over the view (true),
	// and leaving it, ended or dropped (false): a drop zone's highlight.
	OnOverChange func(over bool)
	// OnMove reports where the drag is over the view as it moves, in
	// points from its top left: an insertion point.
	OnMove func(x, y float32)
	// OnDrop is called with what's dropped. A drop from another app
	// comes once its items are loaded (files copied), maybe a moment
	// after OnOverChange's false.
	OnDrop func(Drop)
}

// Drop is what's dropped on a DropTarget.
type Drop struct {
	// Items are the drag's items the target takes (its Accept), in order.
	Items []DragItem
	// X, Y is where it was dropped in the view, in points.
	X, Y float32
	// Local: it was dragged from the app (its Values are there).
	Local bool
	// Copied: the files are the app's own copies, in its caches (another
	// app's drop on iOS and Android): move them where the app keeps
	// them, or remove them when done. Otherwise they're the files
	// themselves, where the user keeps them: don't remove them.
	Copied bool
}

// Interaction states the core last reported, so each change is reported
// once and a view going away while in one reports leaving it.
const (
	statePressed uint8 = 1 << iota
	stateHovered
	stateOver
	stateDragging
	stateCount = iota
)

func stateIndex(bit uint8) int { return mathbits.TrailingZeros8(bit) }

// dragHandlers are a draggable view's.
type dragHandlers struct {
	items []DragItem
	phase func(DragPhase)
}

// dropHandlers are a drop target's.
type dropHandlers struct {
	accept []string
	over   func(bool)
	move   func(x, y float32)
	drop   func(Drop)
}

// drags are the items of the drags under way, by the view they're
// dragged from, as they were when the drag began: a drop in the app gets
// them, Values and all.
var drags = map[ops.NodeID][]DragItem{}

// interactive reports whether p asks for any interaction state or drag.
func (p *ViewProps) interactive() bool {
	return p.OnPressedChange != nil || p.OnHoverChange != nil || p.Drag != nil || p.Drop != nil || p.Interaction != nil
}

// listenInteraction wires a view's interaction states and drags.
func (e *element) listenInteraction(p *ViewProps) {
	if !p.interactive() {
		return
	}
	h := e.handlers()
	if p.OnPressedChange != nil {
		h.pressed = p.OnPressedChange
		e.listenState(statePressed)
	}
	if p.OnHoverChange != nil {
		h.hover = p.OnHoverChange
		e.listenState(stateHovered)
	}
	if d := p.Drop; d != nil && len(d.Accept) > 0 {
		h.drop = &dropHandlers{accept: slices.Clone(d.Accept), over: d.OnOverChange, move: d.OnMove, drop: d.OnDrop}
		e.r.batch.SetBytes(e.id, ops.PropViewDropAccept, ops.AppendStrings(nil, d.Accept))
		e.listen(ops.EventDragOver)
		e.listen(ops.EventDrop)
	}
	if d := p.Drag; d != nil && d.Items != nil {
		dh := &dragHandlers{phase: d.OnPhase}
		h.drag = dh
		var sent []byte
		var wire []ops.Item
		signal.RenderEffect(func() {
			items := d.Items()
			wire = wire[:0]
			for i := range items {
				wire = append(wire, wireItem(&items[i]))
			}
			b := ops.AppendDragItems(nil, wire)
			dh.items = items
			if sent != nil && bytes.Equal(b, sent) {
				return
			}
			sent = b
			e.r.batch.SetBytes(e.id, ops.PropViewDragItems, b)
		})
		e.listen(ops.EventDragging)
	}
	if p.Interaction != nil {
		e.attach(p.Interaction)
	}
	e.hookLeave()
}

// leave reports leaving each interaction state e's view is in, as it
// goes away, and takes it from its Interaction.
func (h *handlers) leave(e *element) {
	if h.state&statePressed != 0 {
		h.setState(statePressed, false, h.pressed)
	}
	if h.state&stateHovered != 0 {
		h.setState(stateHovered, false, h.hover)
	}
	if h.state&stateOver != 0 {
		var over func(bool)
		if h.drop != nil {
			over = h.drop.over
		}
		h.setState(stateOver, false, over)
	}
	if h.state&stateDragging != 0 {
		h.setState(stateDragging, false, nil) // OnPhase hears only the platform's end
	}
	if it := h.ia; it != nil {
		if i := slices.Index(it.views, e); i >= 0 {
			it.views = slices.Delete(it.views, i, i+1)
		}
	}
}

// setState records a state's change and reports it, to the handler and
// the view's Interaction, if it changed.
func (h *handlers) setState(bit uint8, on bool, f func(bool)) {
	if (h.state&bit != 0) == on {
		return
	}
	h.state ^= bit
	if h.ia != nil {
		h.ia.report(bit, on)
	}
	if f != nil {
		f(on)
	}
}

// wireItem is a DragItem as a view's DragItems carries it, its kind,
// path and type filled in.
func wireItem(d *DragItem) ops.Item {
	it := ops.Item{Kind: d.Kind, Text: d.Text, Path: d.Path, Name: d.Name, MIME: d.MIME, Value: d.Value != nil}
	if it.Path != "" {
		if abs, err := filepath.Abs(it.Path); err == nil {
			it.Path = abs
		}
		if it.Name == "" {
			it.Name = filepath.Base(it.Path)
		}
		if it.MIME == "" {
			it.MIME = fileMIME(it.Path)
		}
		if it.Kind < SharedImage {
			it.Kind = fileKind(it.MIME)
		}
	}
	if it.MIME == "" {
		switch {
		case it.Kind == SharedLink:
			it.MIME = "text/uri-list"
		case it.Text != "":
			it.MIME = "text/plain"
		default:
			it.MIME = "application/octet-stream"
		}
	}
	return it
}

// fileMIME is a file's MIME type from its name.
func fileMIME(path string) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		return t
	}
	return "application/octet-stream"
}

// fileKind is the ShareKind of a file of a MIME type.
func fileKind(m string) ShareKind {
	switch {
	case strings.HasPrefix(m, "image/"):
		return SharedImage
	case strings.HasPrefix(m, "video/"):
		return SharedVideo
	case strings.HasPrefix(m, "audio/"):
		return SharedAudio
	}
	return SharedFile
}

// interactionEvent delivers an interaction or drag event to its view's
// handlers; ok is false for any other event.
func interactionEvent(e *element, h *handlers, ev *ops.Event) bool {
	switch ev.Kind {
	case ops.EventPressed:
		h.setState(statePressed, ev.Bool, h.pressed)
	case ops.EventHover:
		h.setState(stateHovered, ev.Bool, h.hover)
	case ops.EventDragOver:
		d := h.drop
		if d == nil {
			return true
		}
		switch ev.G.State {
		case ops.GestureStateBegan, ops.GestureStateChanged:
			h.setState(stateOver, true, d.over)
			if d.move != nil {
				d.move(ev.G.X, ev.G.Y)
			}
		default:
			h.setState(stateOver, false, d.over)
		}
	case ops.EventDrop:
		if h.drop != nil {
			h.dropped(ev.Bytes)
		}
	case ops.EventDragging:
		if h.drag == nil {
			return true
		}
		phase := DragPhase(ev.Int)
		if phase == DragBegan {
			for id := range drags {
				if x := elems[id]; x == nil || x.dead {
					delete(drags, id) // a drag whose view went: it ended unheard
				}
			}
			drags[e.id] = slices.Clone(h.drag.items)
		} else {
			delete(drags, e.id)
		}
		h.setState(stateDragging, phase == DragBegan, nil)
		if h.drag.phase != nil {
			h.drag.phase(phase)
		}
	default:
		return false
	}
	return true
}

// dropped delivers a Drop event: the items of a drag from the app are
// the core's own, as they were when it began; another app's come with
// the event.
func (h *handlers) dropped(b []byte) {
	d := h.drop
	p, ok := ops.DecodeDrop(b)
	if !ok {
		return
	}
	// A drop ends the drag over the view, if the host hasn't said so yet.
	h.setState(stateOver, false, d.over)
	drop := Drop{X: p.X, Y: p.Y, Copied: p.Copied}
	if p.Source != 0 {
		drop.Local = true
		items, ok := drags[p.Source]
		if !ok {
			// Its start unheard (or already its end): the source's items now.
			if x := elems[p.Source]; x != nil && !x.dead && x.h != nil && x.h.drag != nil {
				items = x.h.drag.items
			}
		}
		for i := range items {
			w := wireItem(&items[i])
			if ops.MatchMIME(d.accept, w.MIME) {
				drop.Items = append(drop.Items, DragItem{Kind: w.Kind, Text: w.Text, Path: w.Path, Name: w.Name, MIME: w.MIME, Size: -1, Value: items[i].Value})
			}
		}
	} else {
		for _, it := range p.Items {
			if ops.MatchMIME(d.accept, it.MIME) {
				drop.Items = append(drop.Items, DragItem{Kind: it.Kind, Text: it.Text, Path: it.Path, Name: it.Name, MIME: it.MIME, Size: it.Size})
			}
		}
	}
	if len(drop.Items) == 0 || d.drop == nil {
		return
	}
	d.drop(drop)
}
