//go:build linux && !android

package linux

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// selectableFixture mounts a Selectable of three texts (the second two
// paragraphs) and a link, and returns it and its texts.
func selectableFixture(t *testing.T) (*Host, *node, []*node) {
	h, _ := mount(t, func() ui.Node {
		return ui.Selectable(ui.SelectableProps{Style: ui.Style{Padding: ui.All(16), Gap: 12, Width: ui.Pt(400)}},
			ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Alpha beta")})}),
			ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Gamma delta\nSecond line")})}),
			ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Plain("Epsilon "), ui.Link("zeta", "https://example.com")})}),
		)
	})
	var s *node
	var texts []*node
	settle(t, h, func() bool {
		ss := find(h, ops.KindSelectable)
		if len(ss) != 1 {
			return false
		}
		s = ss[0]
		texts = h.selectTexts(s)
		return len(texts) == 3 && texts[2].placed && richOf(texts[2]).text != ""
	})
	return h, s, texts
}

// charAt is the point in s's view a little inside character c of t's
// text (its leading edge's side).
func charAt(s, t *node, c int) (float64, float64) {
	text := richOf(t).text
	b := len(string([]rune(text)[:c]))
	var r PangoRectangle
	Sel.LayoutIndexToPos(LabelGetLayout(t.inner), int32(b), &r)
	var ox, oy int32
	Txt.LabelGetLayoutOffsets(t.inner, &ox, &oy)
	in := Point{X: float32(ox) + float32(r.X)/1024 + float32(r.W)/1024/4, Y: float32(oy) + float32(r.Y)/1024 + float32(r.H)/1024/2}
	var p Point
	WidgetComputePoint(t.inner, s.widget, &in, &p)
	return float64(p.X), float64(p.Y)
}

func selBounds(t *node) [2]int { return textSelections[t] }

func wantBounds(t *testing.T, texts []*node, want ...[2]int) {
	t.Helper()
	for i, tx := range texts {
		if got := selBounds(tx); got != want[i] {
			fatalf(t, "text %d (%q) selects %v, want %v", i, richOf(tx).text, got, want[i])
		}
	}
}

// A drag selects across texts, a part of each, and copies them as
// paragraphs; double and triple clicks select words and paragraphs;
// Shift+click extends; Ctrl+A selects all; a link's press is the link's.
func TestSelectableAcrossTexts(t *testing.T) {
	h, s, texts := selectableFixture(t)
	onMain(func() {
		// From "beta" in the first to "Eps" in the third.
		x, y := charAt(s, texts[0], 6)
		h.selectPress(s, x, y, 1, false)
		x2, y2 := charAt(s, texts[2], 3)
		h.selectTo(s, h.selectHit(s, x2, y2))
		wantBounds(t, texts, [2]int{6, 10}, [2]int{0, 23}, [2]int{0, 3})
		// The second text's part is its two lines: a box each, drawn.
		boxes := 0
		LayoutRangeBoxes(LabelGetLayout(texts[1].inner), 0, int32(len(richOf(texts[1]).text)), func(r IntRect) {
			if r.W > 0 && r.H > 0 {
				boxes++
			}
		})
		if boxes != 2 {
			fatalf(t, "%d boxes for two lines", boxes)
		}
		if got := h.selectedText(s); got != "beta\nGamma delta\nSecond line\nEps" {
			fatalf(t, "selected %q", got)
		}
		// Backwards from the anchor: from "beta"'s start to "pha".
		x3, y3 := charAt(s, texts[0], 2)
		h.selectTo(s, h.selectHit(s, x3, y3))
		wantBounds(t, texts, [2]int{2, 6}, [2]int{}, [2]int{})

		// Ctrl+C copies it.
		if !h.selectKey(s, 0x63, ModControl) {
			fatalf(t, "Ctrl+C copied nothing")
		}
	})
	var clip string
	onMain(func() { h.readClipboardText(func(s string, ok bool) { clip = s }) })
	settle(t, h, func() bool { return clip == "pha " })

	onMain(func() {
		// A double click selects "delta", a triple click its paragraph.
		x, y := charAt(s, texts[1], 8)
		h.selectPress(s, x, y, 2, false)
		wantBounds(t, texts, [2]int{}, [2]int{6, 11}, [2]int{})
		// A drag after it extends by words: into "Epsilon".
		x2, y2 := charAt(s, texts[2], 2)
		h.selectTo(s, h.selectHit(s, x2, y2))
		wantBounds(t, texts, [2]int{}, [2]int{6, 23}, [2]int{0, 7})
		h.selectPress(s, x, y, 3, false)
		wantBounds(t, texts, [2]int{}, [2]int{0, 11}, [2]int{})

		// A click, then Shift+click extends by characters.
		x0, y0 := charAt(s, texts[0], 1)
		h.selectPress(s, x0, y0, 1, false)
		wantBounds(t, texts, [2]int{}, [2]int{}, [2]int{})
		x4, y4 := charAt(s, texts[1], 3)
		h.selectPress(s, x4, y4, 1, true)
		wantBounds(t, texts, [2]int{1, 10}, [2]int{0, 3}, [2]int{})

		// Ctrl+A: all of every text.
		if !h.selectKey(s, 0x61, ModControl) {
			fatalf(t, "Ctrl+A")
		}
		wantBounds(t, texts, [2]int{0, 10}, [2]int{0, 23}, [2]int{0, 12})
		if got := h.selectedText(s); got != "Alpha beta\nGamma delta\nSecond line\nEpsilon zeta" {
			fatalf(t, "selected %q", got)
		}

		// Below every text: the end of the last; above: the first's start.
		var b Rect
		WidgetComputeBounds(texts[2].inner, s.widget, &b)
		if p := h.selectHit(s, 10, float64(b.Y+b.H)+20); p.t != texts[2] || p.off != 12 {
			fatalf(t, "below: %v %d", p.t == texts[2], p.off)
		}
		if p := h.selectHit(s, 300, 2); p.t != texts[0] || p.off != 0 {
			fatalf(t, "above: %v %d", p.t == texts[0], p.off)
		}

		// A press on the link isn't the selection's; beside it is.
		lx, ly := charAt(s, texts[2], 9)
		if h.selectTakes(s, lx, ly, 1) {
			fatalf(t, "the selection took a press on a link")
		}
		ex, ey := charAt(s, texts[2], 2)
		if !h.selectTakes(s, ex, ey, 1) || !h.selectTakes(s, lx, ly, 3) {
			fatalf(t, "the selection didn't take a press on its text")
		}
	})
}

// Pango's words: a word, the white space between, punctuation alone.
func TestSelectableWordAt(t *testing.T) {
	onMain(func() {
		l := LabelNew("Hello,  world")
		ObjectRefSink(l)
		defer ObjectUnref(l)
		attrs := LogAttrs(LabelGetLayout(l))
		for _, c := range []struct{ at, s, e int }{{0, 0, 5}, {3, 0, 5}, {5, 5, 6}, {6, 6, 8}, {7, 6, 8}, {9, 8, 13}, {12, 8, 13}} {
			if s, e := wordAt(attrs, 13, c.at); s != c.s || e != c.e {
				fatalf(t, "at %d: %d..%d, want %d..%d", c.at, s, e, c.s, c.e)
			}
		}
	})
}

// With the mouse (xdotool, through the X server): the Selectable's drag
// takes the press before the labels do, and selects across them; a
// double click selects a word.
func TestSelectableMouse(t *testing.T) {
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("no xdotool to move the mouse")
	}
	h, s, texts := selectableFixture(t)
	settle(t, h, func() bool { return WidgetGetMapped(texts[2].inner) })
	var xid uintptr
	screen := func(t2 *node, c int) [2]int {
		var out [2]int
		onMain(func() {
			win := h.main.window
			native := WidgetGetNative(win)
			xid = Call(Sym("gdk_x11_surface_get_xid"), NativeGetSurface(native))
			var sx, sy float64
			Call(Sym("gtk_native_get_surface_transform"), native, Addr(&sx), Addr(&sy))
			x, y := charAt(s, t2, c)
			var p Point
			WidgetComputePoint(s.widget, win, &Point{X: float32(x), Y: float32(y)}, &p)
			out = [2]int{int(float64(p.X) + sx), int(float64(p.Y) + sy)}
		})
		return out
	}
	from, to := screen(texts[0], 6), screen(texts[2], 3)
	if xid == 0 {
		t.Skip("not an X11 window")
	}
	w := strconv.Itoa(int(xid))
	xdo := func(args ...string) {
		if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
			t.Fatalf("xdotool %v: %v %s", args, err, out)
		}
	}
	move := func(p [2]int) { xdo("mousemove", "--window", w, strconv.Itoa(p[0]), strconv.Itoa(p[1])) }
	move(from)
	time.Sleep(50 * time.Millisecond)
	xdo("mousedown", "1")
	for i := 1; i <= 10; i++ {
		f := float64(i) / 10
		move([2]int{from[0] + int(f*float64(to[0]-from[0])), from[1] + int(f*float64(to[1]-from[1]))})
		time.Sleep(20 * time.Millisecond)
	}
	xdo("mouseup", "1")
	settle(t, h, func() bool {
		return selBounds(texts[0]) == [2]int{6, 10} && selBounds(texts[1]) == [2]int{0, 23} && selBounds(texts[2]) == [2]int{0, 3}
	})
	onMain(func() {
		if !WidgetHasFocus(s.widget) {
			fatalf(t, "the Selectable hasn't the focus")
		}
	})
	// A double click on "delta" (after the double-click time).
	time.Sleep(500 * time.Millisecond)
	move(screen(texts[1], 8))
	time.Sleep(50 * time.Millisecond)
	xdo("click", "--repeat", "2", "--delay", "80", "1")
	settle(t, h, func() bool {
		return selBounds(texts[0]) == [2]int{} && selBounds(texts[1]) == [2]int{6, 11} && selBounds(texts[2]) == [2]int{}
	})
	// The keyboard: Ctrl+A, Ctrl+C, in the focused Selectable.
	xdo("key", "--window", w, "ctrl+a")
	settle(t, h, func() bool { return selBounds(texts[2]) == [2]int{0, 12} })
}
