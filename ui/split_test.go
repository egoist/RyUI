package ui_test

import (
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
