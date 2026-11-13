package tray_test

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/tray"
	"github.com/egoist/asuka/ui"
)

// An icon follows its signals, sends only what changed, reports clicks
// and chosen items (in submenus too), and goes with its owner.
func TestIcon(t *testing.T) {
	h := fake.New()
	tr := h.Tray()
	shown := signal.New(true)
	title := signal.New("3")
	clicks, opened, shared := 0, 0, ""
	r := ui.Mount(h, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			tray.Show(tray.Props{
				Symbol:  ui.Val("bolt.fill"),
				Title:   title,
				Tooltip: ui.Val("Power"),
				OnClick: func() { clicks++ },
				Menu: signal.Func(func() []ui.MenuItem {
					return []ui.MenuItem{
						{Title: "Open", OnPress: func() { opened++ }},
						{Title: "Share", Submenu: []ui.MenuItem{
							{Title: "Mail", OnPress: func() { shared = "mail" }},
							{Title: "Messages", Checked: title.Get() == "4", OnPress: func() { shared = "messages" }},
						}},
						ui.MenuSeparator,
						ui.MenuQuit,
					}
				}),
			})
			return ui.StaticText("x")
		})
	})
	defer r.Dispose()
	icons := tr.List()
	if len(icons) != 1 || tr.Sets != 1 {
		t.Fatalf("icons %+v, %d sets", icons, tr.Sets)
	}
	ic := icons[0]
	if ic.Symbol != "bolt.fill" || ic.Title != "3" || ic.Tooltip != "Power" || !ic.Clickable || ic.Template {
		t.Fatalf("icon %+v", ic)
	}
	flat := ops.FlattenMenu(ic.Menu)
	if len(flat) != 6 || flat[1].Title != "Share" || !flat[1].Is(ops.MenuSubmenu) || flat[3].Title != "Messages" ||
		flat[5].Role != ops.MenuRoleQuit || flat[3].Index != 3 {
		t.Fatalf("menu %+v", flat)
	}
	if !tr.Following() {
		t.Fatal("not waiting for what happens")
	}

	tr.Click(ic.ID)
	tr.Choose(ic.ID, 0)
	tr.Choose(ic.ID, 3)
	tr.Choose(ic.ID, 5) // the host's own: nothing
	if clicks != 1 || opened != 1 || shared != "messages" {
		t.Fatalf("clicks %d, opened %d, shared %q", clicks, opened, shared)
	}
	if !tr.Following() {
		t.Fatal("stopped waiting")
	}

	// A change sends the icon again, once.
	title.Set("4")
	r.Flush()
	if tr.Sets != 2 || tr.Icons[ic.ID].Title != "4" || !ops.FlattenMenu(tr.Icons[ic.ID].Menu)[3].Is(ops.MenuChecked) {
		t.Fatalf("after a change: %d sets, %+v", tr.Sets, tr.Icons[ic.ID])
	}
	r.Flush()
	if tr.Sets != 2 {
		t.Fatal("sent again without a change")
	}

	// Disposed, it goes, and the core stops waiting.
	shown.Set(false)
	r.Flush()
	h.RunPosted()
	if len(tr.Icons) != 0 || tr.Removes != 1 || tr.Following() {
		t.Fatalf("after dispose: %d icons, %d removes, following %v", len(tr.Icons), tr.Removes, tr.Following())
	}
	// And comes back.
	shown.Set(true)
	r.Flush()
	if len(tr.Icons) != 1 || !tr.Following() {
		t.Fatalf("shown again: %d icons, following %v", len(tr.Icons), tr.Following())
	}
	tr.Click(tr.List()[0].ID)
	if clicks != 2 {
		t.Fatalf("clicks %d", clicks)
	}
}

// The Dock menu: the last mounted shows, the one before when it's
// disposed, none when none is.
func TestDockMenu(t *testing.T) {
	h := fake.New()
	tr := h.Tray()
	inner := signal.New(true)
	chose := ""
	r := ui.Mount(h, func() ui.Node {
		tray.DockMenu(ui.Val([]ui.MenuItem{{Title: "New Window", OnPress: func() { chose = "new" }}}))
		return ui.Show(inner, func() ui.Node {
			tray.DockMenu(ui.Val([]ui.MenuItem{
				{Title: "Status", Submenu: []ui.MenuItem{{Title: "Away", OnPress: func() { chose = "away" }}}},
			}))
			return ui.StaticText("x")
		})
	})
	defer r.Dispose()
	if len(tr.Dock) != 1 || tr.Dock[0].Title != "Status" || len(tr.Dock[0].Items) != 1 {
		t.Fatalf("dock %+v", tr.Dock)
	}
	tr.Choose(0, 1)
	if chose != "away" {
		t.Fatalf("chose %q", chose)
	}
	inner.Set(false)
	r.Flush()
	if len(tr.Dock) != 1 || tr.Dock[0].Title != "New Window" {
		t.Fatalf("dock %+v", tr.Dock)
	}
	tr.Choose(0, 0)
	if chose != "new" {
		t.Fatalf("chose %q", chose)
	}
}

// Without a tray (a phone, the web), Show and DockMenu do nothing, and
// the core stops asking.
func TestNoTray(t *testing.T) {
	h := fake.New()
	r := ui.Mount(h, func() ui.Node {
		tray.Show(tray.Props{Symbol: ui.Val("bolt")})
		tray.DockMenu(ui.Val([]ui.MenuItem{{Title: "New"}}))
		return ui.StaticText("x")
	})
	defer r.Dispose()
	h.RunPosted() // "no module": the answers
	h.Reset()
	h.RunPosted()
	r.Flush()
	if n := h.Count(ops.OpCommand); n != 0 {
		t.Fatalf("%d commands: still asking", n)
	}
}
