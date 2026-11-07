package notify

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/egoist/ryui/internal/notifyspec"
	"github.com/egoist/ryui/ui"
)

// Category is a kind of notification and the actions it shows: buttons
// under it (iOS: when it's pressed long or pulled down; Android: in the
// shade), a reply field for a message.
type Category struct {
	ID      string
	Actions []Action
}

// Action is a button on a notification.
type Action struct {
	// ID names it in the Response.
	ID    string
	Title string
	// Icon is an SF Symbol's name (iOS 15 and macOS 12 show it); Android
	// shows actions without icons.
	Icon string
	// Destructive shows it as one (iOS: red).
	Destructive bool
	// Unlock asks the user to unlock the device before it runs (iOS;
	// Android 12 and later).
	Unlock bool
	// Open opens the app; without, it runs in the background: the app's
	// Go code runs (started for it, if the app isn't running) and gets
	// the Response, without the app coming to the front.
	Open bool
	// Reply makes it a text field: the user types and sends, and the
	// Response has the text.
	Reply *Reply
}

// Reply is an action's text field.
type Reply struct {
	// Button is the send button's title (iOS; Android shows a send icon).
	Button string
	// Placeholder is shown in the empty field.
	Placeholder string
}

// SetCategories sets the app's categories, in place of those set before.
// Set them as the app starts (in its root component), before
// notifications that name them come: pushes too, whose category is
// aps.category on iOS, the data's "category" on Android. They hold
// across launches. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func SetCategories(cs ...Category) {
	type reply struct {
		Button      string `json:"button"`
		Placeholder string `json:"placeholder"`
	}
	type action struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Icon        string `json:"icon,omitempty"`
		Destructive bool   `json:"destructive,omitempty"`
		Unlock      bool   `json:"unlock,omitempty"`
		Open        bool   `json:"open,omitempty"`
		Reply       *reply `json:"reply,omitempty"`
	}
	type category struct {
		ID      string   `json:"id"`
		Actions []action `json:"actions"`
	}
	out := make([]category, 0, len(cs))
	for _, c := range cs {
		wc := category{ID: c.ID, Actions: []action{}}
		for _, a := range c.Actions {
			wa := action{ID: a.ID, Title: a.Title, Icon: a.Icon, Destructive: a.Destructive, Unlock: a.Unlock, Open: a.Open}
			if a.Reply != nil {
				wa.Reply = &reply{Button: a.Reply.Button, Placeholder: a.Reply.Placeholder}
			}
			wc.Actions = append(wc.Actions, wa)
		}
		out = append(out, wc)
	}
	b, _ := json.Marshal(out)
	ui.Call("notify", "categories", b, nil)
}

// Response is the user's answer to a notification: an action, or a tap.
type Response struct {
	// Action is the action's ID; "" when the user tapped the
	// notification itself.
	Action string
	// Reply is what the user typed, for a Reply action.
	Reply string
	// Notification is the one answered.
	Notification Delivered

	work *work
}

// Defer, called in OnAction's f, keeps the app running for work the
// response starts that goes on after f returns (sending a reply to the server): the system
// lets the app finish until done is called, or ctx's deadline, a little
// before the system's (iOS gives about 30 seconds, Android about 10). Any
// goroutine; done can be called more than once.
//
// After a reply on Android, the notification is updated as Android wants
// once the work is done: a conversation shows the reply in it, another is
// taken away, unless the app showed it again (Show, with its ID) by then.
func (r Response) Defer() (ctx context.Context, done func()) {
	if r.work == nil {
		return context.Background(), func() {}
	}
	r.work.add()
	var once sync.Once
	return r.work.ctx, func() { once.Do(r.work.release) }
}

// work is a response's: what holds it (the handlers running, Defer's),
// and the deadline. The host keeps the app running until it's done.
type work struct {
	token  string
	ctx    context.Context
	cancel func()
	mu     sync.Mutex
	held   int
}

func (w *work) add() {
	w.mu.Lock()
	w.held++
	w.mu.Unlock()
}

func (w *work) release() {
	w.mu.Lock()
	w.held--
	last := w.held == 0
	w.mu.Unlock()
	if last {
		w.cancel()
		ui.Post(func() { ui.Call("notify", "actionDone", []byte(w.token), nil) })
	}
}

// OnAction calls f, on the main thread, with each response the user
// gives, until stop: a tap on a notification (Action "") or one of its
// actions, ryui/push's too. A response that came before any f (the one
// that launched the app, or woke it for a background action) waits for
// the first, so set it as the app starts (in its root component). A
// background action's work can go on after f returns (Response.Defer).
//
// A tap's URL also goes to ui.OnOpenURL, and a push's tap to push.OnOpen.
func OnAction(f func(r Response)) (stop func()) {
	return actions.watch(func(b []byte) {
		var wr notifyspec.Response
		if json.Unmarshal(b, &wr) != nil {
			return
		}
		secs := max(wr.Seconds, 1)
		// A margin before the system's deadline, to report it done.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second-time.Second/2)
		w := &work{token: wr.Token, ctx: ctx, cancel: cancel, held: 1}
		r := Response{Action: wr.Action, Reply: wr.Reply, Notification: delivered(wr.Notification), work: w}
		defer w.release()
		for _, f := range actions.handlers() {
			f.(func(Response))(r)
		}
	}, f)
}

var actions = &chain{method: "nextAction"}

// chain waits on the host for one kind of item, on the main thread: its
// method answers with the next one (the host keeps them until asked),
// "stop" (the method's name) fails the waiting call with "stopped".
type chain struct {
	method   string
	watchers []*any
	running  bool
	handle   func([]byte)
}

func (c *chain) handlers() []any {
	out := make([]any, len(c.watchers))
	for i, w := range c.watchers {
		out[i] = *w
	}
	return out
}

func (c *chain) watch(handle func([]byte), f any) (stop func()) {
	p := &f
	c.handle = handle
	c.watchers = append(c.watchers, p)
	if !c.running {
		c.running = true
		c.next()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for i, w := range c.watchers {
				if w == p {
					c.watchers = append(c.watchers[:i], c.watchers[i+1:]...)
					break
				}
			}
			if len(c.watchers) == 0 {
				ui.Call("notify", "stop", []byte(c.method), nil)
			}
		})
	}
}

func (c *chain) next() {
	ui.Call("notify", c.method, nil, func(b []byte, err error) {
		switch {
		case len(c.watchers) == 0:
			c.running = false
		case err == nil:
			c.handle(b)
			c.next()
		case err.Error() == "stopped":
			c.next() // stopped as another began watching: wait on for that one
		default:
			c.running = false
		}
	})
}
