//go:build darwin && !ios

package darwin

import (
	"fmt"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// actionMenuTitles lists a menu's items: a separator as "-", a disabled item
// in brackets, a checked one with a ✓, a submenu with its items after a
// ">", a section header with a "#".
func actionMenuTitles(m ID) []string {
	var out []string
	for i := 0; i < int(SendInt(m, "numberOfItems")); i++ {
		it := Send(m, "itemAtIndex:", uintptr(i))
		if SendBool(it, "isSeparatorItem") {
			out = append(out, "-")
			continue
		}
		s := GoString(Send(it, "title"))
		if RespondsTo(it, "isSectionHeader") && SendBool(it, "isSectionHeader") {
			s = "#" + s
		} else if !SendBool(it, "isEnabled") {
			s = "[" + s + "]"
		}
		if SendInt(it, "state") == 1 {
			s += "✓"
		}
		if sub := Send(it, "submenu"); sub != 0 {
			s += fmt.Sprint(">", actionMenuTitles(sub))
		}
		out = append(out, s)
	}
	return out
}

// A button's menu shows its items' sections, submenus, checkmarks and
// disabled items; a nested item's click reports its number.
func TestRichMenu(t *testing.T) {
	sort := signal.New("name")
	var picked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("More"), Menu: []ui.Action{
			{Title: ui.Val("Copy")},
			ui.MenuSection(ui.Val("Sort by"),
				ui.Action{Title: ui.Val("Name"), Checked: signal.Func(func() bool { return sort.Get() == "name" }), OnPress: func() { sort.Set("name") }},
				ui.Action{Title: ui.Val("Date"), Checked: signal.Func(func() bool { return sort.Get() == "date" }), OnPress: func() { sort.Set("date") }},
			),
			ui.Submenu(ui.Val("Move to"), "", ui.Action{Title: ui.Val("Archive"), OnPress: func() { picked = append(picked, "archive") }}),
			{Title: ui.Val("Delete"), Role: ui.ActionDestructive, Disabled: ui.Val(true)},
		}})
	})
	onMain(func() {
		b := find(h, ops.KindButton)[0]
		got := fmt.Sprint(actionMenuTitles(b.menu))
		header := "[Sort by]"
		if RespondsTo(Class("NSMenuItem"), "sectionHeaderWithTitle:") {
			header = "#Sort by"
		}
		if want := fmt.Sprint([]string{"Copy", "-", header, "Name✓", "Date", "-", "Move to>[Archive]", "[Delete]"}); got != want {
			fatalf(t, "menu %s, want %s", got, want)
		}
		// The submenu's item, clicked: its number reaches the core.
		sub := Send(Send(b.menu, "itemWithTitle:", uintptr(String("Move to"))), "submenu")
		h.enter(func() { h.menuItem(Send(sub, "itemAtIndex:", 0)) })
		if fmt.Sprint(picked) != "[archive]" {
			fatalf(t, "picked %v", picked)
		}
		// A checked item follows its signal.
		h.enter(func() { h.menuItem(Send(b.menu, "itemWithTitle:", uintptr(String("Date")))) })
		if got := fmt.Sprint(actionMenuTitles(b.menu)); got != fmt.Sprint([]string{"Copy", "-", header, "Name", "Date✓", "-", "Move to>[Archive]", "[Delete]"}) {
			fatalf(t, "after choosing Date: %s", got)
		}
	})
}

// A bar button with children opens them as a menu; a disabled one is
// dimmed.
func TestBarButtonMenu(t *testing.T) {
	var picked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("Home") }, ui.ScreenOptions{Title: ui.Val("Home"), Actions: []ui.Action{
			{Title: ui.Val("Add"), Icon: ui.IconPlus, Children: []ui.Action{
				{Title: ui.Val("Note"), OnPress: func() { picked = append(picked, "note") }},
				{Title: ui.Val("Folder"), OnPress: func() { picked = append(picked, "folder") }},
			}},
			{Title: ui.Val("Edit"), Disabled: ui.Val(true)},
		}})
	})
	onMain(func() {
		items := Send(h.main.chrome.toolbar, "items")
		var add, edit ID
		for i := 0; i < int(SendInt(items, "count")); i++ {
			it := Send(items, "objectAtIndex:", uintptr(i))
			switch GoString(Send(it, "label")) {
			case "Add":
				add = it
			case "Edit":
				edit = it
			}
		}
		if add == 0 || edit == 0 {
			fatal(t, "no Add or Edit")
		}
		m := Send(add, "menu")
		if got := fmt.Sprint(actionMenuTitles(m)); got != "[Note Folder]" {
			fatalf(t, "Add's menu %s", got)
		}
		if SendBool(edit, "isEnabled") {
			fatal(t, "Edit is enabled")
		}
		h.enter(func() { h.menuItem(Send(m, "itemAtIndex:", 1)) })
		if fmt.Sprint(picked) != "[folder]" {
			fatalf(t, "picked %v", picked)
		}
	})
}
