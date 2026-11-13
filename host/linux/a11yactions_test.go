//go:build linux && !android

package linux

import (
	"reflect"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ui"
)

// Titles as GAction names: what AT-SPI lists of a view's actions.
func TestA11yActionNames(t *testing.T) {
	got := a11yActionNames([]string{"Reply", "Mark as read", " Move to…/Archive ", "回复", "Reply", "a.b"})
	want := []string{"Reply", "Mark-as-read", "Move-to-Archive", "4", "Reply-2", "a-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}

// listActions are an action group's actions.
func listActions(g Ptr) []string {
	strv := Call(Sym("g_action_group_list_actions"), g)
	var out []string
	for p := strv; *At[Ptr](p) != 0; p += 8 {
		out = append(out, GoString(*At[Ptr](p)))
	}
	Call(Sym("g_strfreev"), strv)
	return out
}

// A view's actions are its "action" group, which AT-SPI's Action
// interface lists and does; the view says it has a popup, and its key.
// A Selectable's are Copy and Select All.
func TestA11yActionsExposed(t *testing.T) {
	did := ""
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.View(ui.ViewProps{Label: ui.Val("Mail"), Style: ui.Style{Height: ui.Pt(40)}, Actions: []ui.A11yAction{
				{Name: ui.Val("Mark as read"), Do: func() { did = "read" }},
				{Name: ui.Val("Delete"), Do: func() { did = "delete" }},
			}}),
		)
	})
	var mail *node
	settle(t, h, func() bool {
		for n, a := range a11yViews {
			if len(a.actions) == 2 {
				mail = n
			}
		}
		return mail != nil && WidgetGetMapped(mail.widget)
	})
	onMain(func() {
		a := a11yViews[mail]
		names := listActions(a.group)
		if len(names) != 2 || !(names[0] == "Mark-as-read" && names[1] == "Delete" || names[1] == "Mark-as-read" && names[0] == "Delete") {
			fatalf(t, "the group's actions: %q", names)
		}
		// What AT-SPI's DoAction does: activate the widget's action.
		if !WidgetActivateAction(mail.widget, "action.Mark-as-read", 0) {
			fatalf(t, "no action.Mark-as-read on the view")
		}
	})
	settle(t, h, func() bool { return did == "read" })
}

func TestSelectableActions(t *testing.T) {
	h, s, texts := selectableFixture(t)
	onMain(func() {
		a := selectAreas[s]
		if Call(Sym("g_action_get_enabled"), a.copy) != 0 {
			fatalf(t, "Copy is enabled without a selection")
		}
		if !WidgetActivateAction(s.widget, "selection.select-all", 0) {
			fatalf(t, "no selection.select-all")
		}
		wantBounds(t, texts, [2]int{0, 10}, [2]int{0, 23}, [2]int{0, 12})
		if Call(Sym("g_action_get_enabled"), a.copy) == 0 {
			fatalf(t, "Copy isn't enabled with a selection")
		}
		ClipboardSetText(DisplayGetClipboard(DisplayGetDefault()), "")
		if !WidgetActivateAction(s.widget, "selection.copy", 0) {
			fatalf(t, "no selection.copy")
		}
	})
	var clip string
	onMain(func() { h.readClipboardText(func(s string, ok bool) { clip = s }) })
	settle(t, h, func() bool {
		if clip == "" {
			h.readClipboardText(func(s string, ok bool) { clip = s })
		}
		return clip == "Alpha beta\nGamma delta\nSecond line\nEpsilon zeta"
	})
}
