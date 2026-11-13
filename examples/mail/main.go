// Mail is an inbox and a composer: a List in sections by day (Today,
// Yesterday...) whose headers stick, swipe actions from both edges
// (Unread, Archive), selecting mails to archive together, and a Rich text
// field with a formatting toolbar for the message.
//
//	go run ./cmd/asuka dev -platform ios examples/mail
package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

type mail struct {
	ID      int
	From    string
	Subject string
	Preview string
	At      time.Time
	Unread  bool
}

var senders = []string{"Ada Lovelace", "Grace Hopper", "Linus Torvalds", "Barbara Liskov", "Ken Thompson"}
var subjects = []string{"Release notes for 1.0", "Lunch on Friday?", "Re: the list's anchor", "Design review", "Your invoice", "Weekly sync"}

func inbox(n int) []mail {
	out := make([]mail, n)
	at := time.Now()
	for i := range out {
		at = at.Add(-time.Duration(40+i*13%90) * time.Minute)
		out[i] = mail{ID: i + 1, From: senders[i%len(senders)], Subject: subjects[i%len(subjects)],
			Preview: "Here's where things stand, and what's left before we ship.", At: at, Unread: i%3 == 0}
	}
	return out
}

func day(t time.Time) string {
	y, m, d := t.Date()
	ny, nm, nd := time.Now().Date()
	switch {
	case y == ny && m == nm && d == nd:
		return "Today"
	case y == ny && m == nm && d == nd-1:
		return "Yesterday"
	}
	return t.Format("Monday, January 2")
}

func Mail() ui.Node {
	mails := signal.NewFunc(inbox(60), nil)
	selecting := signal.New(false)
	chosen := signal.NewFunc(map[int]bool{}, nil)
	composing := signal.New(false)
	at := func(i int) mail { return mails.Peek()[i] }
	update := func(id int, f func(*mail)) {
		ms := slices.Clone(mails.Peek())
		for i := range ms {
			if ms[i].ID == id {
				f(&ms[i])
			}
		}
		mails.Set(ms)
	}
	archive := func(ids map[int]bool) {
		mails.Set(slices.DeleteFunc(slices.Clone(mails.Peek()), func(m mail) bool { return ids[m.ID] }))
	}
	selectRow := func(i int, v bool) {
		m := maps.Clone(chosen.Peek())
		if v {
			m[at(i).ID] = true
		} else {
			delete(m, at(i).ID)
		}
		chosen.Set(m)
	}

	row := func(item signal.Accessor[mail], _ signal.Accessor[int]) ui.Node {
		return ui.Row(ui.Style{Gap: 10, Padding: ui.Symmetric(10, 16), Align: ui.AlignStart},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				s := ui.Style{Width: ui.Pt(10), Height: ui.Pt(10), CornerRadius: 5, Margin: ui.Edges{Top: 6}}
				if item.Get().Unread {
					s.Background = ui.Accent
				}
				return s
			})}),
			ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 2},
				ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Gap: 8},
					ui.Text(signal.Func(func() string { return item.Get().From }), ui.Style{Font: ui.Headline, Lines: 1, Shrink: 1}),
					ui.Text(signal.Func(func() string { return item.Get().At.Format("15:04") }), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
				),
				ui.Text(signal.Func(func() string { return item.Get().Subject }), ui.Style{Lines: 1}),
				ui.Text(signal.Func(func() string { return item.Get().Preview }), ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel, Lines: 2}),
			),
		)
	}

	list := ui.List(ui.ListProps{
		Section: func(i int) string { return day(at(i).At) },
		Header: func(d signal.Accessor[string]) ui.Node {
			return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(6, 16)}},
				ui.Text(d, ui.Style{Font: ui.Footnote.Weighted(ui.Semibold), Color: ui.SecondaryLabel}))
		},
		LeadingSwipeActions: func(i int) []ui.Action {
			m := at(i)
			title := "Unread"
			if m.Unread {
				title = "Read"
			}
			return []ui.Action{{Title: ui.Val(title), Icon: ui.IconMail, OnPress: func() { update(m.ID, func(x *mail) { x.Unread = !x.Unread }) }}}
		},
		SwipeActions: func(i int) []ui.Action {
			id := at(i).ID
			return []ui.Action{{Title: ui.Val("Archive"), Icon: "archivebox", OnPress: func() { archive(map[int]bool{id: true}) }}}
		},
		Selecting:     selecting,
		Selected:      func(i int) bool { return chosen.Get()[mails.Peek()[i].ID] },
		OnSelect:      selectRow,
		OnSelectBegin: func(i int) { selecting.Set(true); selectRow(i, true) },
	}, mails, func(m mail) int { return m.ID }, row)

	return ui.Stack(func() ui.Node {
		return ui.SafeArea(ui.Style{Grow: 1},
			list,
			ui.Show(selecting, func() ui.Node {
				return ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 16)},
					ui.Text(signal.Func(func() string { return fmt.Sprintf("%d selected", len(chosen.Get())) })),
					ui.Button(ui.ButtonProps{Title: ui.Val("Archive"),
						Disabled: signal.Func(func() bool { return len(chosen.Get()) == 0 }),
						OnPress: func() {
							archive(chosen.Peek())
							selecting.Set(false)
							chosen.Set(map[int]bool{})
						}}),
				)
			}),
			ui.Sheet(ui.SheetProps{Open: composing, Content: func() ui.Node { return compose(composing) }}),
		)
	}, ui.ScreenOptions{
		Title:      ui.Val("Inbox"),
		LargeTitle: true,
		Actions: []ui.Action{
			{Title: signal.Func(func() string {
				if selecting.Get() {
					return "Done"
				}
				return "Select"
			}), OnPress: func() {
				selecting.Set(!selecting.Peek())
				chosen.Set(map[int]bool{})
			}},
			{Title: ui.Val("Compose"), Icon: ui.IconCompose, OnPress: func() { composing.Set(true) }},
		},
	})
}

// compose is a message with a formatted body: the toolbar's buttons show
// the styles at the cursor and set them.
func compose(open *signal.Signal[bool]) ui.Node {
	to := signal.New("")
	subject := signal.New("")
	body := signal.NewFunc(ui.Rich{ui.Plain("Hi,\n\nThe "), ui.Strong("1.0"), ui.Plain(" build is ready. Notes are "), ui.Link("here", "https://example.com/notes"), ui.Plain(".\n\nThanks!")}, nil)
	formats := signal.New(ui.TextFormats{})
	var field ui.Node
	style := func(f ui.TextFormat) ui.Styler {
		return ui.StyleFunc(func() ui.Style {
			if formats.Get().Has(f) {
				return ui.Style{Background: ui.SecondaryBackground, CornerRadius: 8}
			}
			return ui.Style{}
		})
	}
	tool := func(icon ui.Icon, label string, f ui.TextFormat) ui.Node {
		return ui.Button(ui.ButtonProps{Icon: icon, Label: ui.Val(label), Style: style(f),
			OnPress: func() { ui.ToggleFormat(field, f) }})
	}
	field = ui.TextField(ui.TextFieldProps{
		Rich: body, Plain: true, Placeholder: ui.Val("Message"),
		OnFormats: formats.Set,
		Style:     ui.Style{Grow: 1, Padding: ui.Symmetric(8, 16)},
	})
	line := func(label string, v *signal.Signal[string], kb ui.KeyboardType) ui.Node {
		return ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 16)},
			ui.StaticText(label, ui.Style{Color: ui.SecondaryLabel}),
			ui.TextField(ui.TextFieldProps{Value: v, Plain: true, Keyboard: kb, Autocapitalize: ui.CapitalizeNone, Style: ui.Style{Grow: 1}}),
		)
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 16)},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), OnPress: func() { open.Set(false) }}),
			ui.StaticText("New Message", ui.Style{Font: ui.Headline}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Send"), Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return strings.TrimSpace(to.Get()) == "" }),
				OnPress:  func() { open.Set(false) }}),
		),
		line("To:", to, ui.KeyboardEmail),
		line("Subject:", subject, ui.KeyboardDefault),
		field,
		ui.Row(ui.Style{Gap: 4, Padding: ui.Symmetric(6, 12)},
			tool("bold", "Bold", ui.FormatBold),
			tool("italic", "Italic", ui.FormatItalic),
			tool("underline", "Underline", ui.FormatUnderline),
			tool("strikethrough", "Strikethrough", ui.FormatStrike),
			tool("chevron.left.forwardslash.chevron.right", "Code", ui.FormatMono),
			ui.Button(ui.ButtonProps{Icon: "link", Label: ui.Val("Link"),
				Style: ui.StyleFunc(func() ui.Style {
					if formats.Get().Link != "" {
						return ui.Style{Background: ui.SecondaryBackground, CornerRadius: 8}
					}
					return ui.Style{}
				}),
				OnPress: func() {
					if formats.Peek().Link != "" {
						ui.SetLink(field, "")
					} else {
						ui.SetLink(field, "https://example.com")
					}
				}}),
		),
	)
}

func main() { app.Run(Mail) }
