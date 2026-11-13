//go:build linux && !android

package linux

import (
	"math"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// richNode is the one RichText of h.
func richNode(t *testing.T, h *Host) *node {
	rs := find(h, ops.KindRichText)
	if len(rs) != 1 {
		fatalf(t, "%d rich texts", len(rs))
	}
	return rs[0]
}

// labelSize is what a label draws: its layout's size at its allocation.
func labelSize(l Ptr) (w, ht float32, lines int) {
	var pw, ph int32
	layout := LabelGetLayout(l)
	LayoutGetPixelSize(layout, &pw, &ph)
	return float32(pw), float32(ph), int(Call(Sym("pango_layout_get_line_count"), layout))
}

func near(a, b, tol float32) bool { return math.Abs(float64(a-b)) <= float64(tol) }

// What's measured is what's drawn: styled paragraphs wrapped in a narrow
// column are as tall as the label's layout at its frame's width, and no
// wider than its widest line; new content is measured again.
func TestRichTextMeasure(t *testing.T) {
	text := signal.NewFunc(ui.Rich{
		ui.Strong("Spans"), ui.Plain(" can be "), ui.Em("italic"), ui.Plain(", "),
		{Text: "larger", Font: ui.Font{Size: 22}}, ui.Plain(", "), ui.Code("code"),
		ui.Plain(" or "), ui.Link("links", "https://go.dev"),
		ui.Plain(" and wrap across lines in a narrow column.\nA second paragraph."),
	}, nil)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16), Width: ui.Pt(200), Align: ui.AlignStart},
			ui.RichText(ui.RichTextProps{Text: text}),
		)
	})
	var n *node
	onMain(func() { n = richNode(t, h) })
	settle(t, h, func() bool {
		_, ht, _ := labelSize(n.inner)
		return n.placed && ht > 0 && near(ht, n.frame[3], 1)
	})
	onMain(func() {
		w, ht, lines := labelSize(n.inner)
		if n.frame[2] > 168.5 || w > n.frame[2]+0.5 || lines < 4 {
			fatalf(t, "frame %v; the label draws %vx%v in %d lines", n.frame, w, ht, lines)
		}
		if n.text != "Spans can be italic, larger, code or links and wrap across lines in a narrow column.\nA second paragraph." {
			fatalf(t, "text %q", n.text)
		}
	})
	// Shorter: measured again, one line as wide as it is.
	onMain(func() { h.enter(func() { text.Set(ui.Rich{ui.Plain("Short")}); r.Flush() }) })
	settle(t, h, func() bool {
		w, ht, lines := labelSize(n.inner)
		return n.text == "Short" && lines == 1 && near(ht, n.frame[3], 1) && near(w, n.frame[2], 1.5)
	})
}

// A line limit cuts the text with an ellipsis, and measures as many lines.
func TestRichTextLines(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Width: ui.Pt(160)},
			ui.RichText(ui.RichTextProps{Style: ui.Style{Lines: 2}, Text: ui.Val(ui.Rich{
				ui.Strong("Two lines at most: "),
				ui.Plain("this paragraph goes on long enough to wrap past its second line, where it's cut."),
			})}),
		)
	})
	var n *node
	onMain(func() { n = richNode(t, h) })
	settle(t, h, func() bool {
		_, ht, lines := labelSize(n.inner)
		return n.placed && lines == 2 && near(ht, n.frame[3], 1)
	})
}

// A click on a link reports it with Link; elsewhere there's no link; the
// pointer over a link is a hand.
func TestRichTextLink(t *testing.T) {
	var linked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16), Align: ui.AlignStart},
			ui.RichText(ui.RichTextProps{OnLink: func(u string) { linked = append(linked, u) }, Text: ui.Val(ui.Rich{
				ui.Plain("Read "), ui.Link("the Go site", "https://go.dev"), ui.Plain(" now."),
			})}),
		)
	})
	var n *node
	onMain(func() { n = richNode(t, h) })
	settle(t, h, func() bool { _, ht, _ := labelSize(n.inner); return n.placed && ht > 0 })
	onMain(func() {
		r := richOf(n)
		if len(r.links) != 1 || r.links[0].url != "https://go.dev" {
			fatalf(t, "links %+v", r.links)
		}
		// Across the line: plain, then the link, then plain.
		var seen []int
		for x := 0.0; x < float64(n.frame[2]); x++ {
			k := r.linkAt(n, x, 6)
			if len(seen) == 0 || seen[len(seen)-1] != k {
				seen = append(seen, k)
			}
		}
		if len(seen) != 3 || seen[0] != -1 || seen[1] != 0 || seen[2] != -1 {
			fatalf(t, "links along the line: %v", seen)
		}
		if r.linkAt(n, 1, 6) >= 0 || r.linkAt(n, 1, 200) >= 0 {
			fatalf(t, "a link outside it")
		}
		// The pointer.
		r.hover(n, true)
		if !r.overLink || Call(Sym("gtk_widget_get_cursor"), n.inner) == 0 {
			fatalf(t, "no hand over the link")
		}
		r.hover(n, false)
		// A click on it, as the gesture's release does.
		var x float64
		for x = 0; r.linkAt(n, x, 6) < 0; x++ {
		}
		r.pressed = r.linkAt(n, x+2, 6)
		h.openLink(n, r.links[r.pressed].url)
	})
	settle(t, h, func() bool { return len(linked) == 1 && linked[0] == "https://go.dev" })
}

// An inline component sits on the baseline where its run is: the line
// makes room for it, and its view is placed there.
func TestRichTextInline(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16), Align: ui.AlignStart},
			ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{
				ui.Plain("Before "),
				ui.Embed("badge", func() ui.Node {
					return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(30), Height: ui.Pt(12), Background: ui.Red}})
				}),
				ui.Plain(" after"),
			})}),
		)
	})
	var n *node
	onMain(func() { n = richNode(t, h) })
	settle(t, h, func() bool {
		return n.placed && len(n.children) == 1 && n.children[0].placed && len(richOf(n).inlineAt) == 1
	})
	settle(t, h, func() bool {
		r, c := richOf(n), n.children[0]
		layout := LabelGetLayout(n.inner)
		var ox, oy int32
		Txt.LabelGetLayoutOffsets(n.inner, &ox, &oy)
		var pos PangoRectangle
		Call(Sym("pango_layout_index_to_pos"), layout, uintptr(r.inlineAt[0]), Addr(&pos))
		base := float32(LayoutGetBaseline(layout)) / 1024
		x, y, w, ht := allocation(c.widget)
		wantX := float32(ox) + float32(pos.X)/1024
		return w == 30 && ht == 12 && near(x, wantX, 1) && x > 20 && near(y+ht, float32(oy)+base, 1)
	})
	// The text is as wide as its words and the component.
	onMain(func() {
		w, _, _ := labelSize(n.inner)
		if w < 30+40 || !near(w, n.frame[2], 1.5) {
			fatalf(t, "the label draws %v wide in a frame %v", w, n.frame)
		}
	})
}

// RichTexts in a Selectable are its to select (one selection across
// them, selectable.go); one outside isn't. Their labels aren't
// selectable themselves.
func TestSelectable(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16)},
			ui.Selectable(ui.SelectableProps{},
				ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Select me")})}),
			),
			ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Not me")})}),
		)
	})
	settle(t, h, func() bool {
		rs, ss := find(h, ops.KindRichText), find(h, ops.KindSelectable)
		if len(rs) != 2 || len(ss) != 1 {
			return false
		}
		texts := h.selectTexts(ss[0])
		for _, n := range rs {
			if !n.placed || Call(Sym("gtk_label_get_selectable"), n.inner) != 0 || richOf(n).selectable != (n.text == "Select me") {
				return false
			}
		}
		return len(texts) == 1 && texts[0].text == "Select me"
	})
}
