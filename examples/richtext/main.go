// Rich text: styled spans, highlights drawn per line, links, and a reply
// that streams in word by word, as a chat assistant's does.
package main

import (
	"strings"
	"time"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

func App() ui.Node {
	return ui.Stack(func() ui.Node {
		onLink := func(url string) {
			ui.Alert(ui.AlertProps{Title: "Open link?", Message: url, Actions: []ui.Action{
				{Title: "Cancel", Role: ui.ActionCancel}, {Title: "Open"},
			}})
		}
		return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 20},
				ui.RichText(ui.RichTextProps{OnLink: onLink, Text: ui.Val(ui.Rich{
					ui.Strong("Spans"), ui.Plain(" can be "), ui.Em("italic"), ui.Plain(", "),
					ui.Strong("bold"), ui.Plain(", "), {Text: "underlined", Underline: true}, ui.Plain(", "),
					{Text: "struck", Strike: true}, ui.Plain(", "), ui.Plain("colored").Colored(ui.Purple),
					ui.Plain(" or "), ui.Plain("highlighted across a line break, which a label can't draw").Highlighted(ui.RGBA(255, 214, 10, 110)),
					ui.Plain(". Inline "), ui.Code("code"), ui.Plain(" and "), ui.Link("links", "https://go.dev"),
					ui.Plain(" too.\n\nA second paragraph, "), {Text: "larger", Font: ui.Font{Size: 22}},
					ui.Plain(" and "), {Text: "smaller", Font: ui.Footnote}, ui.Plain("."),
				})}),
				ui.RichText(ui.RichTextProps{Style: ui.Style{Lines: 2, Color: ui.SecondaryLabel}, Text: ui.Val(ui.Rich{
					ui.Strong("Two lines at most: "),
					ui.Plain("this paragraph goes on long enough to wrap past its second line, where it's cut off with an ellipsis instead."),
				})}),
				stream(onLink),
			),
		)
	}, ui.ScreenOptions{Title: ui.Val("Rich text")})
}

const reply = `Here's how it works. Each paragraph is typeset by the platform's engine and **cached** by its content and width, so while this reply streams in, only the paragraph being written is typeset again: the ones above it are cache hits.

Spans carry their own look: ` + "`code`" + `, **strong**, _emphasis_, and links like [the Go site](https://go.dev).

The view draws exactly what it measured, so the layout never disagrees with what's on screen.`

// tokens turns the reply into spans, word by word, with a tiny subset of
// Markdown (**strong**, _em_, `code`, [link](url)).
func tokens(s string) []ui.Span {
	var out []ui.Span
	for len(s) > 0 {
		switch {
		case strings.HasPrefix(s, "**"):
			end := strings.Index(s[2:], "**")
			out = append(out, ui.Strong(s[2:2+end]))
			s = s[4+end:]
		case strings.HasPrefix(s, "_"):
			end := strings.Index(s[1:], "_")
			out = append(out, ui.Em(s[1:1+end]))
			s = s[2+end:]
		case strings.HasPrefix(s, "`"):
			end := strings.Index(s[1:], "`")
			out = append(out, ui.Code(s[1:1+end]))
			s = s[2+end:]
		case strings.HasPrefix(s, "["):
			mid := strings.Index(s, "](")
			end := strings.Index(s, ")")
			out = append(out, ui.Link(s[1:mid], s[mid+2:end]))
			s = s[end+1:]
		default:
			// A word, with the space or newline after it.
			i := strings.IndexAny(s[1:], " \n*_`[")
			if i < 0 {
				i = len(s) - 1
			}
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\n') {
				j++
			}
			out = append(out, ui.Plain(s[:j]))
			s = s[j:]
		}
	}
	return out
}

func stream(onLink func(string)) ui.Node {
	text := signal.NewFunc(ui.Rich(nil), nil)
	streaming := signal.New(false)
	start := func() {
		if streaming.Peek() {
			return
		}
		streaming.Set(true)
		text.Set(nil)
		go func() {
			for _, t := range tokens(reply) {
				time.Sleep(25 * time.Millisecond)
				ui.Post(func() { text.Set(append(text.Peek(), t)) })
			}
			ui.Post(func() { streaming.Set(false) })
		}()
	}
	return ui.Column(ui.Style{Gap: 12},
		ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
			if streaming.Get() {
				return "Streaming…"
			}
			return "Stream a reply"
		}), Variant: ui.ButtonFilled, Disabled: streaming, OnPress: start}),
		ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(14), Background: ui.SecondaryBackground, CornerRadius: 18}},
			ui.RichText(ui.RichTextProps{Text: text, OnLink: onLink})),
	)
}

func main() { app.Run(App) }
