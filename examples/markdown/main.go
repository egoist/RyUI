// Markdown: a reply streaming in, as a language model writes it, parsed
// and shown incrementally: each chunk parses and updates only the block
// being written. The whole reply is one selection.
package main

import (
	_ "embed"
	"time"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/markdown"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

//go:embed reply.md
var reply string

func App() ui.Node {
	return ui.Stack(func() ui.Node {
		text := signal.New("")
		streaming := signal.New(false)
		stream := func() {
			if streaming.Peek() {
				return
			}
			text.Set("")
			streaming.Set(true)
			go func() {
				// Chunks of a few characters, as tokens come.
				for i := 0; i < len(reply); i += 7 {
					time.Sleep(12 * time.Millisecond)
					end := min(i+7, len(reply))
					ui.Post(func() { text.Set(reply[:end]) })
				}
				ui.Post(func() { streaming.Set(false) })
			}()
		}
		return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
				ui.Button(ui.ButtonProps{Variant: ui.ButtonFilled, Disabled: streaming, OnPress: stream,
					Title: signal.Func(func() string {
						if streaming.Get() {
							return "Writing…"
						}
						return "Stream the reply"
					})}),
				markdown.View(markdown.Props{Text: text, Streaming: streaming}),
			),
		)
	}, ui.ScreenOptions{Title: ui.Val("Markdown")})
}

func main() { app.Run(App) }
