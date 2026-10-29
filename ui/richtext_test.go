package ui_test

import (
	"testing"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
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
