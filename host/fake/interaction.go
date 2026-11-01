package fake

import (
	"fmt"

	"github.com/egoist/ryui/ops"
)

// The interaction states and drags of a view, as a platform reports
// them. The helpers report each the way the hosts do, in their order.

// SetPressed reports v pressed, or not any more, as a finger going down
// on it and lifting does.
func (h *Host) SetPressed(v *View, pressed bool) {
	h.listening(v, ops.EventPressed)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventPressed, Bool: pressed})
}

// Click presses v and lets go, as a finger tapping it does: Pressed,
// Press, then Pressed off (each if v listens for it).
func (h *Host) Click(v *View) {
	if v.Listens[ops.EventPressed] {
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventPressed, Bool: true})
	}
	if v.Listens[ops.EventPress] {
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventPress})
	}
	if v.Listens[ops.EventPressed] {
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventPressed, Bool: false})
	}
}

// SetHovered reports a pointer coming over v, or leaving it.
func (h *Host) SetHovered(v *View, hovered bool) {
	h.listening(v, ops.EventHover)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventHover, Bool: hovered})
}

// DragItems decodes v's DragItems: what dragging it carries.
func (v *View) DragItems() []ops.Item {
	return ops.DecodeDragItems(v.Props[ops.PropViewDragItems].Bytes)
}

// DropAccept decodes v's DropAccept: the MIME types it takes.
func (v *View) DropAccept() []string { return ops.DecodeStrings(v.Props[ops.PropViewDropAccept].Bytes) }

// Takes reports whether target takes any of items, as a host decides
// before it reports a drag over it.
func (v *View) Takes(items []ops.Item) bool {
	accept := v.DropAccept()
	for _, it := range items {
		if ops.MatchMIME(accept, it.MIME) {
			return true
		}
	}
	return false
}

// DragOver reports a drag over target (Began as it comes over, Changed
// as it moves, Cancelled as it leaves), at x, y in it. It panics if
// target takes no drops.
func (h *Host) DragOver(target *View, state ops.GestureState, x, y float32) {
	if len(target.DropAccept()) == 0 {
		panic(fmt.Sprintf("fake: view %d takes no drops", target.ID))
	}
	h.listening(target, ops.EventDragOver)
	h.dispatch(ops.Event{ID: target.ID, Kind: ops.EventDragOver, G: ops.Gesture{State: state, X: x, Y: y, Scale: 1}})
}

// DropItems drops another app's items on target at x, y, as a host does
// once it has loaded them (copied: its files are the app's copies), and
// the drag leaves it. Items it doesn't take go too: the core picks.
func (h *Host) DropItems(target *View, x, y float32, copied bool, items ...ops.Item) {
	if !target.Takes(items) {
		panic(fmt.Sprintf("fake: view %d takes none of the items", target.ID))
	}
	h.DragOver(target, ops.GestureStateBegan, x, y)
	d := ops.Drop{X: x, Y: y, Copied: copied, Items: items}
	h.dispatch(ops.Event{ID: target.ID, Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
	h.DragOver(target, ops.GestureStateCancelled, x, y)
}

// SetDragPhase reports a drag of source at a phase, as the host does as
// it lifts (DragPhaseBegan) and as it ends: to see a drag under way.
func (h *Host) SetDragPhase(source *View, phase ops.DragPhase) {
	h.listening(source, ops.EventDragging)
	h.dispatch(ops.Event{ID: source.ID, Kind: ops.EventDragging, Int: int64(phase)})
}

// Drag drags source onto target and drops it at x, y in it, as the
// hosts report a drag within the app: Dragging Began on source;
// DragOver Began, Changed, a Drop (its items the core's) and Cancelled
// on target, if it takes any of source's items; then Dragging Dropped
// on source, or Cancelled if target took none. A nil target drops it
// nowhere.
func (h *Host) Drag(source, target *View, x, y float32) {
	items := source.DragItems()
	if len(items) == 0 {
		panic(fmt.Sprintf("fake: view %d isn't draggable", source.ID))
	}
	h.listening(source, ops.EventDragging)
	h.dispatch(ops.Event{ID: source.ID, Kind: ops.EventDragging, Int: int64(ops.DragPhaseBegan)})
	phase := ops.DragPhaseCancelled
	if target != nil && target.Takes(items) {
		h.DragOver(target, ops.GestureStateBegan, x, y)
		h.DragOver(target, ops.GestureStateChanged, x, y)
		d := ops.Drop{X: x, Y: y, Source: source.ID}
		h.dispatch(ops.Event{ID: target.ID, Kind: ops.EventDrop, Bytes: ops.AppendDrop(nil, &d)})
		h.DragOver(target, ops.GestureStateCancelled, x, y)
		phase = ops.DragPhaseDropped
	}
	if h.Views[source.ID] != nil { // the drop may have removed it
		h.dispatch(ops.Event{ID: source.ID, Kind: ops.EventDragging, Int: int64(phase)})
	}
}
