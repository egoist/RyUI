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
