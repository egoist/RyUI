package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/ryui/notify"
	"github.com/egoist/ryui/push"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/store"
	"github.com/egoist/ryui/ui"
)

// What a messaging app does with notifications: a chat's messages as a
// conversation, a reply typed in the notification, threads read and
// cleared, channels, the badge, the chat on screen kept quiet, and pushes
// rewritten before they show.

var (
	ana   = notify.Person{ID: "ana", Name: "Ana", Image: "https://i.pravatar.cc/128?u=ana"}
	bo    = notify.Person{ID: "bo", Name: "Bo", Image: "https://i.pravatar.cc/128?u=bo"}
	climb = notify.Conversation{ID: "climbing", Title: "Climbing", Group: true}
	// chatOnScreen is the chat whose screen shows: its notifications don't
	// (Present). Main thread.
	chatOnScreen string
)

// setUpMessages declares the categories and channels, and answers the
// actions: in the root component, so a reply that wakes the app finds it.
func setUpMessages(status *signal.Signal[string]) {
	notify.SetCategories(
		notify.Category{ID: "message", Actions: []notify.Action{
			{ID: "reply", Title: "Reply", Icon: "arrowshape.turn.up.left", Reply: &notify.Reply{Button: "Send", Placeholder: "Message"}},
			{ID: "read", Title: "Mark as Read", Icon: "checkmark"},
			{ID: "mute", Title: "Mute for an Hour", Icon: "bell.slash", Destructive: true},
		}},
		notify.Category{ID: "reminder", Actions: []notify.Action{
			{ID: "open", Title: "Open", Open: true},
			{ID: "later", Title: "In 5 Seconds"},
		}},
	)
	notify.SetChannelGroups(notify.ChannelGroup{ID: "chats", Name: "Chats"})
	notify.SetChannels(
		notify.Channel{ID: "messages", Name: "Messages", Description: "What people send you", Importance: notify.ImportanceHigh, Group: "chats"},
		notify.Channel{ID: "reminders", Name: "Reminders", Importance: notify.ImportanceDefault},
	)
	notify.OnAction(func(r notify.Response) {
		n := r.Notification
		switch r.Action {
		case "":
			status.Set("Opened " + n.Title)
		case "reply":
			// Sending takes a moment: the app keeps running until it's done.
			_, done := r.Defer()
			go func() {
				defer done()
				time.Sleep(time.Second) // the server's round trip
				log.Printf("services: replied %q in %s", r.Reply, n.Thread)
				ui.Post(func() { status.Set(fmt.Sprintf("Replied %q", r.Reply)) })
			}()
		case "read":
			notify.RemoveThread(n.Thread)
			notify.SetBadge(0)
			status.Set("Read " + n.Thread)
		case "mute":
			status.Set("Muted " + n.Thread)
		case "later":
			go notify.Schedule(notify.Notification{ID: n.ID, Title: n.Title, Body: n.Body, Category: "reminder", Channel: "reminders", After: 5 * time.Second})
		}
	})
	notify.Present(func(n notify.Delivered) notify.Presentation {
		if n.Thread != "" && n.Thread == chatOnScreen {
			return notify.Hide
		}
		return notify.Banner
	})
}

// rewriteMessage is the app's push rewrite (push.Rewrite): its server
// sends the message sealed ("box", base64 standing in for a real
// cipher), the sender and the chat in the data; it shows as a message in
// the chat. It runs in iOS's Notification Service Extension, and in the
// app's process on Android.
func rewriteMessage(m *push.Message) error {
	if box := m.Data["box"]; box != "" {
		text, err := base64.StdEncoding.DecodeString(box)
		if err != nil {
			return err
		}
		m.Body = string(text)
	}
	if name := m.Data["sender"]; name != "" {
		chat := notify.Conversation{ID: m.Data["chat"], Title: m.Data["chatTitle"], Group: m.Data["chatTitle"] != ""}
		m.From(notify.Person{ID: m.Data["senderID"], Name: name, Image: m.Data["avatar"]}, chat)
		m.Category = "message"
		m.Channel = "messages"
	}
	// What the app keeps is the same in the extension: count what came.
	var count int
	store.Get("pushes", &count)
	store.Set("pushes", count+1)
	store.Flush()
	return nil
}

// messagesScreen is the Climbing chat: while it shows, its notifications
// don't.
func messagesScreen() ui.Node {
	status := signal.New("The Climbing chat: its notifications don't show while you're here.")
	chatOnScreen = climb.ID
	signal.OnCleanup(func() { chatOnScreen = "" })
	run := func(f func() string) func() {
		return func() {
			go func() {
				s := f()
				ui.Post(func() { status.Set(s) })
			}()
		}
	}
	item := func(title string, f func()) ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val(title), Variant: ui.ButtonTinted, OnPress: f})
	}
	message := func(from notify.Person, text string, after time.Duration) func() string {
		return func() string {
			if ok, err := notify.Request(); err != nil || !ok {
				return "Allow notifications in Settings"
			}
			n := notify.Notification{Body: text, Category: "message", Channel: "messages", Badge: 1, Data: map[string]string{"chat": climb.ID}, After: after}.From(from, climb)
			var err error
			if after > 0 {
				err = notify.Schedule(n)
			} else {
				err = notify.Show(n)
			}
			if err != nil {
				return err.Error()
			}
			return "Leave this screen (or the app) to see it"
		}
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 10},
			ui.Text(status, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
			// One at a time: a message scheduled with the conversation's
			// ID replaces the one waiting.
			item("Ana writes in 5 s", run(message(ana, "Who's in for Saturday?", 5*time.Second))),
			item("Bo answers in 5 s", run(message(bo, "Me! The usual crag?", 5*time.Second))),
			item("Ana writes now (hidden here)", run(message(ana, "Bring the long rope", 0))),
			item("A reminder with actions in 5 s", run(func() string {
				err := notify.Schedule(notify.Notification{ID: "pack", Title: "Pack your bag", Body: "Shoes, harness, chalk.",
					Category: "reminder", Channel: "reminders", Image: "https://picsum.photos/seed/crag/600/400", After: 5 * time.Second})
				if err != nil {
					return err.Error()
				}
				return "In 5 seconds: press it for its actions"
			})),
			item("What shows", run(func() string {
				ns, err := notify.Showing()
				if err != nil {
					return err.Error()
				}
				s := fmt.Sprintf("%d showing", len(ns))
				for _, n := range ns {
					s += fmt.Sprintf("\n%s (%s): %s", n.Title, n.Thread, n.Body)
				}
				return s
			})),
			item("Mark Climbing as read", func() {
				notify.RemoveThread(climb.ID)
				notify.SetBadge(0)
				status.Set("Read: its notifications and the badge are gone")
			}),
			item("Badge 3", func() { notify.SetBadge(3) }),
			item("Remove all", func() { notify.RemoveAll(); notify.SetBadge(0) }),
			item("Pushes rewritten so far", func() {
				var count int
				store.Get("pushes", &count)
				status.Set(fmt.Sprintf("%d (kept in %s)", count, filepath.Base(store.Dir())))
			}),
			ui.StaticText(pushHelp(), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
		),
	)
}

// pushHelp says how to send this app a push that's rewritten.
func pushHelp() string {
	box := base64.StdEncoding.EncodeToString([]byte("See you at the crag at 9"))
	if os.Getenv("RYUI_REMOTE") != "" {
		return "Under ryui dev's player, pushes aren't rewritten: run with -once to try it."
	}
	keys := `"box":"` + box + `","sender":"Ana","senderID":"ana","chat":"climbing","chatTitle":"Climbing"`
	if runtime.GOOS == "android" {
		return "A rewritten push: an FCM data message with " + keys + "."
	}
	return `A rewritten push, from APNs: aps {"alert":{"title":"New message","body":"(encrypted)"},"mutable-content":1}, with ` + keys + " beside it."
}
