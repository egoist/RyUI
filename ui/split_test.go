package ui_test

import (
	"reflect"
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A split view sends its sidebar and shows the chosen destination as its
// stack's first screen; choosing another replaces the stack's screens at
// once, disposing them.
func TestSplitView(t *testing.T) {
	runs := 0
	tick := signal.New(0)
	var nav *ui.Navigator
	h, r := mount(t, func() ui.Node {
		return ui.SplitView(ui.SplitViewProps{Title: ui.Val("Gallery"), Sections: []ui.SidebarSection{
			{Title: ui.Val("Views"), Destinations: []ui.Destination{
				{Title: ui.Val("Controls"), Icon: "slider.horizontal.3", Content: func() ui.Node {
					nav = ui.UseNavigator()
					signal.Effect(func() { tick.Get(); runs++ })
					return ui.Button(ui.ButtonProps{Title: ui.Val("More"), OnPress: func() {
						nav.Push(func() ui.Node { return ui.StaticText("detail") }, ui.ScreenOptions{Title: ui.Val("Detail")})
					}})
				}},
				{Title: ui.Val("Charts"), Content: func() ui.Node { return ui.StaticText("charts") }},
			}},
		}})
	})
	split := h.OfKind(ops.KindSplit)[0]
	if split.String(ops.PropSplitTitle) != "Gallery" || len(split.Props[ops.PropSplitItems].Bytes) == 0 || split.Int(ops.PropSplitSelected) != 0 {
		t.Fatal("no sidebar")
	}
	stack := split.Children[0]
	if stack.Kind != ops.KindStack || len(stack.Children) != 1 || stack.Children[0].String(ops.PropScreenTitle) != "Controls" {
		t.Fatalf("detail:\n%s", split.Tree())
	}
	h.Tap(h.One(ops.KindButton, "More"))
	r.Flush()
	if len(stack.Children) != 2 {
		t.Fatal("no push in the detail")
	}
	h.Select(split, 1)
	r.Flush()
	if len(stack.Children) != 1 || stack.Children[0].String(ops.PropScreenTitle) != "Charts" || h.One(ops.KindText, "charts") == nil {
		t.Fatalf("after choosing Charts:\n%s", split.Tree())
	}
	if len(h.Commands(ops.CmdReplace)) != 1 {
		t.Fatal("the stack wasn't told to replace its screens")
	}
	runs = 0
	tick.Set(1)
	r.Flush()
	if runs != 0 {
		t.Fatal("the old destination's effect still runs")
	}
}

// A destination's badge is sent in the sidebar's Items, which are sent
// again, alone, as it changes; the split view gone, no more.
func TestSplitViewBadges(t *testing.T) {
	unread := signal.New("3")
	shown := signal.New(true)
	h, r := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			return ui.SplitView(ui.SplitViewProps{Sections: []ui.SidebarSection{
				{Destinations: []ui.Destination{
					{Title: ui.Val("Inbox"), Icon: "tray", Badge: unread, Content: func() ui.Node { return ui.StaticText("inbox") }},
					{Title: ui.Val("Drafts"), Content: func() ui.Node { return ui.StaticText("drafts") }},
				}},
				{Title: ui.Val("Mailboxes"), Destinations: []ui.Destination{
					{Title: ui.Val("Work"), Badge: ui.Val("New"), Content: func() ui.Node { return ui.StaticText("work") }},
				}},
			}})
		})
	})
	split := h.OfKind(ops.KindSplit)[0]
	want := []ops.SidebarSection{
		{Items: []ops.SidebarItem{{Title: "Inbox", Icon: "tray", Badge: "3"}, {Title: "Drafts"}}},
		{Title: "Mailboxes", Items: []ops.SidebarItem{{Title: "Work", Badge: "New"}}},
	}
	if got := split.Sidebar(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sidebar %+v", got)
	}
	h.Reset()
	unread.Set("4")
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 || h.Count(ops.OpCreate) != 0 || split.Sidebar()[0].Items[0].Badge != "4" {
		t.Fatalf("a badge changing:\n%s", h.Log())
	}
	h.Reset()
	unread.Set("4")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatal("the same sidebar sent again")
	}
	shown.Set(false)
	r.Flush()
	h.Reset()
	unread.Set("5")
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("a gone sidebar is still sent:\n%s", h.Log())
	}
}
