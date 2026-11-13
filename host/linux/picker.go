//go:build linux && !android

package linux

import (
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Picker is a GtkDropDown over a GtkStringList of its items, as GNOME's
// apps choose one of many. GTK shows nothing while none is chosen, so its
// label is a dim label over the drop-down's button where the chosen item
// would be (as a multiline field's placeholder is), shown only then; it
// names the drop-down for screen readers too.

type picker struct {
	drop  Ptr // the GtkDropDown
	label Ptr // its label while none is chosen
	text  string
}

// pickerPad is where Adwaita's button puts its content: its padding.
const pickerPad = 10

var pickers = map[*node]*picker{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindPicker {
			return false
		}
		h.makePicker(n)
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		pk := pickers[n]
		if pk == nil {
			return false
		}
		switch p {
		case ops.PropPickerItems:
			n.items = ops.DecodeStrings(v.Bytes)
			n.updating = true
			model := StringListNew(nil)
			for _, s := range n.items {
				StringListAppend(model, s)
			}
			DropDownSetModel(pk.drop, model)
			ObjectUnref(model)
			n.updating = false
			h.selectPicker(n, pk)
		case ops.PropPickerSelected:
			n.selected = int(v.Int)
			h.selectPicker(n, pk)
		case ops.PropPickerLabel:
			pk.text = v.Str
			LabelSetText(pk.label, v.Str)
			if n.a11yLabel == "" {
				setA11yString(pk.drop, a11yPropLabel, v.Str)
			}
		default:
			return false
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		delete(pickers, n)
	})
}

func (h *Host) makePicker(n *node) {
	pk := &picker{drop: DropDownNewFromStrings(nil)}
	pk.label = LabelNew("")
	WidgetAddCSSClass(pk.label, "dim-label")
	LabelSetEllipsize(pk.label, EllipsizeEnd)
	LabelSetXAlign(pk.label, 0)
	WidgetSetCanTarget(pk.label, false) // clicks go to the drop-down
	WidgetSetHAlign(pk.label, AlignStart)
	WidgetSetVAlign(pk.label, AlignCenter)
	Txt.WidgetSetMarginStart(pk.label, pickerPad)
	Txt.WidgetSetMarginEnd(pk.label, pickerPad)
	WidgetSetVisible(pk.label, false)
	ov := OverlayNew()
	OverlaySetChild(ov, pk.drop)
	OverlayAddOverlay(ov, pk.label)
	// The label is part of its size: a short item's drop-down still fits it.
	OverlaySetMeasureOverlay(ov, pk.label, true)
	n.selected = -1 // none, till the core says
	pickers[n] = pk
	h.setInner(n, ov)
	d := pk.drop
	n.handlers = append(n.handlers, Connect(d, "notify::selected", func(uintptr) {
		if n.updating {
			return
		}
		i := DropDownGetSelected(d)
		if i == invalidPosition {
			return
		}
		n.selected = int(i)
		WidgetSetVisible(pk.label, false)
		h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(n.selected)})
	}))
}

// invalidPosition is GTK_INVALID_LIST_POSITION: no item.
const invalidPosition = ^uint32(0)

// selectPicker selects the chosen item, or none (GTK would choose the
// first), and shows the label then.
func (h *Host) selectPicker(n *node, pk *picker) {
	none := n.selected < 0 || n.selected >= len(n.items)
	n.updating = true
	if none {
		DropDownSetSelected(pk.drop, invalidPosition)
	} else {
		DropDownSetSelected(pk.drop, uint32(n.selected))
	}
	n.updating = false
	WidgetSetVisible(pk.label, none)
}
