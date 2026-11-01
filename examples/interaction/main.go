// Interaction shows the states a view reports for styles to follow, as
// its style's variants and through an Interaction: pressed (CSS's
// :active), hovered (:hover, and a group's), dragged, a drag over a drop
// target; and drag and drop, within the app (cards between
// columns, carrying Go values) and with other apps (files, photos,
// links, text).
//
//	go run ./cmd/ryui dev -ios examples/interaction
//	go run ./cmd/ryui dev -mac examples/interaction
package main

import (
	"fmt"
	"strings"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func main() { app.Run(Interaction) }

func Interaction() ui.Node {
	return ui.Stack(home, ui.ScreenOptions{Title: ui.Val("Interaction")})
}

func home() ui.Node {
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.SafeArea(ui.Style{Gap: 24, Padding: ui.All(16)},
			section("Pressed and hovered", cards()),
			section("Hold", hold()),
			section("Drag between columns", board()),
			section("Drop from another app", dropZone()),
			section("Drag to another app", sources()),
		),
	)
}

func section(title string, content ui.Node) ui.Node {
	return ui.Column(ui.Style{Gap: 8},
		ui.StaticText(title, ui.Style{Font: ui.Headline}),
		content,
	)
}

// cards are pressable rows. Their own states are their style's variants:
// pressed, a card shrinks a little and darkens; hovered, it lightens and
// takes an accent border. Its title and chevron follow its hover too,
// through its Interaction (a group's hover).
func cards() ui.Node {
	presses := signal.New(0)
	var all []*ui.Interaction
	card := func(name string) ui.Node {
		it := ui.NewInteraction()
		all = append(all, it)
		accent := ui.Style{Color: ui.Accent}
		return ui.View(ui.ViewProps{
			OnPress:     func() { presses.Update(func(n int) int { return n + 1 }) },
			Interaction: it,
			Label:       name,
			Style: ui.Style{Direction: ui.RowDir, Align: ui.AlignCenter, Gap: 12, Padding: ui.All(16), CornerRadius: 12,
				Background: ui.SecondaryGroupedBackground, BorderWidth: 1, BorderColor: ui.Separator,
				Hovered: &ui.Style{Background: ui.SecondaryBackground, BorderColor: ui.Accent},
				Pressed: &ui.Style{Background: ui.Separator, Transform: ui.Transform{Scale: 0.97}}},
		},
			ui.Symbol(ui.IconStar),
			ui.StaticText(name, ui.Style{Grow: 1, Font: ui.Body}.When(it.Hovered, accent)),
			ui.Symbol(ui.IconForward, ui.Style{Color: ui.TertiaryLabel}.When(it.Hovered, accent)),
		)
	}
	// which is the name of the card in a state, read from their
	// Interactions.
	which := func(in func(*ui.Interaction) bool) string {
		for i, it := range all {
			if in(it) {
				return fmt.Sprintf("Card %d", i+1)
			}
		}
		return "none"
	}
	return ui.Column(ui.Style{Gap: 8},
		card("Card 1"), card("Card 2"), card("Card 3"),
		ui.Text(signal.Func(func() string {
			return fmt.Sprintf("Pressed: %s · Hovered: %s",
				which((*ui.Interaction).Pressed), which((*ui.Interaction).Hovered))
		}), ui.Style{Color: ui.SecondaryLabel}),
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Presses: %d", presses.Get()) }), ui.Style{Color: ui.SecondaryLabel}),
	)
}

// hold is a press held, with no OnPress: recording while it's down. The
// recording is a side effect, so it's OnPressedChange's; the look is the
// variant's, and the Interaction's for the text and symbol inside (text
// color isn't inherited).
func hold() ui.Node {
	it := ui.NewInteraction()
	recording := signal.New(false)
	onRed := ui.Style{}.When(it.Pressed, ui.Style{Color: ui.RGB(255, 255, 255)})
	return ui.View(ui.ViewProps{
		OnPressedChange: recording.Set, // start and stop the recorder here
		Interaction:     it,
		Label:           "Hold to record",
		Style: ui.Style{Direction: ui.RowDir, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Gap: 8, Padding: ui.All(16), CornerRadius: 28,
			Background: ui.SecondaryBackground,
			Pressed:    &ui.Style{Background: ui.Red}},
	},
		ui.Symbol(ui.IconMic, onRed),
		ui.Text(signal.Func(func() string {
			if recording.Get() {
				return "Recording…"
			}
			return "Hold to record"
		}), onRed),
	)
}

type task struct {
	id    int
	title string
	done  bool
}

// board is two columns of cards: a card dragged to the other column
// moves there. The drag carries the *task itself.
func board() ui.Node {
	tasks := signal.NewFunc([]*task{
		{1, "Write the design", true},
		{2, "Implement the hosts", false},
		{3, "Test on devices", false},
	}, nil)
	move := func(t *task, done bool) {
		next := make([]*task, 0, len(tasks.Peek()))
		for _, x := range tasks.Peek() {
			if x != t {
				next = append(next, x)
			}
		}
		next = append(next, &task{t.id, t.title, done})
		tasks.Set(next)
	}
	column := func(title string, done bool) ui.Node {
		mine := signal.Func(func() []*task {
			var out []*task
			for _, t := range tasks.Get() {
				if t.done == done {
					out = append(out, t)
				}
			}
			return out
		})
		return ui.View(ui.ViewProps{
			Drop: &ui.DropTarget{
				Accept: []string{"application/x-ryui-task"},
				OnDrop: func(d ui.Drop) {
					for _, it := range d.Items {
						if t, ok := it.Value.(*task); ok && t.done != done {
							ui.Animate(func() { move(t, done) })
						}
					}
				},
			},
			Style: ui.Style{Grow: 1, Basis: ui.Pt(0), Gap: 8, Padding: ui.All(8), CornerRadius: 12, MinHeight: ui.Pt(160),
				Background: ui.SecondaryBackground, BorderWidth: 2, BorderColor: ui.Clear,
				DragOver: &ui.Style{BorderColor: ui.Accent}},
		},
			ui.StaticText(title, ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}),
			ui.For(mine, func(t *task) int { return t.id }, func(t signal.Accessor[*task], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					Drag: &ui.DragSource{
						Items: func() []ui.DragItem {
							return []ui.DragItem{{Value: t.Get(), MIME: "application/x-ryui-task", Text: t.Get().title}}
						},
					},
					Style: ui.Style{Padding: ui.All(12), CornerRadius: 8, Background: ui.SystemBackground,
						Dragging: &ui.Style{Opacity: 0.4}},
				}, ui.Text(signal.Func(func() string { return t.Get().title })))
			}),
		)
	}
	return ui.Row(ui.Style{Gap: 8, Align: ui.AlignStretch},
		column("To do", false),
		column("Done", true),
	)
}

// dropZone takes anything another app drags in: files, photos, links,
// text.
func dropZone() ui.Node {
	dropped := signal.New("Drop files, photos, links or text here")
	return ui.View(ui.ViewProps{
		Drop: &ui.DropTarget{
			Accept: []string{"*/*"},
			OnDrop: func(d ui.Drop) {
				var lines []string
				for _, it := range d.Items {
					switch {
					case it.IsFile():
						lines = append(lines, fmt.Sprintf("%s (%s, %d bytes)", it.Name, it.MIME, it.Size))
					default:
						lines = append(lines, fmt.Sprintf("%q (%s)", it.Text, it.MIME))
					}
				}
				from := "another app"
				if d.Local {
					from = "this app"
				}
				dropped.Set(fmt.Sprintf("From %s:\n%s", from, strings.Join(lines, "\n")))
			},
		},
		Style: ui.Style{Align: ui.AlignCenter, Justify: ui.JustifyCenter, Padding: ui.All(24), MinHeight: ui.Pt(120), CornerRadius: 12,
			BorderWidth: 2, BorderColor: ui.Separator, Background: ui.SecondaryBackground,
			DragOver: &ui.Style{BorderColor: ui.Accent, Background: ui.SecondaryGroupedBackground}},
	}, ui.Text(dropped, ui.Style{TextAlign: ui.TextCenter, Color: ui.SecondaryLabel}))
}

// sources drag a text and a link out of the app (to Notes, a browser, a
// text field), and into its drop zone.
func sources() ui.Node {
	chip := func(label string, item ui.DragItem) ui.Node {
		return ui.View(ui.ViewProps{
			Drag: &ui.DragSource{Items: func() []ui.DragItem { return []ui.DragItem{item} }},
			Style: ui.Style{Padding: ui.Symmetric(8, 14), CornerRadius: 16, Background: ui.SecondaryBackground,
				Dragging: &ui.Style{Opacity: 0.4}},
		}, ui.StaticText(label))
	}
	return ui.Row(ui.Style{Gap: 8, Wrap: true},
		chip("Some text", ui.DragItem{Text: "Hello from RyUI"}),
		chip("A link", ui.DragItem{Kind: ui.SharedLink, Text: "https://github.com/egoist/ryui"}),
	)
}
