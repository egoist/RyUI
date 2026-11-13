// Web Push is the web e2e's app (e2e/webpush_test.go): it registers for
// pushes, shows what comes, what's tapped and answered, and schedules a
// notification, each on a line the test reads.
package main

import (
	"time"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/notify"
	"github.com/egoist/ryui/push"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func App() ui.Node {
	status := signal.New("status: ready")
	token := signal.New("token: none")
	received := signal.New("received: none")
	opened := signal.New("opened: none")
	action := signal.New("action: none")
	url := signal.New("url: none")

	notify.SetCategories(notify.Category{ID: "chat", Actions: []notify.Action{
		{ID: "like", Title: "Like"},
		{ID: "reply", Title: "Reply", Reply: &notify.Reply{Placeholder: "Message"}},
	}})
	push.OnReceive(func(m push.Message) { received.Set("received: " + m.Title + " " + m.Data["k"]) })
	push.OnOpen(func(m push.Message) { opened.Set("opened: " + m.Title) })
	notify.OnAction(func(r notify.Response) {
		action.Set("action: [" + r.Action + "] [" + r.Reply + "] " + r.Notification.ID)
	})
	ui.OnOpenURL(func(u string) { url.Set("url: " + u) })

	run := func(f func() string) func() {
		return func() {
			go func() {
				s := f()
				ui.Post(func() { status.Set("status: " + s) })
			}()
		}
	}
	return ui.SafeArea(ui.Style{Gap: 8, Padding: ui.All(16)},
		ui.Button(ui.ButtonProps{Title: ui.Val("Register"), OnPress: run(func() string {
			r, err := push.Register()
			if err != nil {
				return err.Error()
			}
			ui.Post(func() { token.Set("token: " + r.Token) })
			return "registered " + r.Service
		})}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Schedule"), OnPress: run(func() string {
			if ok, err := notify.Request(); !ok {
				return "not allowed " + errText(err)
			}
			if err := notify.Schedule(notify.Notification{ID: "later", Title: "Reminder", Body: "Due", After: 5 * time.Second}); err != nil {
				return err.Error()
			}
			return "scheduled"
		})}),
		ui.Text(status),
		ui.Text(received),
		ui.Text(opened),
		ui.Text(action),
		ui.Text(url),
		ui.Text(token),
	)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func main() { app.Run(App) }
