package markdown_test

import (
	"strings"
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/markdown"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func mount(t *testing.T, root func() ui.Node) (*fake.Host, *ui.Root) {
	t.Helper()
	h := fake.New()
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	return h, r
}

func texts(h *fake.Host) []string {
	var out []string
	for _, v := range h.OfKind(ops.KindRichText) {
		var b strings.Builder
		for i, p := range v.Rich() {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text())
		}
		out = append(out, b.String())
	}
	return out
}

func TestBlocks(t *testing.T) {
	src := "# Title\n\nSome *emphasis*, **strong**, `code` and [a link](https://x.dev).\n\n- one\n- two\n\n1. first\n\n> quoted\n\n```go\nfmt.Println(1)\n```\n\n| a | b |\n|---|--:|\n| 1 | 2 |\n\n---\n"
	h, _ := mount(t, func() ui.Node { return markdown.View(markdown.Props{Text: ui.Val(src)}) })
	got := strings.Join(texts(h), "|")
	want := "Title|Some emphasis, strong, code and a link.|one|two|first|quoted|fmt.Println(1)|a|b|1|2"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	para := h.OfKind(ops.KindRichText)[1].Rich()[0]
	for _, r := range para {
		switch r.Text {
		case "emphasis":
			if !r.Style.Italic {
				t.Error("emphasis")
			}
		case "strong":
			if r.Style.Weight != ui.Bold {
				t.Error("strong")
			}
		case "code":
			if !r.Style.Mono {
				t.Error("code")
			}
		case "a link":
			if r.Style.Link != "https://x.dev" {
				t.Error("link")
			}
		}
	}
	if len(h.OfKind(ops.KindSelectable)) != 1 {
		t.Fatal("not selectable")
	}
}

func TestStreamingUpdatesOnlyTheLastBlock(t *testing.T) {
	src := signal.New("# Title\n\nFirst paragraph.\n\nSecond")
	streaming := signal.New(true)
	h, r := mount(t, func() ui.Node {
		return markdown.View(markdown.Props{Text: src, Streaming: streaming})
	})
	if got := strings.Join(texts(h), "|"); got != "Title|First paragraph.|Second" {
		t.Fatal(got)
	}
	last := h.OfKind(ops.KindRichText)[2]
	h.Reset()
	for _, tok := range []string{" para", "graph with **bold", " text** and"} {
		src.Set(src.Peek() + tok)
		r.Flush()
	}
	// Only the paragraph being written changed: no other view, no new one.
	for _, op := range h.Ops {
		if op.Code == ops.OpSetProp && op.ID != last.ID || op.Code == ops.OpCreate {
			t.Fatalf("%v\n%s", op, h.Log())
		}
	}
	// While streaming, the unclosed emphasis showed as bold; done, the
	// text is exactly CommonMark's.
	if got := texts(h)[2]; got != "Second paragraph with bold text and" {
		t.Fatal(got)
	}
	src.Set(src.Peek() + " done.\n\nA new paragraph.")
	streaming.Set(false)
	r.Flush()
	if got := strings.Join(texts(h), "|"); got != "Title|First paragraph.|Second paragraph with bold text and done.|A new paragraph." {
		t.Fatal(got)
	}
}

func TestBlockDisposal(t *testing.T) {
	src := signal.New("one\n\ntwo\n\nthree")
	h, r := mount(t, func() ui.Node { return markdown.View(markdown.Props{Text: src}) })
	src.Set("one")
	r.Flush()
	if got := strings.Join(texts(h), "|"); got != "one" {
		t.Fatal(got)
	}
}

func TestCodeTabs(t *testing.T) {
	src := "```go\nfunc f() {\n\tif x {\n\t\treturn\n\t}\n}\nab\tc\n```\n"
	h, _ := mount(t, func() ui.Node { return markdown.View(markdown.Props{Text: ui.Val(src)}) })
	want := "func f() {\n    if x {\n        return\n    }\n}\nab  c"
	if got := texts(h)[0]; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// An image in a paragraph's text is an inline component of its
// RichText, built once while the text after it streams in.
func TestInlineImage(t *testing.T) {
	src := signal.New("Built with ![ryui](ryui.png) and")
	h, r := mount(t, func() ui.Node { return markdown.View(markdown.Props{Text: src}) })
	rt := h.OfKind(ops.KindRichText)[0]
	if len(rt.Children) != 1 || len(h.OfKind(ops.KindImage)) != 1 || h.OfKind(ops.KindImage)[0].String(ops.PropImageSource) != "ryui.png" {
		t.Fatalf("rich text:\n%s", rt.Tree())
	}
	h.Reset()
	src.Set("Built with ![ryui](ryui.png) and Go.")
	r.Flush()
	if h.Count(ops.OpCreate) != 0 {
		t.Fatalf("%d creates while streaming", h.Count(ops.OpCreate))
	}
}

// A linked image follows its link.
func TestLinkedImage(t *testing.T) {
	var got []string
	h, _ := mount(t, func() ui.Node {
		return markdown.View(markdown.Props{Text: ui.Val("See [![build](b.png)](https://ci.dev) here"), OnLink: func(u string) { got = append(got, u) }})
	})
	rt := h.OfKind(ops.KindRichText)[0]
	if len(rt.Children) != 1 {
		t.Fatalf("rich text:\n%s", rt.Tree())
	}
	h.Tap(rt.Children[0].Children[0])
	if len(got) != 1 || got[0] != "https://ci.dev" {
		t.Fatalf("links %v", got)
	}
}

// A theme sets what it changes; what it leaves zero is the default's.
func TestTheme(t *testing.T) {
	src := "# Title\n\nSee [docs](https://x.dev) and `code`.\n\n```\nx\n```\n"
	th := &markdown.Theme{
		Link:                ui.Purple,
		CodeBlockBackground: ui.Yellow,
		Headings:            [6]markdown.HeadingStyle{{Font: ui.Font{Weight: ui.Semibold}}},
	}
	h, _ := mount(t, func() ui.Node { return markdown.View(markdown.Props{Text: ui.Val(src), Theme: th}) })
	rts := h.OfKind(ops.KindRichText)
	title := rts[0]
	// The weight it set, the style it didn't.
	if title.Props[ops.PropFontWeight].Int != int64(ui.Semibold) || title.Props[ops.PropTextStyle].Int != int64(ops.TextStyleTitle) {
		t.Fatalf("heading props %+v", title.Props)
	}
	var link, code fake.RichRun
	for _, r := range rts[1].Rich()[0] {
		switch r.Text {
		case "docs":
			link = r
		case "code":
			code = r
		}
	}
	if link.Style.Color != ui.Purple || link.Style.Link != "https://x.dev" {
		t.Fatalf("link %+v", link)
	}
	if code.Style.Background != ui.SecondaryBackground {
		t.Fatalf("code span %+v", code)
	}
	block := rts[2]
	for p := block.Parent; p != nil; p = p.Parent {
		if p.Props[ops.PropBackground].Int != 0 {
			if ops.Color(p.Props[ops.PropBackground].Int) != ui.Yellow {
				t.Fatalf("code block background %x", p.Props[ops.PropBackground].Int)
			}
			return
		}
	}
	t.Fatal("no code block background")
}
