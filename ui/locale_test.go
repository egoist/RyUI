package ui_test

import (
	"sync"
	"testing"
	"testing/fstest"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/i18n"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// catalog is an app's catalog, registered once: its languages are the app's
// for the rest of the tests, whose hosts' language is English.
var catalog = sync.OnceValue(func() *i18n.Catalog {
	c, err := i18n.NewCatalog(fstest.MapFS{
		"en.json":      {Data: []byte(`{"hello": "Hello", "files": "{n, plural, one {# file} other {# files}}", "ryui.ok": "Okay"}`)},
		"de.json":      {Data: []byte(`{"hello": "Hallo", "files": "{n, plural, one {# Datei} other {# Dateien}}"}`)},
		"ar.json":      {Data: []byte(`{"hello": "مرحبا", "files": "{n, plural, zero {لا ملفات} one {ملف} two {ملفان} few {# ملفات} many {# ملفًا} other {# ملف}}"}`)},
		"zh-Hant.json": {Data: []byte(`{"hello": "哈囉"}`)},
	}, "en")
	if err != nil {
		panic(err)
	}
	i18n.Register(c)
	return c
})

func say(key string) string { return catalog().Format(i18n.Locale(), key, nil) }
func files(n int) string    { return catalog().Format(i18n.Locale(), "files", map[string]any{"n": n}) }

// mountLocale mounts root on a host reading languages, and leaves the app
// following them when the test ends.
func mountLocale(t *testing.T, languages string, root func() ui.Node) (*fake.Host, *ui.Root) {
	t.Helper()
	catalog()
	h := fake.New()
	h.Env.Languages = languages
	r := ui.Mount(h, root)
	t.Cleanup(func() {
		ui.SetLocale("")
		r.Dispose()
		h2 := fake.New()
		ui.Mount(h2, func() ui.Node { return ui.Column(ui.Style{}) }).Dispose()
	})
	return h, r
}

func textsOf(h *fake.Host) []string {
	var out []string
	for _, v := range h.OfKind(ops.KindText) {
		out = append(out, v.String(ops.PropTextText))
	}
	return out
}

func TestLocaleMatchesTheAppsLanguages(t *testing.T) {
	for _, c := range []struct{ languages, want string }{
		{"de-AT,en", "de"},
		{"zh-TW", "zh-Hant"}, // not zh-Hans, and not the app's own
		{"fr,de", "de"},      // what the app has of what they read
		{"fr", "en"},         // none: the app's own
		{"ar-EG,en", "ar"},   //
		{"en-GB,de", "en"},   //
		{"zh-Hans,en", "en"}, // another script doesn't suit them
	} {
		h, r := mountLocale(t, c.languages, func() ui.Node { return ui.Column(ui.Style{}) })
		if got := ui.Locale(); got != c.want {
			t.Errorf("reading %s: Locale() = %s, want %s", c.languages, got, c.want)
		}
		cmds := h.Commands(ops.CmdLocale)
		if len(cmds) != 1 || string(cmds[0].Value.Bytes[1:]) != c.want {
			t.Errorf("reading %s: %d Locale commands, %v", c.languages, len(cmds), cmds)
		}
		r.Dispose()
	}
}

// A message said in an accessor follows the language, as it follows its
// arguments; a change costs one prop and the views' measures, no rebuild.
func TestMessagesFollowTheLanguage(t *testing.T) {
	count := signal.New(1)
	builds := 0
	h, r := mountLocale(t, "en", func() ui.Node {
		builds++
		return ui.Column(ui.Style{},
			ui.Text(signal.Func(func() string { return say("hello") })),
			ui.Text(signal.Func(func() string { return files(count.Get()) })),
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string { return say("hello") })}),
		)
	})
	check := func(want ...string) {
		t.Helper()
		got := textsOf(h)
		if len(got) != len(want) {
			t.Fatalf("texts %q, want %q", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("texts %q, want %q", got, want)
			}
		}
	}
	check("Hello", "1 file")
	h.Reset()
	ui.SetLocale("de")
	r.Flush()
	check("Hallo", "1 Datei")
	if got := h.One(ops.KindButton, "Hallo"); got == nil {
		t.Errorf("the button's title didn't change")
	}
	if builds != 1 {
		t.Errorf("the component ran %d times", builds)
	}
	// Three messages' props, and the command, and nothing else that's a prop.
	if n := h.Count(ops.OpSetProp); n != 3 {
		t.Errorf("%d props sent", n)
	}
	count.Set(3)
	r.Flush()
	check("Hallo", "3 Dateien")
}

// SetLocale asks the host to keep the language, and "" to follow the user's.
func TestSetLocale(t *testing.T) {
	h, r := mountLocale(t, "de,en", func() ui.Node { return ui.Column(ui.Style{}) })
	if ui.Locale() != "de" {
		t.Fatalf("Locale = %s", ui.Locale())
	}
	ui.SetLocale("en")
	r.Flush()
	if ui.Locale() != "en" {
		t.Errorf("after SetLocale: %s", ui.Locale())
	}
	saves := h.Commands(ops.CmdSaveLocale)
	if len(saves) != 1 || string(saves[0].Value.Bytes) != "en" {
		t.Errorf("saves: %v", saves)
	}
	ui.SetLocale("")
	r.Flush()
	if ui.Locale() != "de" {
		t.Errorf("following again: %s", ui.Locale())
	}
	saves = h.Commands(ops.CmdSaveLocale)
	if len(saves) != 2 || len(saves[1].Value.Bytes) != 0 {
		t.Errorf("saves: %v", saves)
	}
	defer func() {
		if recover() == nil {
			t.Error("SetLocale of no language: no panic")
		}
	}()
	ui.SetLocale("not a language!")
}

// The environment's languages change while the app runs.
func TestLocaleFollowsTheEnvironment(t *testing.T) {
	h, _ := mountLocale(t, "en", func() ui.Node {
		return ui.Text(signal.Func(func() string { return say("hello") }))
	})
	env := h.Env
	env.Languages = "de"
	h.SetEnvironment(env)
	if got := textsOf(h); len(got) != 1 || got[0] != "Hallo" {
		t.Errorf("texts %q", got)
	}
}

func TestPseudoLocale(t *testing.T) {
	h, r := mountLocale(t, "en", func() ui.Node {
		return ui.Text(signal.Func(func() string { return say("hello") }))
	})
	ui.SetLocale("ar-XB")
	r.Flush()
	if !ui.IsRTL() {
		t.Error("ar-XB isn't right to left")
	}
	if got := textsOf(h); len(got) != 1 || got[0] == "Hello" {
		t.Errorf("pseudo locale says %q", got)
	}
	ui.SetLocale("en-XA")
	r.Flush()
	if ui.IsRTL() {
		t.Error("en-XA is right to left")
	}
}

// Actions, tabs and labels are accessors like any text.
func TestStaticLookingPropsFollow(t *testing.T) {
	title := signal.Func(func() string { return say("hello") })
	h, r := mountLocale(t, "en", func() ui.Node {
		return ui.Tabs(ui.TabsProps{},
			ui.Tab{Title: title, Content: func() ui.Node {
				return ui.Column(ui.Style{},
					ui.Button(ui.ButtonProps{Title: ui.Val("b"), Menu: []ui.Action{{Title: title}}}),
					ui.View(ui.ViewProps{Label: title, OnPress: func() {}}),
					ui.Segmented(ui.SegmentedProps{Segments: signal.Func(func() []string { return []string{say("hello")} })}),
				)
			}},
		)
	})
	ui.SetLocale("de")
	r.Flush()
	if got := h.OfKind(ops.KindScreen)[0].String(ops.PropScreenTitle); got != "Hallo" {
		t.Errorf("tab: %q", got)
	}
	if got := h.One(ops.KindButton, "b").Actions(ops.PropMenu); len(got) != 1 || got[0] != "Hallo" {
		t.Errorf("menu: %q", got)
	}
}

// Own words follow the language, and an app's catalog changes them.
func TestOwnWords(t *testing.T) {
	h, r := mountLocale(t, "de", func() ui.Node { return ui.Column(ui.Style{}) })
	ui.Alert(ui.AlertProps{Title: "t"})
	r.Flush()
	alert := h.OfKind(ops.KindAlert)
	if len(alert) != 1 {
		t.Fatal("no alert")
	}
	if got := alert[0].Actions(ops.PropAlertButtons); len(got) != 1 || got[0] != "OK" {
		t.Errorf("de: %q", got)
	}
	ui.SetLocale("en")
	r.Flush()
	ui.Alert(ui.AlertProps{Title: "t"})
	r.Flush()
	alert = h.OfKind(ops.KindAlert)
	if got := alert[len(alert)-1].Actions(ops.PropAlertButtons); len(got) != 1 || got[0] != "Okay" {
		t.Errorf("the app's ryui.ok: %q", got)
	}
}

// A goroutine may say a message at any time.
func TestLocaleFromGoroutines(t *testing.T) {
	_, r := mountLocale(t, "en", func() ui.Node {
		return ui.Text(signal.Func(func() string { return say("hello") }))
	})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				say("hello")
				ui.Locale()
			}
		}()
	}
	for _, tag := range []string{"de", "ar", "en", "zh-Hant", ""} {
		ui.SetLocale(tag)
		r.Flush()
	}
	wg.Wait()
}

func TestRegion(t *testing.T) {
	h := fake.New()
	h.Env.Languages, h.Env.Locale, h.Env.FirstWeekday, h.Env.HourCycle = "en", "en_DE@rg=gbzzzz", 0, 0
	catalog()
	r := ui.Mount(h, func() ui.Node { return ui.Column(ui.Style{}) })
	defer r.Dispose()
	if got := ui.CurrentRegion(); got.Locale != "en-GB" || got.HourCycle != 24 {
		t.Errorf("region %+v", got)
	}
	if got := ui.FormatLocale(); got != "en-GB-u-fw-mon-hc-h23" {
		t.Errorf("format locale %s", got)
	}
}

func frameOf(v *fake.View) [4]float32 { return v.Frame }

// A Row runs from the right in a right-to-left language, Start and End
// mirror, Left and Right don't, and a flip lays it out again from clean
// caches, with frames that stay physical.
func TestRightToLeftLayout(t *testing.T) {
	h, r := mountLocale(t, "en", func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Row(ui.Style{Height: ui.Pt(20)},
				ui.View(ui.ViewProps{Label: ui.Val("first"), Style: ui.Style{Width: ui.Pt(30)}}),
				ui.View(ui.ViewProps{Label: ui.Val("second"), Style: ui.Style{Width: ui.Pt(40)}}),
			),
			ui.View(ui.ViewProps{Label: ui.Val("logical"), Style: ui.Style{Height: ui.Pt(10), Margin: ui.Edges{Start: 5, End: 7}}}),
			ui.View(ui.ViewProps{Label: ui.Val("physical"), Style: ui.Style{Height: ui.Pt(10), Margin: ui.Edges{Left: 5, Right: 7}}}),
			ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(30)}},
				ui.View(ui.ViewProps{Label: ui.Val("inset"), Style: ui.Style{Absolute: true, Start: ui.Pt(3), Width: ui.Pt(9), Height: ui.Pt(9)}}),
			),
		)
	})
	byLabel := func(s string) [4]float32 {
		t.Helper()
		for _, v := range h.OfKind(ops.KindView) {
			if v.String(ops.PropA11yLabel) == s {
				return frameOf(v)
			}
		}
		t.Fatalf("no view labelled %s", s)
		return [4]float32{}
	}
	const w = 390
	check := func(want map[string]float32) {
		t.Helper()
		for s, x := range want {
			if got := byLabel(s)[0]; got != x {
				t.Errorf("%s at x=%v, want %v", s, got, x)
			}
		}
	}
	check(map[string]float32{"first": 0, "second": 30, "logical": 5, "physical": 5, "inset": 3})
	ui.SetLanguages("en", "ar")
	ui.SetLocale("ar")
	r.Flush()
	if !ui.IsRTL() {
		t.Fatal("not right to left")
	}
	check(map[string]float32{"first": w - 30, "second": w - 70, "logical": 7, "physical": 5, "inset": w - 12})
	cmds := h.Commands(ops.CmdLocale)
	if last := cmds[len(cmds)-1].Value.Bytes; last[0] != 1 || string(last[1:]) != "ar" {
		t.Errorf("Locale command %q", last)
	}
	ui.SetLocale("en")
	r.Flush()
	check(map[string]float32{"first": 0, "second": 30, "logical": 5, "physical": 5, "inset": 3})
	ui.SetLanguages()
}

// The direction reaches screens, tab pages, sheets and list rows, which are
// layout roots of their own.
func TestRightToLeftInLayoutRoots(t *testing.T) {
	h, r := mountLocale(t, "ar", func() ui.Node {
		return ui.Stack(func() ui.Node {
			return ui.Row(ui.Style{Height: ui.Pt(20)}, ui.View(ui.ViewProps{Label: ui.Val("a"), Style: ui.Style{Width: ui.Pt(30)}}))
		}, ui.ScreenOptions{})
	})
	_ = r
	for _, v := range h.OfKind(ops.KindView) {
		if v.String(ops.PropA11yLabel) == "a" && frameOf(v)[0] != 360 {
			t.Errorf("in a screen, at x=%v", frameOf(v)[0])
		}
	}
}

// A horizontal Scroll's content sits at its origin, laid out from the right
// inside, in either direction, and after a flip.
func TestHorizontalScrollRightToLeft(t *testing.T) {
	build := func() ui.Node {
		return ui.Scroll(ui.ScrollProps{Horizontal: true, Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(40)}},
			ui.View(ui.ViewProps{Label: ui.Val("first"), Style: ui.Style{Width: ui.Pt(80)}}),
			ui.View(ui.ViewProps{Label: ui.Val("second"), Style: ui.Style{Width: ui.Pt(80)}}),
		)
	}
	h, r := mountLocale(t, "en", build)
	content := func() *fake.View {
		t.Helper()
		kids := h.OfKind(ops.KindScroll)[0].Children
		if len(kids) != 1 {
			t.Fatalf("%d children", len(kids))
		}
		return kids[0]
	}
	byLabel := func(s string) float32 {
		for _, v := range h.OfKind(ops.KindView) {
			if v.String(ops.PropA11yLabel) == s {
				return frameOf(v)[0]
			}
		}
		return -1
	}
	if c := frameOf(content()); c[0] != 0 || c[2] != 160 || byLabel("first") != 0 || byLabel("second") != 80 {
		t.Errorf("left to right: content %v, first %v, second %v", c, byLabel("first"), byLabel("second"))
	}
	ui.SetLocale("ar")
	r.Flush()
	if c := frameOf(content()); c[0] != 0 || c[2] != 160 || byLabel("first") != 80 || byLabel("second") != 0 {
		t.Errorf("right to left: content %v, first %v, second %v", c, byLabel("first"), byLabel("second"))
	}
}

// WithDirection holds through a flip: its views say so to the host, and a
// horizontal Scroll in one stays at its origin, the bug a flip once caused.
func TestWithDirection(t *testing.T) {
	h, r := mountLocale(t, "en", func() ui.Node {
		return ui.Column(ui.Style{},
			ui.WithDirection(ui.RightToLeft, func() ui.Node {
				return ui.Column(ui.Style{},
					ui.Row(ui.Style{Height: ui.Pt(10)},
						ui.View(ui.ViewProps{Label: ui.Val("rtl"), Style: ui.Style{Width: ui.Pt(30)}})),
					ui.Scroll(ui.ScrollProps{Horizontal: true, Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(10)}},
						ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(300), Height: ui.Pt(10), Background: ui.Red}})),
					ui.StaticText("t"),
				)
			}),
			ui.Row(ui.Style{Height: ui.Pt(10)},
				ui.View(ui.ViewProps{Label: ui.Val("app"), Style: ui.Style{Width: ui.Pt(30)}})),
		)
	})
	x := func(s string) float32 {
		for _, v := range h.OfKind(ops.KindView) {
			if v.String(ops.PropA11yLabel) == s {
				return frameOf(v)[0]
			}
		}
		return -1
	}
	if x("rtl") != 360 || x("app") != 0 {
		t.Errorf("rtl at %v, app at %v", x("rtl"), x("app"))
	}
	if d := h.OfKind(ops.KindText)[0].Int(ops.PropLayoutDirection); d != int64(ops.LayoutDirectionRightToLeft) {
		t.Errorf("a text in it has direction %d", d)
	}
	scrollContent := h.OfKind(ops.KindScroll)[0].Children[0]
	if frameOf(scrollContent)[0] != 0 {
		t.Errorf("content at %v", frameOf(scrollContent)[0])
	}
	ui.SetLanguages("en", "ar")
	defer ui.SetLanguages()
	ui.SetLocale("ar")
	r.Flush()
	if x("rtl") != 360 || x("app") != 360 {
		t.Errorf("after a flip: rtl at %v, app at %v", x("rtl"), x("app"))
	}
	if frameOf(scrollContent)[0] != 0 {
		t.Errorf("after a flip, content at %v", frameOf(scrollContent)[0])
	}
}

// A flip disposes nothing: the owners of what's on screen live on, and
// effects run only for what follows the language.
func TestFlipKeepsOwners(t *testing.T) {
	made, cleaned := 0, 0
	state := signal.New(5)
	h, r := mountLocale(t, "en", func() ui.Node {
		return ui.Show(ui.Val(true), func() ui.Node {
			made++
			signal.OnCleanup(func() { cleaned++ })
			return ui.Text(signal.Func(func() string { return files(state.Get()) }))
		})
	})
	ui.SetLocale("de")
	r.Flush()
	if made != 1 || cleaned != 0 {
		t.Errorf("made %d, cleaned %d", made, cleaned)
	}
	if got := textsOf(h); len(got) != 1 || got[0] != "5 Dateien" {
		t.Errorf("texts %q", got)
	}
}

// Numbers and dates in messages go the user's way, the language's words.
func TestMessagesInRegionalFormats(t *testing.T) {
	catalog()
	h := fake.New()
	h.Env.Languages, h.Env.Locale = "en", "en_DE"
	r := ui.Mount(h, func() ui.Node { return ui.Column(ui.Style{}) })
	defer r.Dispose()
	got := catalog().FormatIn(i18n.Locale(), i18n.FormatLocale(), "files", map[string]any{"n": 1234})
	if got != "1.234 files" {
		t.Errorf("%q", got)
	}
}
