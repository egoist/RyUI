// Gallery shows nui's components, one section each.
//
//	go run ./cmd/nui dev -ios examples/gallery
package main

import (
	"fmt"
	"time"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/store"
	"github.com/egoist/nui/ui"
)

func App() ui.Node {
	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 28},
				ui.Button(ui.ButtonProps{Title: ui.Val("Open the inbox (a List)"), Icon: ui.IconMail, Variant: ui.ButtonBordered,
					OnPress: func() {
						nav.Push(inbox, ui.ScreenOptions{Title: ui.Val("Inbox"), Actions: []ui.Action{
							{Title: "Top", Icon: ui.IconUp, OnPress: func() { ui.ScrollToItem(inboxList, 0, true) }},
						}})
					}}),
				symbols(),
				textInput(),
				controls(),
				dates(),
				services(),
			),
		)
	}, ui.ScreenOptions{Title: ui.Val("Gallery"), Actions: []ui.Action{
		{Title: "Share", Icon: ui.IconShare, OnPress: func() {}},
	}})
}

// section is a titled group of examples.
func section(title string, kids ...ui.Node) ui.Node {
	return ui.Column(ui.Style{Gap: 12},
		append([]ui.Node{ui.StaticText(title, ui.Style{Font: ui.Headline})}, kids...)...)
}

func symbols() ui.Node {
	liked := signal.New(false)
	icons := []ui.Icon{ui.IconHome, ui.IconSearch, ui.IconBell, ui.IconMail, ui.IconCamera,
		ui.IconCalendar, ui.IconLocation, ui.IconCart, ui.IconLock, ui.IconSettings}
	var row []ui.Node
	for _, i := range icons {
		row = append(row, ui.Symbol(i))
	}
	return section("Symbols",
		ui.Row(ui.Style{Gap: 14, Wrap: true}, row...),
		ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
			ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: ui.Yellow, Font: ui.Title}),
			ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: ui.Red, Font: ui.Title2}),
			ui.Symbol(ui.IconCheck, ui.Style{Color: ui.Green, Font: ui.Caption}),
		),
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonFilled}),
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
				if liked.Get() {
					return "Liked"
				}
				return "Like"
			}), Icon: ui.IconHeart, Variant: ui.ButtonTinted, OnPress: func() { liked.Set(!liked.Peek()) }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Menu: []ui.Action{
				{Title: "Copy", Icon: ui.IconCopy},
				{Title: "Edit", Icon: ui.IconEdit},
				{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive},
			}}),
		),
	)
}

func textInput() ui.Node {
	name := signal.New("")
	message := signal.New("")
	sent := signal.New("")
	return section("Text input",
		ui.TextField(ui.TextFieldProps{Value: name, Placeholder: ui.Val("Name"), ContentType: "name", Autocapitalize: ui.CapitalizeWords}),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd},
			ui.TextField(ui.TextFieldProps{Value: message, Placeholder: ui.Val("Message"), Multiline: true,
				Style: ui.Style{Grow: 1, Lines: 5}}),
			ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: "Send", Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return message.Get() == "" }),
				OnPress:  func() { sent.Set(message.Peek()); message.Set("") }}),
		),
		ui.Text(signal.Func(func() string {
			if sent.Get() == "" {
				return "Grows to five lines, then scrolls."
			}
			return "Sent: " + sent.Get()
		}), ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
	)
}

func controls() ui.Node {
	period := store.Signal("period", 1) // kept across launches
	volume := signal.New(0.4)
	wifi := signal.New(true)
	return section("Controls",
		ui.Segmented(ui.SegmentedProps{Segments: []string{"Day", "Week", "Month"}, Selected: period}),
		ui.Text(signal.Func(func() string {
			return []string{"Today", "This week", "This month"}[period.Get()]
		}), ui.Style{Color: ui.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Symbol(ui.IconSpeaker),
			ui.Slider(ui.SliderProps{Value: volume, Style: ui.Style{Grow: 1}}),
		),
		ui.Progress(volume),
		ui.Progress(nil),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(ui.Val("Wi-Fi"), ui.Style{Grow: 1}),
			ui.Toggle(ui.ToggleProps{Value: wifi}),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Activity(),
			ui.StaticText("Loading…", ui.Style{Color: ui.SecondaryLabel}),
		),
	)
}

func dates() ui.Node {
	when := signal.New(time.Now().Add(24 * time.Hour).Truncate(time.Hour))
	row := func(label string, mode ui.DateMode) ui.Node {
		return ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(ui.Val(label), ui.Style{Grow: 1}),
			ui.DatePicker(ui.DatePickerProps{Value: when, Mode: mode, Min: time.Now()}))
	}
	return section("Dates",
		row("Date", ui.DateOnly),
		row("Time", ui.TimeOnly),
		row("Starts", ui.DateAndTime),
		ui.Text(signal.Func(func() string { return when.Get().Format("Mon Jan 2, 15:04") }),
			ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
	)
}

type message struct {
	id      int
	from    string
	subject string
}

func messages(from, n int) []message {
	names := []string{"Ada", "Grace", "Linus", "Ken", "Rob", "Barbara"}
	var out []message
	for i := from; i < from+n; i++ {
		out = append(out, message{i, names[i%len(names)], fmt.Sprintf("Message %d", i+1)})
	}
	return out
}

// inbox is a List: swipe a row to archive or delete it; more load at the
// end; the bar's button scrolls back to the top.
func inbox() ui.Node {
	items := signal.NewFunc(messages(0, 30), nil)
	remove := func(i int) {
		l := append([]message(nil), items.Peek()...)
		items.Set(append(l[:i], l[i+1:]...))
	}
	inboxList = ui.List(ui.ListProps{
		SwipeActions: func(i int) []ui.Action {
			return []ui.Action{
				{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
				{Title: "Archive", Icon: "archivebox", OnPress: func() { remove(i) }},
			}
		},
		OnEndReached: func() {
			l := items.Peek()
			if len(l) < 120 {
				items.Set(append(append([]message(nil), l...), messages(len(l), 30)...))
			}
		},
	}, items, func(m message) int { return m.id }, func(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
		return ui.Row(ui.Style{Padding: ui.Symmetric(12, 20), Gap: 12, Align: ui.AlignCenter},
			ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: ui.SecondaryLabel, Font: ui.Title2}),
			ui.Column(ui.Style{Grow: 1, Gap: 2},
				ui.Text(signal.Func(func() string { return m.Get().from }), ui.Style{Font: ui.Headline}),
				ui.Text(signal.Func(func() string { return m.Get().subject }), ui.Style{Color: ui.SecondaryLabel}),
			),
		)
	})
	return inboxList
}

func services() ui.Node {
	copied := signal.New(false)
	return section("Services",
		ui.StaticText("The segment above is kept: relaunch to see it.", ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
				if copied.Get() {
					return "Copied"
				}
				return "Copy link"
			}), Icon: ui.IconCopy, Variant: ui.ButtonGray, OnPress: func() { ui.Copy("https://go.dev"); copied.Set(true) }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Share"), Icon: ui.IconShare, Variant: ui.ButtonGray,
				OnPress: func() { ui.Share("https://go.dev") }}),
		),
	)
}

// inboxList is the inbox's List, which the bar's button scrolls.
var inboxList ui.Node

func main() { app.Run(App) }
