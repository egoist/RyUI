// Chat is a conversation: a List anchored at its bottom (it opens at
// the newest message and stays there as messages come), older messages
// loaded as the top comes into view (OnStartReached) without moving what
// shows, a day per section with its header pinned at the top, swipe
// actions both ways, selecting messages to delete, and a composer that
// rides on the keyboard, inserts at the cursor and takes pasted images.
//
//	go run ./cmd/asuka dev --platform ios examples/chat
package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

type message struct {
	ID    int
	From  string // "" is me
	Text  string
	Image string // a pasted image's file
	At    time.Time
}

var people = []string{"Ada", "Grace", "Linus"}

var lines = []string{
	"Did the build pass?", "Shipping it after lunch.", "Can you look at the list's anchor?",
	"The sticky headers look right on Android now.", "👍", "Let's keep the API small.",
	"I pushed a fix for the keyboard.", "Who's on call this week?", "Reviewing now.",
	"The chat example scrolls nicely.", "Meeting moved to 3pm.", "Coffee?",
}

// history makes n messages ending before the first ID and time given.
func history(firstID int, before time.Time, n int) []message {
	out := make([]message, n)
	at := before
	for i := n - 1; i >= 0; i-- {
		at = at.Add(-time.Duration(7+(firstID+i)%50) * time.Minute * 9)
		id := firstID - n + i
		from := people[(id%3+3)%3]
		if id%4 == 0 {
			from = ""
		}
		out[i] = message{ID: id, From: from, Text: lines[(id%len(lines)+len(lines))%len(lines)], At: at}
	}
	return out
}

func day(t time.Time) string {
	now := time.Now()
	y, m, d := t.Date()
	ny, nm, nd := now.Date()
	switch {
	case y == ny && m == nm && d == nd:
		return "Today"
	case y == ny && m == nm && d == nd-1:
		return "Yesterday"
	case y == ny:
		return t.Format("Monday, January 2")
	}
	return t.Format("January 2, 2006")
}

func Chat() ui.Node {
	msgs := signal.NewFunc(history(1000, time.Now(), 40), nil)
	oldest := 960 - 400 // the history's start
	loading := false
	selecting := signal.New(false)
	chosen := signal.NewFunc(map[int]bool{}, nil)
	draft := signal.New("")
	sel := signal.New(ui.TextRange{})
	var field, list ui.Node
	nextID := 1000

	at := func(i int) message { return msgs.Peek()[i] }
	loadOlder := func() {
		first := msgs.Peek()[0]
		if loading || first.ID <= oldest {
			return
		}
		loading = true
		// As a server would answer.
		go func() {
			time.Sleep(400 * time.Millisecond)
			ui.Post(func() {
				loading = false
				older := history(first.ID, first.At, 30)
				msgs.Set(append(older, msgs.Peek()...))
			})
		}()
	}
	add := func(m message) {
		nextID++
		m.ID, m.At = nextID, time.Now()
		msgs.Set(append(slices.Clone(msgs.Peek()), m))
	}
	send := func() {
		text := strings.TrimSpace(draft.Peek())
		if text == "" {
			return
		}
		add(message{Text: text})
		draft.Set("")
		// One's own message shows, wherever the list was.
		ui.ScrollToEnd(list, true)
		// Someone answers.
		go func() {
			time.Sleep(1200 * time.Millisecond)
			ui.Post(func() { add(message{From: "Ada", Text: "Got it: “" + text + "”"}) })
		}()
	}
	remove := func(ids map[int]bool) {
		var keep []message
		for _, m := range msgs.Peek() {
			if !ids[m.ID] {
				keep = append(keep, m)
			}
		}
		msgs.Set(keep)
	}
	selectRow := func(i int, v bool) {
		m := map[int]bool{}
		for k, x := range chosen.Peek() {
			if x {
				m[k] = true
			}
		}
		if v {
			m[at(i).ID] = true
		} else {
			delete(m, at(i).ID)
		}
		chosen.Set(m)
	}
	endSelecting := func() {
		selecting.Set(false)
		chosen.Set(map[int]bool{})
	}

	bubble := func(item signal.Accessor[message]) ui.Node {
		return ui.Column(ui.StyleFunc(func() ui.Style {
			s := ui.Style{Padding: ui.Symmetric(8, 12), CornerRadius: 18, MaxWidth: ui.Pct(78), Gap: 2}
			if item.Get().From == "" {
				s.AlignSelf, s.Background = ui.AlignEnd, ui.Accent
			} else {
				s.AlignSelf, s.Background = ui.AlignStart, ui.SecondaryBackground
			}
			return s
		}),
			ui.Show(signal.Func(func() bool { return item.Get().From != "" }), func() ui.Node {
				return ui.Text(signal.Func(func() string { return item.Get().From }), ui.Style{Font: ui.Caption.Weighted(ui.Semibold), Color: ui.SecondaryLabel})
			}),
			ui.Show(signal.Func(func() bool { return item.Get().Image != "" }), func() ui.Node {
				return ui.Image(ui.ImageProps{Source: signal.Func(func() string { return item.Get().Image }), Mode: ui.ImageFill,
					Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(150), CornerRadius: 12}})
			}),
			ui.Show(signal.Func(func() bool { return item.Get().Text != "" }), func() ui.Node {
				return ui.Text(signal.Func(func() string { return item.Get().Text }), ui.StyleFunc(func() ui.Style {
					if item.Get().From == "" {
						return ui.Style{Color: ui.OnAccent}
					}
					return ui.Style{}
				}))
			}),
		)
	}

	list = ui.List(ui.ListProps{
		Appearance:     ui.ListBare,
		Anchor:         ui.AnchorBottom,
		OnStartReached: loadOlder,
		Section:        func(i int) string { return day(at(i).At) },
		Header: func(d signal.Accessor[string]) ui.Node {
			return ui.Row(ui.Style{Justify: ui.JustifyCenter, Padding: ui.Symmetric(6, 0)},
				ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(3, 10), CornerRadius: 999, Background: ui.SecondaryBackground}},
					ui.Text(d, ui.Style{Font: ui.Caption.Weighted(ui.Semibold), Color: ui.SecondaryLabel})))
		},
		LeadingSwipeActions: func(i int) []ui.Action {
			m := at(i)
			return []ui.Action{{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", OnPress: func() {
				who := m.From
				if who == "" {
					who = "me"
				}
				draft.Set("@" + who + " ")
				sel.Set(ui.TextRange{Start: len(draft.Peek()), End: len(draft.Peek())})
				ui.Focus(field)
			}}}
		},
		SwipeActions: func(i int) []ui.Action {
			id := at(i).ID
			return []ui.Action{{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(map[int]bool{id: true}) }}}
		},
		Selecting:     selecting,
		Selected:      func(i int) bool { return chosen.Get()[msgs.Peek()[i].ID] },
		OnSelect:      selectRow,
		OnSelectBegin: func(i int) { selecting.Set(true); selectRow(i, true) },
	}, msgs, func(m message) int { return m.ID }, func(item signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(3, 12)}}, bubble(item))
	})

	composer := func() ui.Node {
		return ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd, Padding: ui.Symmetric(8, 12)},
			ui.Button(ui.ButtonProps{Icon: "face.smiling", OnPress: func() { ui.InsertText(field, "😀") },
				Label: ui.Val("Insert an emoji")}),
			func() ui.Node {
				field = ui.TextField(ui.TextFieldProps{
					Value: draft, Selection: sel, Multiline: true,
					Placeholder: ui.Val("Message"),
					PasteTypes:  []string{"image/*"},
					OnPaste: func(items []ui.SharedItem) {
						for _, it := range items {
							add(message{Image: it.Path})
						}
					},
					Style: ui.Style{Grow: 1, Lines: 5},
				})
				return field
			}(),
			ui.Button(ui.ButtonProps{Icon: ui.IconSend, Variant: ui.ButtonFilled, OnPress: send,
				Label:    ui.Val("Send"),
				Disabled: signal.Func(func() bool { return strings.TrimSpace(draft.Get()) == "" })}),
		)
	}
	toolbar := func() ui.Node {
		return ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 16)},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("%d selected", len(chosen.Get())) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Delete"), Style: ui.Style{Color: ui.Red},
				Disabled: signal.Func(func() bool { return len(chosen.Get()) == 0 }),
				OnPress:  func() { remove(chosen.Peek()); endSelecting() }}),
		)
	}

	return ui.Stack(func() ui.Node {
		return ui.SafeArea(ui.Style{Grow: 1},
			list,
			ui.Show(selecting, toolbar, composer),
		)
	}, ui.ScreenOptions{
		Title: ui.Val("Design Team"),
		Actions: []ui.Action{{
			Title: signal.Func(func() string {
				if selecting.Get() {
					return "Done"
				}
				return "Select"
			}),
			OnPress: func() {
				if selecting.Peek() {
					endSelecting()
				} else {
					selecting.Set(true)
				}
			},
		}},
	})
}

func main() { app.Run(Chat) }
