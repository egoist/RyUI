package ui

import (
	"bytes"
	"encoding/binary"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
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
}

// SidebarSection is a titled group of destinations.
type SidebarSection struct {
	Title        signal.Accessor[string] // nil: none
	Destinations []Destination
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
		if !bytes.Equal(b, e.sidebar) {
			e.sidebar = b
			r.batch.SetBytes(e.id, ops.PropSplitItems, b)
		}
	})
	bind(p.Title, func(v string) { r.batch.SetString(e.id, ops.PropSplitTitle, v) })
	sel := p.Selected
	if sel == nil {
		sel = signal.New(0)
	}
	detail, st := newStack(nil, ScreenOptions{}, nil)
	shown := -1 // the destination the stack shows
	signal.RenderEffect(func() {
		i := sel.Get()
		r.batch.SetInt(e.id, ops.PropSplitSelected, int64(i))
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
	e.handlers().selected = func(i int64) { sel.Set(int(i)) }
	e.listen(ops.EventSelected)
	e.appendKid(detail.e)
	return Node{e}
}

// encodeSidebar is Split's Items: the sections' titles and their
// destinations' titles and icons, read in the effect that sends them.
func encodeSidebar(sections []SidebarSection) []byte {
	str := func(b []byte, s string) []byte {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(sections)))
	for _, s := range sections {
		t := ""
		if s.Title != nil {
			t = s.Title.Get()
		}
		b = str(b, t)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s.Destinations)))
		for _, d := range s.Destinations {
			t := ""
			if d.Title != nil {
				t = d.Title.Get()
			}
			b = str(b, t)
			b = str(b, string(d.Icon))
		}
	}
	return b
}
