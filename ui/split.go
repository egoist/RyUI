package ui

import (
	"bytes"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// Destination is a place a SplitView's sidebar leads to.
type Destination struct {
	Title signal.Accessor[string]
	Icon  Icon
	// Content is the destination's first screen; it can push more with
	// UseNavigator. It's built when the destination is chosen and disposed
	// when another is.
	Content func() Node
	// Options are its first screen's (its bar's buttons, search); the
	// title is the destination's unless they give one.
	Options ScreenOptions
	// Badge is shown at the row's trailing end (SwiftUI's badge on a
	// sidebar row; Mail's unread counts): usually a count, any short
	// text; empty or nil, none. It follows the accessor, updating the row
	// in place. Screen readers say it with the row's title, a count as so
	// many items ("Inbox, 3 items"):
	//
	//   - iOS: a label accessory at the cell's trailing end, as the
	//     system's sidebars show counts.
	//   - Android: a counter at the drawer item's end, as Gmail's are;
	//     TalkBack says Material's badge words ("3 new notifications").
	//   - macOS: the source list's count, as Mail's mailboxes show.
	//   - Windows: Fluent's InfoBadge on the navigation pane's item.
	//   - Linux: a dimmed count at the row's end, as GNOME's sidebars
	//     show one.
	//   - Web: a badge on the row, part of its accessible name.
	Badge signal.Accessor[string]
}

// SidebarSection is a titled group of destinations.
type SidebarSection struct {
	Title        signal.Accessor[string] // nil: none
	Destinations []Destination

	group *rnode // a Router's: the group of routes it shows
}

// SplitViewProps configures a SplitView.
type SplitViewProps struct {
	// Title is the sidebar's (the app's name, say).
	Title    signal.Accessor[string]
	Sections []SidebarSection
	// Selected is the chosen destination, its index among all the
	// sections' destinations: the user's choice writes it, code's shows
	// it. nil: the first, kept by the view.
	Selected *signal.Signal[int]
	Style    Styler

	// routed is a Router's split view: it shows the destinations' screens
	// in the detail stack itself.
	routed *splitRouting
}

// splitRouting is how a Router drives a SplitView: it's given the detail
// stack as it's made, and told of the user's choices.
type splitRouting struct {
	stack  func(st *stackState)
	chosen func(i int)
}

// SplitView is an app's top level navigation, many destinations in a
// sidebar and the chosen one's screens beside it (SwiftUI's
// NavigationSplitView), the way each platform has it:
//
//   - iPad and an iPhone in landscape on a large one: UISplitViewController's
//     sidebar beside the content, which the user can hide.
//   - iPhone: the same split view collapsed into a stack, as iOS does it:
//     the sidebar is the first screen, a list, and choosing a destination
//     pushes it; back goes to the list.
//   - Android: Material's navigation drawer, modal on a phone (opened by the
//     menu button of the first screen's top app bar, or a swipe from the
//     edge, and closed by choosing), standing beside the content on a wide
//     screen.
//   - macOS: a source list in the window's sidebar; Windows: a navigation
//     pane.
//
// The destination's screens are a Stack's: they push with UseNavigator,
// and choosing a destination replaces them at once with its first.
func SplitView(p SplitViewProps) Node {
	e := newElement(ops.KindSplit, true)
	e.setStyle(p.Style, false)
	growByDefault(e)
	r := e.r
	var all []Destination
	for _, s := range p.Sections {
		all = append(all, s.Destinations...)
	}
	signal.RenderEffect(func() {
		b := encodeSidebar(p.Sections)
		if !bytes.Equal(b, e.encoded) {
			e.encoded = b
			r.batch.SetBytes(e.id, ops.PropSplitItems, b)
		}
	})
	bind(p.Title, func(v string) { r.batch.SetString(e.id, ops.PropSplitTitle, v) })
	sel := p.Selected
	if sel == nil {
		sel = signal.New(0)
	}
	detail, st := newStack(nil, ScreenOptions{}, nil)
	if p.routed != nil {
		p.routed.stack(st)
	}
	shown := -1 // the destination the stack shows
	signal.RenderEffect(func() {
		i := sel.Get()
		r.batch.SetInt(e.id, ops.PropSplitSelected, int64(i))
		if p.routed != nil {
			return // the Router shows it
		}
		if i == shown || i < 0 || i >= len(all) {
			return
		}
		shown = i
		d := all[i]
		o := d.Options
		if o.Title == nil {
			o.Title = d.Title
		}
		signal.Untrack(func() struct{} {
			if len(st.screens) == 0 {
				st.push(d.Content, o)
			} else {
				st.reset(d.Content, o)
			}
			return struct{}{}
		})
	})
	e.handlers().selected = func(i int64) {
		sel.Set(int(i))
		if p.routed != nil {
			p.routed.chosen(int(i))
		}
	}
	e.listen(ops.EventSelected)
	e.appendKid(detail.e)
	return Node{e}
}

// encodeSidebar is Split's Items: the sections' titles and their
// destinations' titles, icons and badges, read in the effect that sends
// them.
func encodeSidebar(sections []SidebarSection) []byte {
	get := func(a signal.Accessor[string]) string {
		if a == nil {
			return ""
		}
		return a.Get()
	}
	out := make([]ops.SidebarSection, len(sections))
	for i, s := range sections {
		items := make([]ops.SidebarItem, len(s.Destinations))
		for j, d := range s.Destinations {
			items[j] = ops.SidebarItem{Title: get(d.Title), Icon: string(d.Icon), Badge: get(d.Badge)}
		}
		out[i] = ops.SidebarSection{Title: get(s.Title), Items: items}
	}
	return ops.AppendSidebar(nil, out)
}
