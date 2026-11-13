//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// shot writes the window as a PNG when ASUKA_TEST_SHOTS names a directory, to
// look at what a test built.
func shot(h *Host, name string) {
	if dir := os.Getenv("ASUKA_TEST_SHOTS"); dir != "" {
		Send(h.window, "orderFront:", 0)
		h.snapshot(filepath.Join(dir, name+".png"))
	}
}

// setLocale chooses the language of the running app, as ui.SetLocale in an
// event handler does.
func setLocale(h *Host, tag string) {
	h.enter(func() { ui.Post(func() { ui.SetLocale(tag) }) })
	h.runPosted()
}

// mountLocale mounts root in an app written in English and Arabic, showing
// the language of tag, and puts the language back when the test ends. A
// process with ASUKA_LOCALE set shows that one instead.
func mountLocale(t *testing.T, tag string, root func() ui.Node) *Host {
	t.Helper()
	onMain(func() { ui.SetLanguages("en", "ar") })
	h, _ := mount(t, root)
	saved := ownLanguages()
	onMain(func() { setLocale(h, tag) })
	t.Cleanup(func() {
		onMain(func() {
			setLocale(h, "")
			ui.SetLanguages()
			restoreLanguages(saved)
		})
	})
	return h
}

// ownLanguages reads AppleLanguages of the process's own defaults, alone.
func ownLanguages() []string {
	var out []string
	onMain(func() { out = languagesOf() })
	return out
}

func languagesOf() []string {
	var out []string
	name := Send(Send(Class("NSProcessInfo"), "processInfo"), "processName")
	if dom := Send(Send(Class("NSUserDefaults"), "standardUserDefaults"), "persistentDomainForName:", uintptr(name)); dom != 0 {
		for _, s := range ArrayItems(Send(dom, "objectForKey:", uintptr(String("AppleLanguages")))) {
			out = append(out, GoString(s))
		}
	}
	return out
}

func restoreLanguages(tags []string) {
	d := Send(Class("NSUserDefaults"), "standardUserDefaults")
	if len(tags) == 0 {
		Send(d, "removeObjectForKey:", uintptr(String("AppleLanguages")))
	} else {
		var items []ID
		for _, t := range tags {
			items = append(items, String(t))
		}
		Send(d, "setObject:forKey:", uintptr(Array(items...)), uintptr(String("AppleLanguages")))
	}
	Send(d, "synchronize")
}

func viewRTL(v ID) bool { return SendInt(v, "userInterfaceLayoutDirection") == 1 }

func byText(h *Host, s string) *node {
	for _, n := range find(h, ops.KindText) {
		if n.text == s {
			return n
		}
	}
	return nil
}

func byTitle(h *Host, s string) *node {
	for _, n := range find(h, ops.KindButton) {
		if n.title == s {
			return n
		}
	}
	return nil
}

// A Row's children are placed from the right, in physical frames the host
// doesn't touch, and a text's start is the right.
func TestLocaleRowAndText(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Row(ui.Style{Gap: 8},
				ui.Button(ui.ButtonProps{Title: ui.Val("One")}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Two")}),
			),
			ui.StaticText("start"),
			ui.StaticText("end", ui.Style{TextAlign: ui.TextEnd}),
			ui.StaticText("center", ui.Style{TextAlign: ui.TextCenter}),
		)
	})
	onMain(func() {
		one, two := MsgRect(byTitle(h, "One").view, Sel("frame")), MsgRect(byTitle(h, "Two").view, Sel("frame"))
		if two.Origin.X+two.Size.Width > one.Origin.X {
			fatalf(t, "the second button at %v isn't left of the first at %v", two, one)
		}
		width := MsgRect(h.content, Sel("bounds")).Size.Width
		if right := one.Origin.X + one.Size.Width; right != width-10 {
			fatalf(t, "the first button ends at %v of a %v wide window", right, width)
		}
		start := byText(h, "start")
		if !viewRTL(start.view) || !viewRTL(byTitle(h, "One").view) {
			fatal(t, "views aren't right to left")
		}
		if a := SendInt(start.view, "alignment"); uintptr(a) != alignRight {
			fatalf(t, "a text starts at alignment %d", a)
		}
		// Its base writing direction is its own: English reads as English.
		if b := SendInt(start.view, "baseWritingDirection"); b != -1 {
			fatalf(t, "a text's base writing direction is %d", b)
		}
		if a := SendInt(byText(h, "end").view, "alignment"); uintptr(a) != alignLeft {
			fatalf(t, "a text's end is at alignment %d", a)
		}
		if a := SendInt(byText(h, "center").view, "alignment"); uintptr(a) != alignCenter {
			fatalf(t, "a centered text is at alignment %d", a)
		}
	})
}

// WithDirection's views read its way in an app that reads the other, and
// what's beside them is the app's.
func TestLayoutDirectionProp(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("app"),
			ui.WithDirection(ui.LeftToRight, func() ui.Node {
				return ui.Row(ui.Style{Gap: 8},
					ui.StaticText("first"),
					ui.StaticText("second"),
					ui.Symbol("chevron.forward"),
					ui.List(ui.ListProps{Columns: 2, Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(40)}}, signal.Static([]int{1, 2}), func(i int) int { return i },
						func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
							return ui.Text(signal.Func(func() string { return fmt.Sprint("cell ", item.Get()) }))
						}),
				)
			}),
		)
	})
	onMain(func() {
		app, first, second := byText(h, "app"), byText(h, "first"), byText(h, "second")
		if viewRTL(first.view) || !viewRTL(app.view) {
			fatalf(t, "the override's text is RTL %v, the app's %v", viewRTL(first.view), viewRTL(app.view))
		}
		if MsgRect(first.view, Sel("frame")).Origin.X >= MsgRect(second.view, Sel("frame")).Origin.X {
			fatal(t, "the override's row runs from the right")
		}
		if a := SendInt(first.view, "alignment"); uintptr(a) != alignLeft {
			fatalf(t, "the override's text is at alignment %d", a)
		}
		if b := SendInt(first.view, "baseWritingDirection"); b != 0 {
			fatalf(t, "the override's text has writing direction %d", b)
		}
		sym := find(h, ops.KindSymbol)[0]
		if viewRTL(sym.view) {
			fatal(t, "the override's symbol is RTL")
		}
		// A list made in it has its columns from the left, and its cells the
		// direction, as what's in them lays out by.
		l := find(h, ops.KindList)[0].list
		rv := l.shown[0]
		if len(rv.cells) != 2 {
			fatalf(t, "%d cells", len(rv.cells))
		}
		if MsgRect(rv.cells[0].n.view, Sel("frame")).Origin.X != 0 || viewRTL(rv.cells[0].n.view) {
			fatal(t, "the override's list isn't left to right")
		}
	})
}

// A WithDirection says the writing direction of its text and its rich text,
// whichever way the app reads; without one, it's the text's.
func TestWritingDirection(t *testing.T) {
	rich := ui.Val(ui.Rich{ui.Plain("rich")})
	h := mountLocale(t, "en", func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("app"),
			ui.RichText(ui.RichTextProps{Text: rich}),
			ui.WithDirection(ui.RightToLeft, func() ui.Node {
				return ui.Column(ui.Style{}, ui.StaticText("right"), ui.RichText(ui.RichTextProps{Text: rich}))
			}),
			ui.WithDirection(ui.LeftToRight, func() ui.Node {
				return ui.Column(ui.Style{}, ui.StaticText("left"), ui.RichText(ui.RichTextProps{Text: rich}))
			}),
		)
	})
	onMain(func() {
		check := func(locale string) {
			t.Helper()
			for text, want := range map[string]int{"app": -1, "right": 1, "left": 0} {
				if d := SendInt(byText(h, text).view, "baseWritingDirection"); d != want {
					fatalf(t, "%s: %q has writing direction %d", locale, text, d)
				}
			}
			for _, n := range find(h, ops.KindRichText) {
				want := -1
				switch n.dir {
				case ops.LayoutDirectionRightToLeft:
					want = 1
				case ops.LayoutDirectionLeftToRight:
					want = 0
				}
				n.rich.placeInlines(h, n)
				para := Send(Send(n.view, "textStorage"), "attribute:atIndex:effectiveRange:", uintptr(Const(AppKit(), "NSParagraphStyleAttributeName")), 0, 0)
				if d := SendInt(para, "baseWritingDirection"); d != want {
					fatalf(t, "%s: rich text with direction %d has writing direction %d", locale, n.dir, d)
				}
			}
		}
		check("en")
		setLocale(h, "ar")
		check("ar")
	})
}

// A forward symbol points where the view reads, and chevron.right where it
// says, whichever way.
func TestSymbolsMirror(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Column(ui.Style{Gap: 8, Padding: ui.All(8)},
			ui.Symbol("chevron.forward"),
			ui.Symbol("chevron.backward"),
			ui.Symbol("chevron.right"),
			ui.WithDirection(ui.LeftToRight, func() ui.Node { return ui.Symbol("chevron.forward") }),
		)
	})
	onMain(func() {
		byName := map[string][]*node{}
		for _, n := range find(h, ops.KindSymbol) {
			byName[n.symbol] = append(byName[n.symbol], n)
		}
		want := func(n *node, side string) {
			t.Helper()
			if got := pointsTo(n.view); got != side {
				fatalf(t, "%s (rtl %v) points %s", n.symbol, viewRTL(n.view), got)
			}
		}
		want(byName["chevron.backward"][0], "right")
		want(byName["chevron.right"][0], "right")
		for _, n := range byName["chevron.forward"] {
			if n.dir == ops.LayoutDirectionLeftToRight {
				want(n, "right")
			} else {
				want(n, "left")
			}
		}
	})
}

// pointsTo says which way a chevron drawn in v points: its tip is the side
// whose ink is at its middle row. The view draws as it does on screen.
func pointsTo(v ID) string {
	b := MsgRect(v, Sel("bounds"))
	rep := MsgRectToID(v, Sel("bitmapImageRepForCachingDisplayInRect:"), b)
	MsgRectIDVoid(v, Sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
	w, ht := SendInt(rep, "pixelsWide"), SendInt(rep, "pixelsHigh")
	first := func(y int) int {
		for x := range w {
			c := Send(rep, "colorAtX:y:", uintptr(x), uintptr(y))
			if MsgFloat(c, Sel("alphaComponent")) > 0.5 {
				return x
			}
		}
		return w
	}
	// The tip's row starts further from the left than the ends' when it points
	// right, and nearer when it points left.
	var top, mid int
	for y := range ht {
		if first(y) < w {
			top = y
			break
		}
	}
	mid = ht / 2
	if first(mid) > first(top+1) {
		return "right"
	}
	return "left"
}

func TestEnvironmentLanguages(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		env := h.Environment()
		if env.Languages == "" {
			fatal(t, "no languages")
		}
		if env.FirstWeekday < 1 || env.FirstWeekday > 7 {
			fatalf(t, "first weekday %d", env.FirstWeekday)
		}
		if env.HourCycle != 12 && env.HourCycle != 24 {
			fatalf(t, "hour cycle %d", env.HourCycle)
		}
		if got := hourCycle("HH 'h'"); got != 24 {
			fatalf(t, "a pattern with a quoted h: %d", got)
		}
		if got := hourCycle("h a"); got != 12 {
			fatalf(t, "h a: %d", got)
		}
	})
}

// A saved language comes first in what's reported, with the system's after
// it, and removing it gives the system's back in this session.
func TestSaveLocale(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	saved := ownLanguages()
	t.Cleanup(func() { onMain(func() { restoreLanguages(saved) }) })
	onMain(func() {
		system := h.languages
		if len(saved) > 0 {
			restoreLanguages(nil)
			h.refreshLocale()
			system = h.languages
		}
		h.saveLocale("de")
		if h.languages != "de,"+system && h.languages != "de" {
			fatalf(t, "saved de: %q, the system's %q", h.languages, system)
		}
		if got := languagesOf(); len(got) != 1 || got[0] != "de" {
			fatalf(t, "the app's defaults have %v", got)
		}
		if env := h.Environment(); env.Languages != h.languages {
			fatalf(t, "reported %q, read %q", env.Languages, h.languages)
		}
		h.saveLocale("")
		if h.languages != system {
			fatalf(t, "removed: %q, the system's %q", h.languages, system)
		}
		if got := languagesOf(); len(got) != 0 {
			fatalf(t, "the app's defaults still have %v", got)
		}
		if env := h.Environment(); env.Languages != system {
			fatalf(t, "reported %q after removing, the system's %q", env.Languages, system)
		}
	})
}

func TestWindowTitleCommand(t *testing.T) {
	title := signal.New("First")
	ui.SetWindowOptions(ui.WindowOptions{Title: title})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if s := GoString(Send(h.window, "title")); s != "First" {
			fatalf(t, "title %q", s)
		}
		h.enter(func() { ui.Post(func() { title.Set("Second") }) })
		h.runPosted()
		if s := GoString(Send(h.window, "title")); s != "Second" {
			fatalf(t, "title after the change %q", s)
		}
		h.enter(func() { ui.Post(func() { title.Set("") }) })
		h.runPosted()
		if s := GoString(Send(h.window, "title")); s != h.appName {
			fatalf(t, "an empty title gives %q, the app's name is %q", s, h.appName)
		}
	})
}

func TestRichTextRTL(t *testing.T) {
	linked := ""
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.RichText(ui.RichTextProps{
				Style:  ui.Style{Width: ui.Pt(300)},
				OnLink: func(url string) { linked = url },
				Text:   ui.Val(ui.Rich{ui.Plain("مرحبا "), ui.Link("link", "https://example.com")}),
			}),
		)
	})
	onMain(func() {
		n := find(h, ops.KindRichText)[0]
		ts := Send(n.view, "textStorage")
		if SendInt(ts, "length") == 0 {
			fatal(t, "no text")
		}
		para := Send(ts, "attribute:atIndex:effectiveRange:", uintptr(Const(AppKit(), "NSParagraphStyleAttributeName")), 0, 0)
		if a := SendInt(para, "alignment"); uintptr(a) != alignRight {
			fatalf(t, "a paragraph's alignment is %d", a)
		}
		if d := SendInt(para, "baseWritingDirection"); d != -1 {
			fatalf(t, "a paragraph's base writing direction is %d", d)
		}
		if !viewRTL(n.view) {
			fatal(t, "the text view isn't right to left")
		}
		// It's drawn at the view's right, and it measured its own width.
		lm, tc := Send(n.view, "layoutManager"), Send(n.view, "textContainer")
		used := MsgIDToRect(lm, Sel("usedRectForTextContainer:"), tc)
		w := MsgRect(n.view, Sel("bounds")).Size.Width
		if w != 300 || used.Origin.X+used.Size.Width < w-1 {
			fatalf(t, "the text is at %v in a %v wide view", used, w)
		}
		// The link, at the left in a right to left line, still takes a click.
		var i int
		str := GoString(Send(ts, "string"))
		for k, r := range []rune(str) {
			if r == 'l' {
				i = k
				break
			}
		}
		g := SendInt(lm, "glyphIndexForCharacterAtIndex:", uintptr(i))
		rect := MsgRangeToRect(lm, Sel("boundingRectForGlyphRange:inTextContainer:"), uint(g), 1, tc)
		if !n.rich.linkAt(n.view, Point{X: rect.Origin.X + rect.Size.Width/2, Y: rect.Origin.Y + rect.Size.Height/2}) {
			fatalf(t, "no link at %v", rect)
		}
		if !h.linkClicked(n.view, String("https://example.com")) || linked != "https://example.com" {
			fatalf(t, "the link gave %q", linked)
		}
		// Selection: the range is the text's, whichever way it's drawn.
		Send(n.view, "setSelectedRange:", 0, 3)
		if sel := Send(Send(n.view, "selectedRanges"), "firstObject"); sel == 0 {
			fatal(t, "nothing selected")
		}
	})
}

// A horizontal Scroll starts at its end, keeps its distance from the end as
// its content changes, and its offsets are physical.
func TestHorizontalScrollRTL(t *testing.T) {
	var seen []float32
	count := signal.New(30)
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Scroll(ui.ScrollProps{
			Horizontal: true,
			Style:      ui.Style{Height: ui.Pt(40), Width: ui.Pt(300)},
			OnScroll:   func(x, y float32) { seen = append(seen, x) },
			ContentStyle: ui.Style{
				Gap: 4,
			},
		},
			ui.For(signal.Func(func() []int {
				out := make([]int, count.Get())
				for i := range out {
					out[i] = i
				}
				return out
			}), func(i int) int { return i }, func(i signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return "item " + strconv.Itoa(i.Get()) }))
			}),
		)
	})
	onMain(func() {
		n := find(h, ops.KindScroll)[0]
		doc, clip := Send(n.view, "documentView"), Send(n.view, "contentView")
		df := MsgRect(doc, Sel("frame"))
		b := MsgRect(clip, Sel("bounds"))
		if df.Origin.X != 0 {
			fatalf(t, "the content is at %v", df.Origin)
		}
		if df.Size.Width <= b.Size.Width {
			fatalf(t, "the content is %v wide, the view %v", df.Size.Width, b.Size.Width)
		}
		if want := df.Size.Width - b.Size.Width; b.Origin.X != want {
			fatalf(t, "it starts at %v, its end is at %v", b.Origin.X, want)
		}
		if first := find(h, ops.KindText); len(first) == 0 {
			fatal(t, "no text")
		}
		item0 := byText(h, "item 0")
		if f := MsgRect(item0.view, Sel("frame")); f.Origin.X+f.Size.Width != df.Size.Width {
			fatalf(t, "the first item is at %v of %v", f, df.Size.Width)
		}

		// 50 from the end: ScrollToOffset is physical.
		x := df.Size.Width - b.Size.Width - 50
		h.scrollToOffset(n, x, 0, false)
		if got := MsgRect(clip, Sel("bounds")).Origin.X; got != x {
			fatalf(t, "scrolled to %v, at %v", x, got)
		}
		// The content grows: what shows stays, it's the same distance from the
		// end.
		h.enter(func() { ui.Post(func() { count.Set(40) }) })
		h.runPosted()
		df2 := MsgRect(doc, Sel("frame"))
		if df2.Size.Width <= df.Size.Width {
			fatalf(t, "the content didn't grow: %v", df2.Size.Width)
		}
		if got, want := MsgRect(clip, Sel("bounds")).Origin.X, df2.Size.Width-b.Size.Width-50; got != want {
			fatalf(t, "after growing at %v, want %v", got, want)
		}
		// And shrinks.
		h.enter(func() { ui.Post(func() { count.Set(20) }) })
		h.runPosted()
		df3 := MsgRect(doc, Sel("frame"))
		if got, want := MsgRect(clip, Sel("bounds")).Origin.X, df3.Size.Width-b.Size.Width-50; got != want {
			fatalf(t, "after shrinking at %v, want %v", got, want)
		}
		// Reported physically: the host's scrolling is what was seen.
		seen = nil
		Send(n.view, "reflectScrolledClipView:", uintptr(clip))
		h.scrolled(clip)
		if len(seen) == 0 || float64(seen[len(seen)-1]) != MsgRect(clip, Sel("bounds")).Origin.X {
			fatalf(t, "OnScroll saw %v, at %v", seen, MsgRect(clip, Sel("bounds")).Origin.X)
		}
		// A new language in the same direction doesn't move it; another
		// direction mirrors the offset.
		gap := endGap(n)
		setLocale(h, "en")
		if got := MsgRect(clip, Sel("bounds")).Origin.X; got != gap {
			fatalf(t, "left to right at %v, it was %v from the end", got, gap)
		}
		setLocale(h, "ar")
		if got := endGap(n); got != gap {
			fatalf(t, "right to left again %v from the end, was %v", got, gap)
		}
	})
}

// Short content hugs the right, at the scroll view's origin. A left to right
// scroll in a right to left app stays at its start.
func TestHorizontalScrollShort(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Scroll(ui.ScrollProps{Horizontal: true, Style: ui.Style{Height: ui.Pt(40), Width: ui.Pt(300)}},
				ui.StaticText("short"),
			),
			ui.WithDirection(ui.LeftToRight, func() ui.Node {
				return ui.Scroll(ui.ScrollProps{Horizontal: true, Style: ui.Style{Height: ui.Pt(40), Width: ui.Pt(100)}},
					ui.StaticText("a line wider than its scroll view, by a good way"),
				)
			}),
		)
	})
	onMain(func() {
		for _, n := range find(h, ops.KindScroll) {
			doc, clip := Send(n.view, "documentView"), Send(n.view, "contentView")
			df, b := MsgRect(doc, Sel("frame")), MsgRect(clip, Sel("bounds"))
			if n.dir == ops.LayoutDirectionLeftToRight {
				if viewRTL(n.view) || b.Origin.X != 0 || df.Origin.X != 0 {
					fatalf(t, "a left to right scroll: rtl %v at %v, content %v", viewRTL(n.view), b.Origin.X, df.Origin.X)
				}
				continue
			}
			if b.Origin.X != 0 || df.Origin.X != 0 || df.Size.Width != b.Size.Width {
				fatalf(t, "short content at %v, %v wide in %v at %v", df.Origin.X, df.Size.Width, b.Size.Width, b.Origin.X)
			}
			short := byText(h, "short")
			if f := MsgRect(short.view, Sel("frame")); f.Origin.X+f.Size.Width != df.Size.Width {
				fatalf(t, "the short text is at %v of %v", f, df.Size.Width)
			}
		}
	})
}

// A grid's columns run from the right.
func TestListGridRTL(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.List(ui.ListProps{Columns: 3, Gap: 10}, signal.Static([]int{1, 2, 3, 4}), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("cell ", item.Get()) }), ui.Style{Height: ui.Pt(30)})
			})
	})
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		rv := l.shown[0]
		cw := (l.width - 20) / 3
		if len(rv.cells) != 3 {
			fatalf(t, "%d cells in a row", len(rv.cells))
		}
		for i, c := range rv.cells {
			want := float64(2-i) * (cw + 10)
			if got := MsgRect(c.n.view, Sel("frame")).Origin.X; got != want {
				fatalf(t, "cell %d at %v, want %v", i, got, want)
			}
		}
		// Row 2's only cell starts at the right, too.
		if got := MsgRect(l.shown[1].cells[0].n.view, Sel("frame")).Origin.X; got != 2*(cw+10) {
			fatalf(t, "the next row's first cell at %v", got)
		}
		// Going left to right, the rows are placed again.
		setLocale(h, "en")
		if got := MsgRect(rv.cells[0].n.view, Sel("frame")).Origin.X; got != 0 {
			fatalf(t, "left to right, the first cell at %v", got)
		}
	})
}

// The language changes as the app runs: what follows it does, and the host's
// own words and order.
func TestLiveLocale(t *testing.T) {
	var nav *ui.Navigator
	h := mountLocale(t, "en", func() ui.Node {
		return ui.Stack(func() ui.Node {
			nav = ui.UseNavigator()
			return ui.Column(ui.Style{Padding: ui.All(16), Gap: 8},
				ui.Text(signal.Func(func() string { return "language " + ui.Locale() })),
				ui.Row(ui.Style{Gap: 8},
					ui.Button(ui.ButtonProps{Title: ui.Val("One")}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Two")}),
				),
				ui.Symbol("chevron.forward"),
				ui.Slider(ui.SliderProps{Value: signal.New(0.25), Style: ui.Style{Width: ui.Pt(200)}}),
			)
		}, ui.ScreenOptions{Title: ui.Val("Home")})
	})
	onMain(func() {
		if h.rtl || h.locale != "en" {
			fatalf(t, "starts at %q rtl %v", h.locale, h.rtl)
		}
		h.enter(func() {
			ui.Post(func() {
				nav.Push(func() ui.Node { return ui.StaticText("Detail") }, ui.ScreenOptions{Title: ui.Val("Detail"),
					Actions: []ui.Action{{Title: ui.Val("A"), Icon: "trash"}, {Title: ui.Val("B"), Icon: "arrow.forward"}}})
			})
		})
		h.runPosted()
		// The back button comes first where AppKit reads the way the app does.
		ids := h.main.chrome.ids
		if len(ids) < 4 || (ids[0] == "asuka.back") == h.appRTL {
			fatalf(t, "the toolbar, left to right, AppKit's right to left %v: %v", h.appRTL, ids)
		}
		shot(h, "live-en")
		toolbarBack := func() ID {
			for _, it := range ArrayItems(Send(h.main.chrome.toolbar, "items")) {
				if GoString(Send(it, "itemIdentifier")) == "asuka.back" {
					return it
				}
			}
			return 0
		}
		if s := GoString(Send(toolbarBack(), "label")); s != l10n.Back("en") {
			fatalf(t, "the back button's label is %q", s)
		}

		setLocale(h, "ar")
		if !h.rtl || h.locale != "ar" {
			fatalf(t, "after SetLocale: %q rtl %v", h.locale, h.rtl)
		}
		if s := text(byText(h, "language ar")); s != "language ar" {
			fatalf(t, "the text says %q", s)
		}
		label := byText(h, "language ar")
		if !viewRTL(label.view) || uintptr(SendInt(label.view, "alignment")) != alignRight {
			fatalf(t, "the text: rtl %v alignment %d", viewRTL(label.view), SendInt(label.view, "alignment"))
		}
		one, two := MsgRect(byTitle(h, "One").view, Sel("frame")), MsgRect(byTitle(h, "Two").view, Sel("frame"))
		if two.Origin.X >= one.Origin.X {
			fatalf(t, "the row didn't mirror: %v, %v", one, two)
		}
		// The host's own words.
		if s := GoString(Send(toolbarBack(), "label")); s != l10n.Back("ar") {
			fatalf(t, "the back button's label is %q", s)
		}
		if s := GoString(Send(toolbarBack(), "toolTip")); s != l10n.Back("ar") {
			fatalf(t, "the back button's tool tip is %q", s)
		}
		bar := Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu")
		found := false
		var walk func(m ID)
		walk = func(m ID) {
			for _, it := range ArrayItems(Send(m, "itemArray")) {
				if SEL(Send(it, "action")) == Sel("back:") {
					found = GoString(Send(it, "title")) == l10n.Back("ar")
				}
				if s := Send(it, "submenu"); s != 0 {
					walk(s)
				}
			}
		}
		walk(bar)
		if !found {
			fatal(t, "the View menu's Back isn't in Arabic")
		}
		// The toolbar: the host orders it where AppKit doesn't.
		back := h.main.chrome.ids
		if h.appRTL {
			if back[0] != "asuka.back" {
				fatalf(t, "AppKit's order, reading right to left: %v", back)
			}
		} else if back[len(back)-1] != "asuka.back" {
			fatalf(t, "the toolbar, right to left: %v", back)
		}
		shot(h, "live-ar")

		// Popping now slides the other way and the app's screen comes back.
		h.back(h.main)
		if s := GoString(Send(h.window, "title")); s != "Home" {
			fatalf(t, "after back, the title is %q", s)
		}
		shot(h, "live-ar-home")

		setLocale(h, "en")
		if h.rtl || viewRTL(byText(h, "language en").view) {
			fatal(t, "not left to right again")
		}
		if uintptr(SendInt(byText(h, "language en").view, "alignment")) != alignLeft {
			fatal(t, "a text's start isn't left again")
		}
		if b := SendInt(byText(h, "language en").view, "baseWritingDirection"); b != -1 {
			fatalf(t, "the writing direction is %d", b)
		}
	})
}

// NSTabView orders its tabs by the app's direction: the host keeps them in
// the order that reads, and the child selected is still the core's.
func TestTabsMirror(t *testing.T) {
	sel := signal.New(0)
	h := mountLocale(t, "en", func() ui.Node {
		return ui.Tabs(ui.TabsProps{Selected: sel},
			ui.Tab{Title: ui.Val("First"), Content: func() ui.Node { return ui.StaticText("a") }},
			ui.Tab{Title: ui.Val("Second"), Content: func() ui.Node { return ui.StaticText("b") }},
			ui.Tab{Title: ui.Val("Third"), Content: func() ui.Node { return ui.StaticText("c") }},
		)
	})
	onMain(func() {
		labels := func(n *node) []string {
			var out []string
			for _, it := range ArrayItems(Send(n.view, "tabViewItems")) {
				out = append(out, GoString(Send(it, "label")))
			}
			return out
		}
		n := find(h, ops.KindTabs)[0]
		first := fmt.Sprint(labels(n))
		ordered, reversed := "[First Second Third]", "[Third Second First]"
		if h.appRTL && first != reversed && first != ordered {
			fatalf(t, "tabs %s", first)
		}
		wantEn, wantAr := ordered, reversed
		if h.appRTL {
			wantEn, wantAr = reversed, ordered
		}
		if first != wantEn {
			fatalf(t, "left to right: %s", first)
		}
		setLocale(h, "ar")
		if s := fmt.Sprint(labels(n)); s != wantAr {
			fatalf(t, "right to left: %s", s)
		}
		// The second child's item, wherever it is, selects the second.
		sel2 := n.children[2].nav.tabItem
		Send(n.view, "selectTabViewItem:", uintptr(sel2))
		if sel.Peek() != 2 {
			fatalf(t, "selected %d", sel.Peek())
		}
		if s := Send(n.view, "selectedTabViewItem"); s != sel2 {
			fatal(t, "the third's item isn't selected")
		}
		setLocale(h, "en")
		if s := fmt.Sprint(labels(n)); s != wantEn {
			fatalf(t, "left to right again: %s", s)
		}
		if s := Send(n.view, "selectedTabViewItem"); s != sel2 {
			fatal(t, "the selection moved")
		}
	})
}

// An alert with no button says OK in the language, and shows the app's way.
func TestAlertOK(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		Send(h.window, "orderFront:", 0)
		h.nextCell++
		n := &node{id: ops.HostIDBit | h.nextCell, kind: ops.KindAlert, nav: &nav{}, title: "t", message: "m"}
		h.nodes[n.id] = n
		h.present(n)
		a := n.nav.alert
		btns := ArrayItems(Send(a, "buttons"))
		if len(btns) != 1 || GoString(Send(btns[0], "title")) != l10n.OK("ar") {
			fatalf(t, "%d buttons", len(btns))
		}
		h.destroy(n.id)
	})
}

// Each control reads the app's way, as the view it is.
func TestControlsMirror(t *testing.T) {
	on := signal.New(true)
	seg := signal.New(0)
	wheel := signal.New(0)
	value := signal.New(0.25)
	field := signal.New("")
	notes := signal.New("")
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Column(ui.Style{Gap: 8, Padding: ui.All(12)},
			ui.Toggle(ui.ToggleProps{Value: on}),
			ui.Slider(ui.SliderProps{Value: value}),
			ui.Segmented(ui.SegmentedProps{Segments: ui.Val([]string{"One", "Two", "Three"}), Selected: seg}),
			ui.Stepper(ui.StepperProps{Value: value, Max: 10, Step: 1}),
			ui.Wheel(ui.WheelProps{Items: ui.Val([]string{"First", "Second"}), Selected: wheel}),
			ui.Progress(value),
			ui.Button(ui.ButtonProps{Title: ui.Val("Icon"), Icon: "trash"}),
			ui.TextField(ui.TextFieldProps{Value: field, Placeholder: ui.Val("Placeholder")}),
			ui.TextField(ui.TextFieldProps{Value: notes, Multiline: true, Placeholder: ui.Val("Notes")}),
		)
	})
	onMain(func() {
		for _, k := range []ops.Kind{ops.KindToggle, ops.KindSlider, ops.KindSegmented, ops.KindStepper, ops.KindWheel, ops.KindProgress, ops.KindButton, ops.KindTextField} {
			ns := find(h, k)
			if len(ns) == 0 {
				fatalf(t, "no %s", k)
			}
			for _, n := range ns {
				if !viewRTL(n.view) {
					fatalf(t, "%s isn't right to left", k)
				}
			}
		}
		if !viewRTL(Send(find(h, ops.KindWheel)[0].view, "menu")) {
			fatal(t, "the pop-up's menu isn't right to left")
		}
		for _, n := range find(h, ops.KindTextField) {
			v := n.view
			if n.multiline {
				v = n.tracking
			}
			if a := SendInt(v, "alignment"); uintptr(a) != alignRight {
				fatalf(t, "a field (multiline %v) at alignment %d", n.multiline, a)
			}
			if d := SendInt(v, "baseWritingDirection"); d != -1 {
				fatalf(t, "a field (multiline %v) has writing direction %d", n.multiline, d)
			}
		}
		// A multiline field's text follows, as the app sets it.
		h.enter(func() { ui.Post(func() { notes.Set("line") }) })
		h.runPosted()
		for _, n := range find(h, ops.KindTextField) {
			if n.multiline && uintptr(SendInt(n.tracking, "alignment")) != alignRight {
				fatal(t, "the multiline field's text isn't right aligned")
			}
		}
		shot(h, "controls-ar")
	})
}

// A vertical Scroll's scroller is on the side the view ends, where AppKit
// reads that way: NSScrollView tiles by the application's direction, not its
// own, so one that changed language keeps its scroller until the next launch.
func TestScrollerSide(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.Scroll(ui.ScrollProps{Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(100)}},
			ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1000)}}))
	})
	onMain(func() {
		n := find(h, ops.KindScroll)[0]
		Send(n.view, "tile")
		f := MsgRect(Send(n.view, "verticalScroller"), Sel("frame"))
		if left := f.Origin.X+f.Size.Width/2 < 100; left != h.appRTL {
			fatalf(t, "the scroller is at %v in a 200 wide view, AppKit's right to left %v", f, h.appRTL)
		}
	})
}

// menuTitles reads the main menu's titles: the bar's, and each menu's items'.
func menuTitles() map[string]bool {
	out := map[string]bool{}
	var walk func(m ID)
	walk = func(m ID) {
		for _, it := range ArrayItems(Send(m, "itemArray")) {
			if SendBool(it, "isSeparatorItem") {
				continue
			}
			if s := GoString(Send(it, "title")); s != "" {
				out[s] = true
			}
			if sub := Send(it, "submenu"); sub != 0 {
				walk(sub)
			}
		}
	}
	walk(Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu"))
	return out
}

// The standard menus say the language shown: AppKit's own words, Asuka's for
// the rest, and again as it changes. The app's name goes in those that take it.
func TestMenusInLanguage(t *testing.T) {
	h := mountLocale(t, "ar", func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		name := h.appName
		arabic := []string{
			l10n.About("ar", name), "خدمات", l10n.Hide("ar", name), l10n.HideOthers("ar"), "إظهار الكل", l10n.Quit("ar", name),
			"ملف", "إغلاق النافذة",
			"تعديل", "تراجع", "إعادة", "قص", "نسخ", "لصق", "تحديد الكل",
			"عرض", l10n.Back("ar"), "الدخول إلى وضع ملء الشاشة",
			"نافذة", "تصغير", l10n.Zoom("ar"), "إحضار الكل إلى الأمام",
		}
		got := menuTitles()
		t.Log("the menus in ar:", arabic)
		for _, s := range arabic {
			if !got[s] {
				fatalf(t, "no %q in the menus: %v", s, got)
			}
		}
		english := []string{"File", "Edit", "View", "Window", "Copy", "Paste", "Undo", "Zoom", "Hide Others", "Quit " + name}
		for _, s := range english {
			if got[s] {
				fatalf(t, "%q is still English", s)
			}
		}
		// As the language changes, the menus do.
		setLocale(h, "en")
		got = menuTitles()
		for _, s := range english {
			if !got[s] {
				fatalf(t, "no %q in English: %v", s, got)
			}
		}
		// A language macOS doesn't speak: AppKit's are English, Asuka's aren't.
		h.locale = "sr"
		if s := h.menuWord("Copy"); s != "Copy" {
			fatalf(t, "Copy in Serbian says %q", s)
		}
		// Undo's table marks an access key; it isn't shown.
		h.locale = "de"
		if s := h.menuWord("Undo"); s != "Widerrufen" {
			fatalf(t, "Undo in German says %q", s)
		}
		// A pseudo locale's say so.
		h.locale = "ar-XB"
		if s := h.menuWord("Copy"); s == "Copy" {
			fatal(t, "Copy in ar-XB is plain English")
		}
		h.locale = "en"
	})
}

// A menu of the app's joins the standard one it's titled as, in English or
// in the language shown.
func TestMenuBarJoinsInLanguage(t *testing.T) {
	saved := 0
	h := mountLocale(t, "ar", func() ui.Node {
		return ui.MenuBar(signal.Static([]ui.MenuBarMenu{
			{Title: "ملف", Items: []ui.MenuItem{{Title: "حفظ", Shortcut: ui.Cmd("s"), OnPress: func() { saved++ }}}},
			{Title: "Edit", Items: []ui.MenuItem{{Title: "بحث"}}},
			{Title: "أدوات", Items: []ui.MenuItem{{Title: "فحص"}}},
		}))
	})
	onMain(func() {
		bar := Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu")
		var titles []string
		for _, it := range ArrayItems(Send(bar, "itemArray")) {
			titles = append(titles, GoString(Send(it, "title")))
		}
		want := fmt.Sprint([]string{h.appName, "ملف", "تعديل", "عرض", "أدوات", "نافذة"})
		if got := fmt.Sprint(titles); got != want {
			fatalf(t, "the bar: %s, want %s", got, want)
		}
		has := func(menu, title string) ID {
			m := Send(Send(bar, "itemWithTitle:", uintptr(String(menu))), "submenu")
			return Send(m, "itemWithTitle:", uintptr(String(title)))
		}
		save := has("ملف", "حفظ")
		if save == 0 || has("ملف", "إغلاق النافذة") == 0 {
			fatal(t, "File doesn't have the app's item and its own")
		}
		if has("تعديل", "بحث") == 0 || has("تعديل", "نسخ") == 0 {
			fatal(t, "Edit doesn't have the app's item and its own")
		}
		if has("أدوات", "فحص") == 0 {
			fatal(t, "the app's own menu lost its item")
		}
		Send(Send(save, "target"), "appMenuItem:", uintptr(save))
		if saved != 1 {
			fatal(t, "Save didn't run")
		}
	})
}
