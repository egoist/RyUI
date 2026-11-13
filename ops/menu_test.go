package ops

import (
	"reflect"
	"testing"
)

// Menus round-trip, numbered depth first across a menu bar's menus.
func TestMenuRoundTrip(t *testing.T) {
	bar := []MenuBarMenu{
		{Title: "File", Items: []MenuItem{
			{Title: "New", Key: "n", Mods: 1},
			{Title: "Open Recent", Items: []MenuItem{
				{Title: "a.txt"},
				{Flags: MenuSeparator},
				{Title: "Clear", Flags: MenuDestructive},
			}},
			{Flags: MenuSeparator},
		}},
		{Title: "App", Items: []MenuItem{{Role: MenuRoleQuit}, {Title: "Off", Flags: MenuDisabled | MenuChecked}}},
	}
	got := DecodeMenuBar(AppendMenuBar(nil, bar))
	want := []MenuBarMenu{
		{Title: "File", Items: []MenuItem{
			{Index: 0, Title: "New", Key: "n", Mods: 1},
			{Index: 1, Title: "Open Recent", Flags: MenuSubmenu, Items: []MenuItem{
				{Index: 2, Title: "a.txt"},
				{Index: 3, Flags: MenuSeparator},
				{Index: 4, Title: "Clear", Flags: MenuDestructive},
			}},
			{Index: 5, Flags: MenuSeparator},
		}},
		{Title: "App", Items: []MenuItem{{Index: 6, Role: MenuRoleQuit}, {Index: 7, Title: "Off", Flags: MenuDisabled | MenuChecked}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	flat := FlattenMenu(got[0].Items)
	for i, it := range flat {
		if it.Index != i {
			t.Fatalf("item %d numbered %d", i, it.Index)
		}
	}
	// A cut menu ends where it's cut.
	b := AppendMenu(nil, bar[0].Items)
	if items := DecodeMenu(b[:len(b)-3]); len(items) != 2 || len(items[1].Items) != 3 {
		t.Fatalf("cut: %+v", items)
	}
}

// Action menus round-trip, numbered depth first, a section or submenu
// before its items.
func TestActionsRoundTrip(t *testing.T) {
	menu := []ActionItem{
		{Title: "Copy", Icon: "doc.on.doc"},
		{Title: "Sort by", Flags: MenuInline, Items: []ActionItem{
			{Title: "Name", Flags: MenuChecked},
			{Title: "Date"},
		}},
		{Title: "Move to", Icon: "folder", Items: []ActionItem{
			{Title: "Inbox", Flags: MenuDisabled},
			{Title: "More", Items: []ActionItem{{Title: "Archive"}}},
		}},
		{Flags: MenuInline}, // an empty section
		{Title: "Delete", Role: AlertButtonRoleDestructive},
	}
	got := DecodeActions(AppendActions(nil, menu))
	want := []ActionItem{
		{Index: 0, Title: "Copy", Icon: "doc.on.doc"},
		{Index: 1, Title: "Sort by", Flags: MenuInline | MenuSubmenu, Items: []ActionItem{
			{Index: 2, Title: "Name", Flags: MenuChecked},
			{Index: 3, Title: "Date"},
		}},
		{Index: 4, Title: "Move to", Icon: "folder", Flags: MenuSubmenu, Items: []ActionItem{
			{Index: 5, Title: "Inbox", Flags: MenuDisabled},
			{Index: 6, Title: "More", Flags: MenuSubmenu, Items: []ActionItem{{Index: 7, Title: "Archive"}}},
		}},
		{Index: 8, Flags: MenuInline | MenuSubmenu},
		{Index: 9, Title: "Delete", Role: AlertButtonRoleDestructive},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	for i, it := range FlattenActions(got) {
		if it.Index != i {
			t.Fatalf("item %d numbered %d", i, it.Index)
		}
	}
	if n := CountActions(got); n != 10 {
		t.Fatalf("count %d", n)
	}
	// A cut menu ends where it's cut; nothing is nothing.
	b := AppendActions(nil, menu)
	if items := DecodeActions(b[:len(b)-3]); len(items) != 4 {
		t.Fatalf("cut: %+v", items)
	}
	if DecodeActions(nil) != nil || len(DecodeActions(AppendActions(nil, nil))) != 0 {
		t.Fatal("empty")
	}
}
