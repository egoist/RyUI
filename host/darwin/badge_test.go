//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A tab's badge is in its label, in parentheses, and VoiceOver hears it
// after the title, as so many items; it changes the label alone.
func TestTabBadge(t *testing.T) {
	unread := signal.New("3")
	h, r := mount(t, func() ui.Node {
		return ui.Tabs(ui.TabsProps{},
			ui.Tab{Title: ui.Val("Inbox"), Badge: unread, Content: func() ui.Node { return ui.StaticText("a") }},
			ui.Tab{Title: ui.Val("Archive"), Content: func() ui.Node { return ui.StaticText("b") }},
		)
	})
	onMain(func() {
		n := find(h, ops.KindTabs)[0]
		item := n.children[0].nav.tabItem
		check := func(label, said string) {
			t.Helper()
			if got := GoString(Send(item, "label")); got != label {
				fatalf(t, "label %q, want %q", got, label)
			}
			if got := GoString(Send(item, "accessibilityAttributeValue:", uintptr(String("AXTitle")))); got != said {
				fatalf(t, "VoiceOver hears %q, want %q", got, said)
			}
		}
		check("Inbox (3)", "Inbox, 3 items")
		if got := GoString(Send(n.children[1].nav.tabItem, "accessibilityAttributeValue:", uintptr(String("AXTitle")))); got != "Archive" {
			fatalf(t, "a tab without a badge: %q", got)
		}
		h.enter(func() { unread.Set("New"); r.Flush() })
		if Send(n.children[0].nav.tabItem, "self") != item {
			fatal(t, "the tab was made again")
		}
		check("Inbox (New)", "Inbox, New")
		h.enter(func() { unread.Set(""); r.Flush() })
		check("Inbox", "Inbox")
	})
}

// A destination's badge is a count at the row's trailing end, VoiceOver
// hearing it as so many items; it changes in place, the row's view and
// the selection kept.
func TestSidebarBadge(t *testing.T) {
	unread := signal.New("3")
	h, r := mount(t, func() ui.Node {
		return ui.SplitView(ui.SplitViewProps{Sections: []ui.SidebarSection{
			{Title: ui.Val("Mail"), Destinations: []ui.Destination{
				{Title: ui.Val("Inbox"), Icon: "tray", Badge: unread, Content: func() ui.Node { return ui.StaticText("inbox") }},
				{Title: ui.Val("Drafts"), Content: func() ui.Node { return ui.StaticText("drafts") }},
			}},
		}})
	})
	onMain(func() {
		s := find(h, ops.KindSplit)[0].split
		MsgSetRect(s.table, selSetFrame, Rect{Size: Size{Width: 240, Height: 200}})
		cell := Send(s.table, "viewAtColumn:row:makeIfNecessary:", 0, 1, 1)
		badge := sidebarBadge(cell)
		if badge == 0 || GoString(Send(badge, "stringValue")) != "3" || GoString(Send(badge, "accessibilityLabel")) != "3 items" {
			fatal(t, "no badge on Inbox's row")
		}
		MsgSetRect(cell, selSetFrame, Rect{Size: Size{Width: 220, Height: 24}})
		bf, tf := MsgRect(badge, Sel("frame")), MsgRect(Send(cell, "textField"), Sel("frame"))
		if bf.Origin.X+bf.Size.Width > 220 || bf.Origin.X < tf.Origin.X+tf.Size.Width {
			fatalf(t, "badge at %v, title at %v", bf, tf)
		}
		if sidebarBadge(Send(s.table, "viewAtColumn:row:makeIfNecessary:", 0, 2, 1)) != 0 {
			fatal(t, "Drafts has a badge")
		}
		h.enter(func() { unread.Set("12"); r.Flush() })
		if Send(s.table, "viewAtColumn:row:makeIfNecessary:", 0, 1, 0) != cell || GoString(Send(sidebarBadge(cell), "stringValue")) != "12" {
			fatal(t, "the row wasn't changed in place")
		}
		if SendInt(s.table, "selectedRow") != 1 {
			fatal(t, "the selection moved")
		}
		h.enter(func() { unread.Set(""); r.Flush() })
		if sidebarBadge(cell) != 0 {
			fatal(t, "the badge stayed")
		}
	})
}
