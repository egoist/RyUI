//go:build linux && !android

package linux

import (
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Screen readers (Orca) read GTK's accessibility tree over AT-SPI. A
// node's view takes its role (a pressable view a button, a header a
// heading), its label (A11yLabel; a text's text) and description (the
// hint, else the tooltip), and is hidden with A11yHidden.

const (
	a11yPropDescription = 1 // GTK_ACCESSIBLE_PROPERTY_DESCRIPTION
	a11yPropLabel       = 4 // GTK_ACCESSIBLE_PROPERTY_LABEL
	a11yStateHidden     = 4 // GTK_ACCESSIBLE_STATE_HIDDEN
)

var roleType GType

// setRole sets a view's accessible role, while GTK still lets it (until
// the accessibility context is realized, as the view first shows).
func setRole(w Ptr, nick string) {
	if roleType == 0 {
		roleType = Call(Sym("gtk_accessible_role_get_type"))
	}
	v, ok := EnumValue(roleType, nick)
	if !ok {
		return
	}
	var val Value
	ValueInit(&val, roleType)
	ValueSetEnum(&val, v)
	ObjectSetProperty(w, "accessible-role", &val)
	ValueUnset(&val)
}

func (h *Host) setA11y(n *node, p ops.PropID, v *ops.Value) {
	if n.widget == 0 {
		return
	}
	switch p {
	case ops.PropA11yHint:
		setA11yString(n.widget, a11yPropDescription, v.Str)
	case ops.PropA11yRole:
		nick := ""
		switch ops.Role(v.Int) {
		case ops.RoleButton:
			nick = "button"
		case ops.RoleHeader:
			nick = "heading"
		case ops.RoleImage:
			nick = "img"
		case ops.RoleLink:
			nick = "link"
		case ops.RoleText:
			nick = "label"
		case ops.RoleSearch:
			nick = "search"
		}
		if nick != "" {
			setRole(n.widget, nick)
		}
	case ops.PropA11yHidden:
		var val Value
		ValueInit(&val, TypeBoolean)
		ValueSetBoolean(&val, v.Int != 0)
		state := int32(a11yStateHidden)
		AccessibleUpdateState(n.widget, 1, &state, &val)
		ValueUnset(&val)
	}
}

func setA11yString(w Ptr, prop int32, s string) {
	var val Value
	ValueInit(&val, TypeString)
	ValueSetString(&val, s)
	AccessibleUpdateProperty(w, 1, &prop, &val)
	ValueUnset(&val)
}

// updateA11y labels a view: its A11yLabel, a text's text.
func (h *Host) updateA11y(n *node) {
	if n.widget == 0 {
		return
	}
	label := n.a11yLabel
	if label == "" && n.kind == ops.KindText {
		return // the label inside says it
	}
	if label == "" && n.kind == ops.KindSymbol {
		label = symbolLabels[n.symbol]
	}
	if label != "" {
		setA11yString(n.widget, a11yPropLabel, label)
	}
	if n.events != nil && n.events.key != 0 && n.kind == ops.KindView {
		setRole(n.widget, "button")
	}
}

// announce has a screen reader read text out.
func (h *Host) announce(text string) {
	if AccessibleAnnounce != nil && h.main != nil {
		AccessibleAnnounce(h.main.window, text, 1) // GTK_ACCESSIBLE_ANNOUNCEMENT_PRIORITY_MEDIUM
	}
}
