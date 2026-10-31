package ui_test

import (
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func TestRichText(t *testing.T) {
	text := signal.NewFunc(ui.Rich{
		ui.Plain("Hello "), ui.Strong("world"), ui.Plain(", see "), ui.Link("the docs", "https://x.dev"),
		ui.Plain(".\nRun "), ui.Code("go test"), ui.Plain("."),
	}, nil)
	var links []string
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{}, ui.RichText(ui.RichTextProps{Text: text, OnLink: func(u string) { links = append(links, u) },
			Style: ui.Style{Font: ui.Body, Color: ui.SecondaryLabel}}))
	})
	v := h.OfKind(ops.KindRichText)[0]
	paras := v.Rich()
	if len(paras) != 2 || paras[0].Text() != "Hello world, see the docs." || paras[1].Text() != "Run go test." {
		t.Fatalf("%+v", paras)
	}
	if paras[0][1].Style.Weight != ui.Bold || paras[0][3].Style.Link != "https://x.dev" || !paras[1][1].Style.Mono ||
		paras[1][1].Style.Background != ui.SecondaryBackground || paras[0][0].Style != paras[0][2].Style {
		t.Fatalf("%+v", paras)
	}
	// The view's own style carries the defaults, as a Text's does.
	if v.Props[ops.PropTextStyle].Int != int64(ops.TextStyleBody) {
		t.Fatal("text style")
	}
	// It's measured as its paragraphs, a line each here.
	if f := v.Frame; f[3] != 40 {
		t.Fatal(f)
	}
	// Appending sends the new content once; the same content, nothing.
	h.Reset()
	text.Set(append(text.Peek(), ui.Plain(" More.")))
	r.Flush()
	text.Set(append(ui.Rich(nil), text.Peek()...))
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 {
		t.Fatalf("\n%s", h.Log())
	}
	h.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventLink, Str: "https://x.dev"})
	if len(links) != 1 {
		t.Fatal(links)
	}
	_ = fake.KindCell
}

// An inline component is built once for its key, a child view of the
// RichText that the host is told the size of before measuring the text;
// a span gone disposes it.
func TestRichTextInline(t *testing.T) {
	builds, cleanups := 0, 0
	badge := func() ui.Node {
		builds++
		signal.OnCleanup(func() { cleanups++ })
		return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(30), Height: ui.Pt(44)}})
	}
	text := signal.NewFunc(ui.Rich{ui.Plain("a "), ui.Embed("b", badge)}, nil)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{}, ui.RichText(ui.RichTextProps{Text: text}))
	})
	v := h.OfKind(ops.KindRichText)[0]
	if builds != 1 || len(v.Children) != 1 {
		t.Fatalf("builds %d, children %d", builds, len(v.Children))
	}
	holder := v.Children[0]
	if got := v.Inlines(); len(got) != 1 || got[0] != (fake.RichInline{ID: holder.ID, W: 30, H: 44}) {
		t.Fatalf("inlines %+v", got)
	}
	if runs := v.Rich()[0]; len(runs) != 2 || !runs[1].Style.Inline || runs[1].Text != "￼" {
		t.Fatalf("runs %+v", runs)
	}
	// The text is measured with the component's height; the component
	// is laid out at its own size, at the origin (the host places it).
	if f := v.Frame; f[3] != 44 {
		t.Fatalf("rich text frame %v", f)
	}
	if f := holder.Frame; f != [4]float32{0, 0, 30, 44} {
		t.Fatalf("holder frame %v", f)
	}

	// Streaming more text keeps the component, and sends no sizes again:
	// one crossing for the props, one for the frames.
	h.Reset()
	text.Set(ui.Rich{ui.Plain("a "), ui.Embed("b", badge), ui.Plain(" more")})
	r.Flush()
	if builds != 1 || h.Count(ops.OpCreate) != 0 || h.Applies > 2 {
		t.Fatalf("builds %d, creates %d, applies %d", builds, h.Count(ops.OpCreate), h.Applies)
	}

	// The span gone: the component is disposed, its view destroyed.
	text.Set(ui.Rich{ui.Plain("a")})
	r.Flush()
	if cleanups != 1 || len(v.Children) != 0 || h.Views[holder.ID] != nil {
		t.Fatalf("cleanups %d, children %d", cleanups, len(v.Children))
	}
	if got := v.Inlines(); len(got) != 0 {
		t.Fatalf("inlines %+v", got)
	}
}

// Disposing the root disposes the inline components.
func TestRichTextInlineDisposal(t *testing.T) {
	cleanups := 0
	h := fake.New()
	r := ui.Mount(h, func() ui.Node {
		return ui.RichText(ui.RichTextProps{Text: ui.Val(ui.Rich{ui.Embed("x", func() ui.Node {
			signal.OnCleanup(func() { cleanups++ })
			return ui.StaticText("x")
		})})})
	})
	r.Dispose()
	if cleanups != 1 {
		t.Fatalf("cleanups %d", cleanups)
	}
}
