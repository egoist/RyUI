//go:build windows

package windows

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/notifyspec"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// asuka/sdk/notify on Windows: toasts (WinRT's ToastNotificationManager), the
// module's methods as the other hosts have them. The app is registered
// with the shell as an unpackaged app is (a Start menu shortcut with its
// AppUserModelID, the AppUserModelId key) with a COM activator of its
// own (toastcom.go), so that a toast's click and its buttons reach the
// app: the running one, or one COM starts for them. A toast carries its
// notification (its ID, URL, data, category, thread, when it showed) in
// its arguments, JSON, which come back with a click, and which the
// notification center's history (ToastNotificationHistory) gives back
// for Showing.
//
// A category's actions are the toast's buttons (five at most), a reply
// a text box with its send button beside it; an action that opens the
// app activates it in the foreground, another in the background: the app
// gets the Response without coming to the front, and an app COM started
// for it quits once its Go code is done (actionDone) or the time's up,
// unless it came to the front meanwhile. The badge is the taskbar
// button's overlay with the count drawn on it (a packaged app's, the
// system's badge). What Windows lacks: Android's channels (ignored, as on
// iOS), threads (a thread's toasts aren't grouped; RemoveThread finds
// them), and a say in how a scheduled toast shows while the app is in
// front (Windows shows it; Present decides for those shown now).

func init() {
	launchedForToast = TakeToastLaunch()
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["notify"] = h.notifyModule()
	})
}

// launchedForToast: COM started the app for a click on one of its toasts.
var launchedForToast bool

// actionSeconds: what a background action has, as on iOS.
const actionSeconds = 25

// toastWait: how long an app COM started waits for the click it was
// started for before it shows its window anyway.
const toastWait = 10 * time.Second

// waitQueue is what Go waits for ("next..." methods). Main thread.
type waitQueue struct {
	items   [][]byte
	waiting []func([]byte, error)
}

func (q *waitQueue) next(reply func([]byte, error)) {
	if len(q.items) == 0 {
		q.waiting = append(q.waiting, reply)
		return
	}
	it := q.items[0]
	q.items = q.items[1:]
	reply(it, nil)
}

func (q *waitQueue) deliver(b []byte) {
	if len(q.waiting) == 0 {
		q.items = append(q.items, b)
		return
	}
	w := q.waiting[0]
	q.waiting = q.waiting[1:]
	w(b, nil)
}

func (q *waitQueue) stop() {
	ws := q.waiting
	q.waiting = nil
	for _, w := range ws {
		w(nil, errors.New("stopped"))
	}
}

// notifier is the module's state. Main thread.
type notifier struct {
	toaster        *Toaster
	activator      bool // the class object is registered
	presenting     bool
	presents       waitQueue
	actions        waitQueue
	pendingPresent map[string]func(byte)
	pendingActions map[string]func() // background actions, until done
	categories     []byte
	categoriesRead bool
	tokens         int
	badge          int
	badgeMsg       uint32 // TaskbarButtonCreated
	hidden         bool   // started for a toast, not shown yet
	stopWaiting    func()
}

func (h *Host) notes() *notifier {
	if h.svc.notes == nil {
		h.svc.notes = &notifier{pendingPresent: map[string]func(byte){}, pendingActions: map[string]func(){}}
	}
	return h.svc.notes
}

// appUserModelID is the app's ID for the shell: asuka.json's ID, else one
// of its name.
func (h *Host) appUserModelID() string {
	if boot.ID != "" {
		return boot.ID
	}
	var b strings.Builder
	for _, r := range h.appName {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		b.WriteString("App")
	}
	return "Asuka." + b.String()
}

// toastActivatorID is the app's activator's CLSID.
func (h *Host) toastActivatorID() GUID { return ToastCLSID(h.appUserModelID()) }

// toasts is the app's notifier, registered with the shell the first
// time (a Start menu shortcut, its name, its icon as a PNG in the
// caches, the activator).
func (h *Host) toasts() (*Toaster, error) {
	s := h.notes()
	if s.toaster != nil {
		return s.toaster, nil
	}
	id := h.appUserModelID()
	icon := ""
	if hi := AppIcon(); hi != 0 {
		p := filepath.Join(h.cacheDir(), "asuka-notify-icon.png")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil && WriteIconPNG(hi, 64, p) == nil {
			icon = p
		}
	}
	exe, _ := os.Executable()
	clsid := h.toastActivatorID()
	if err := RegisterAppID(id, h.appName, icon, exe, clsid); err != nil {
		return nil, fmt.Errorf("asuka/sdk/notify: %w", err)
	}
	if err := RegisterToastActivator(id, clsid, exe); err != nil {
		return nil, fmt.Errorf("asuka/sdk/notify: %w", err)
	}
	h.startActivator()
	t, err := NewToaster(id)
	if err != nil {
		return nil, fmt.Errorf("asuka/sdk/notify: %w", err)
	}
	s.toaster = t
	return t, nil
}

// startActivator registers the activator's class object, so that clicks
// on the app's toasts come to this process.
func (h *Host) startActivator() {
	s := h.notes()
	if s.activator {
		return
	}
	if err := StartToastActivator(h.toastActivatorID(), func(a ToastActivation) {
		// Activate is a COM call: the work waits until it's returned.
		h.Post(func() { h.toastActivated(a) })
	}); err != nil {
		fmt.Fprintf(os.Stderr, "asuka/sdk/notify: %v\n", err)
		return
	}
	s.activator = true
}

// startToasts readies the app for its toasts as it starts: its
// activator, if it has shown toasts before (or COM started it for one).
func (h *Host) startToasts() {
	if launchedForToast || ToastActivatorRegistered(h.toastActivatorID()) {
		h.startActivator()
	}
}

// waitForToast holds the window back while the app COM started waits
// for the click: one in the background leaves it hidden; one in the
// foreground, or none in time, shows it.
func (h *Host) waitForToast() {
	s := h.notes()
	s.hidden = true
	s.stopWaiting = ownTimer(uint32(toastWait/time.Millisecond), func() {
		if len(s.pendingActions) == 0 {
			h.toFront()
		}
	})
}

// toFront shows the app's window, in front.
func (h *Host) toFront() {
	s := h.notes()
	if s.stopWaiting != nil {
		s.stopWaiting()
		s.stopWaiting = nil
	}
	if s.hidden {
		s.hidden = false
		h.showMain()
		return
	}
	if !IsWindowVisible(h.window) {
		// Closed while the app keeps running: it shows again. A
		// background app's, which hasn't shown, is the app's to show.
		if !h.mainCtl.placed {
			return
		}
		h.showCtl(h.mainCtl, true)
		h.lifecycle(ops.LifecycleForeground)
	}
	if IsIconic(h.window) {
		ShowWindow(h.window, 9) // SW_RESTORE
	}
	SetForegroundWindow(h.window)
}

// toastArgs are a toast's arguments, and its buttons': the notification
// and the action.
type toastArgs struct {
	Asuka  int              `json:"asuka"`
	Action string           `json:"a,omitempty"`
	Note   notifyspec.Shown `json:"n"`
}

func (h *Host) newToken() string {
	s := h.notes()
	s.tokens++
	return time.Now().Format("150405.000000000") + "-" + strconv.Itoa(s.tokens)
}

// toastActivated is a click on one of the app's toasts, or on one of its
// buttons.
func (h *Host) toastActivated(a ToastActivation) {
	s := h.notes()
	var args toastArgs
	if json.Unmarshal([]byte(a.Args), &args) != nil || args.Asuka != 1 {
		h.toFront() // not one of ours: the app's opened
		return
	}
	if args.Note.Data == nil {
		args.Note.Data = map[string]string{}
	}
	token := h.newToken()
	b, _ := json.Marshal(notifyspec.Response{Token: token, Action: args.Action, Reply: a.Inputs[replyInput(args.Action)],
		Seconds: actionSeconds, Notification: args.Note})
	s.actions.deliver(b)
	if args.Action == "" || h.opensApp(args.Note.Category, args.Action) {
		h.toFront()
		if args.Action == "" && args.Note.URL != "" && h.root != nil {
			h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventIncomingURL, Str: args.Note.URL})
		}
		return
	}
	// A background action: Go says when it's done; an app started for it
	// quits then.
	stop := func() {}
	done := func() {
		if s.pendingActions[token] == nil {
			return
		}
		delete(s.pendingActions, token)
		stop()
		if s.hidden && len(s.pendingActions) == 0 {
			PostQuitMessage(0)
		}
	}
	s.pendingActions[token] = done
	stop = ownTimer(actionSeconds*1000, done)
}

// replyInput is the ID of an action's text box.
func replyInput(action string) string { return "r:" + action }

// toastCategory is a category's JSON (notify.SetCategories).
type toastCategory struct {
	ID      string `json:"id"`
	Actions []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Destructive bool   `json:"destructive"`
		Open        bool   `json:"open"`
		Reply       *struct {
			Button      string `json:"button"`
			Placeholder string `json:"placeholder"`
		} `json:"reply"`
	} `json:"actions"`
}

// categoriesFile keeps the categories across launches: an app COM starts
// for a click hears of them only once its Go code has set them.
func (h *Host) categoriesFile() string {
	d := os.Getenv("ASUKA_DATA")
	if d == "" {
		d = h.cacheDir()
	}
	return filepath.Join(d, "asuka-notify-categories.json")
}

func (h *Host) category(id string) *toastCategory {
	s := h.notes()
	if !s.categoriesRead {
		s.categoriesRead = true
		if s.categories == nil {
			s.categories, _ = os.ReadFile(h.categoriesFile())
		}
	}
	var cs []toastCategory
	json.Unmarshal(s.categories, &cs)
	for i := range cs {
		if cs[i].ID == id {
			return &cs[i]
		}
	}
	return nil
}

// opensApp reports whether category's action opens the app.
func (h *Host) opensApp(category, action string) bool {
	if c := h.category(category); c != nil {
		for _, a := range c.Actions {
			if a.ID == action {
				return a.Open
			}
		}
	}
	return false
}

// shownOfSpec is what's reported of a notification shown from a spec, at
// (Unix milliseconds).
func shownOfSpec(s notifyspec.Spec, at int64) notifyspec.Shown {
	data := s.Data
	if data == nil {
		data = map[string]string{}
	}
	title := s.Title
	if title == "" && s.Sender != nil {
		title = s.Sender.Name
	}
	return notifyspec.Shown{ID: s.ID, Title: title, Subtitle: s.Subtitle, Body: s.Body, URL: s.URL, Data: data,
		Category: s.Category, Thread: s.Thread, Time: at}
}

// toastArgsJSON is a toast's or a button's arguments: the notification,
// its body cut short (the toast's XML is 5 KB at most, and the
// notification is in it once for each button).
func toastArgsJSON(n notifyspec.Shown, action string) string {
	if r := []rune(n.Body); len(r) > 240 {
		n.Body = string(r[:240])
	}
	b, _ := json.Marshal(toastArgs{Asuka: 1, Action: action, Note: n})
	return string(b)
}

// toastXML is a notification's toast: its title (a message's sender),
// subtitle and body, an image, silent or not, its category's actions,
// and its arguments.
func (h *Host) toastXML(s notifyspec.Spec, shown notifyspec.Shown, image string) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<toast launch="` + esc(toastArgsJSON(shown, "")) + `"`)
	c := h.category(s.Category)
	if c != nil {
		for _, a := range c.Actions {
			if a.Destructive {
				b.WriteString(` useButtonStyle="true"`)
				break
			}
		}
	}
	b.WriteString(`><visual><binding template="ToastGeneric">`)
	if shown.Title != "" {
		b.WriteString(`<text>` + esc(shown.Title) + `</text>`)
	}
	if s.Subtitle != "" {
		b.WriteString(`<text>` + esc(s.Subtitle) + `</text>`)
	}
	if s.Body != "" {
		b.WriteString(`<text>` + esc(s.Body) + `</text>`)
	}
	if image != "" {
		b.WriteString(`<image src="` + esc((&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(image)}).String()) + `"/>`)
	}
	b.WriteString(`</binding></visual>`)
	if c != nil && len(c.Actions) > 0 {
		b.WriteString(`<actions>`)
		n := 0
		for _, a := range c.Actions {
			if a.Reply != nil && n < 5 {
				b.WriteString(`<input id="` + esc(replyInput(a.ID)) + `" type="text" placeHolderContent="` + esc(a.Reply.Placeholder) + `"/>`)
				n++
			}
		}
		for i, a := range c.Actions {
			if i == 5 {
				break
			}
			title := a.Title
			if a.Reply != nil && a.Reply.Button != "" {
				title = a.Reply.Button
			}
			kind := "background"
			if a.Open {
				kind = "foreground"
			}
			b.WriteString(`<action content="` + esc(title) + `" arguments="` + esc(toastArgsJSON(shown, a.ID)) + `" activationType="` + kind + `"`)
			if a.Reply != nil {
				b.WriteString(` hint-inputId="` + esc(replyInput(a.ID)) + `"`)
			}
			if a.Destructive {
				b.WriteString(` hint-buttonStyle="Critical"`)
			}
			b.WriteString(`/>`)
		}
		b.WriteString(`</actions>`)
	}
	if s.Silent {
		b.WriteString(`<audio silent="true"/>`)
	}
	b.WriteString(`</toast>`)
	return b.String()
}

// appActive reports whether one of the app's windows is in front (tests
// replace it).
var appActive = func(h *Host) bool { return h.keyWindow(ForegroundWindow()) != 0 }

// showToast shows a notification now: if the app is in front and Go
// presents (notify.OnPresent), it decides how first (a second at most).
func (h *Host) showToast(t *Toaster, s notifyspec.Spec, image string, reply func([]byte, error)) {
	n := h.notes()
	shown := shownOfSpec(s, time.Now().UnixMilli())
	show := func(how byte) {
		if how == 2 { // none
			reply(nil, nil)
			return
		}
		if s.Badge > 0 {
			h.setBadge(s.Badge)
		}
		reply(nil, t.Show(h.toastXML(s, shown, image), s.ID, 0, how == 1))
	}
	if !n.presenting || !appActive(h) {
		show(0)
		return
	}
	token := h.newToken()
	n.pendingPresent[token] = show
	b, _ := json.Marshal(notifyspec.Present{Token: token, Notification: shown})
	n.presents.deliver(b)
	var stop func()
	stop = ownTimer(1000, func() {
		stop()
		if d := n.pendingPresent[token]; d != nil {
			delete(n.pendingPresent, token)
			d(0)
		}
	})
}

// toastsShowing is the app's toasts in the notification center, newest first.
func (h *Host) toastsShowing(t *Toaster) ([]notifyspec.Shown, error) {
	ts, err := t.Showing()
	if err != nil {
		return nil, err
	}
	out := make([]notifyspec.Shown, 0, len(ts))
	for _, x := range ts {
		var a toastArgs
		if json.Unmarshal([]byte(x.Launch), &a) != nil || a.Asuka != 1 {
			a.Note = notifyspec.Shown{ID: x.Tag}
		}
		if a.Note.Data == nil {
			a.Note.Data = map[string]string{}
		}
		if a.Note.ID == "" {
			a.Note.ID = x.Tag
		}
		out = append(out, a.Note)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out, nil
}

// setBadge shows n on the app's taskbar button (0: none).
func (h *Host) setBadge(n int) error {
	s := h.notes()
	s.badge = n
	if Packaged() {
		return SetPackageBadge(n)
	}
	if s.badgeMsg == 0 {
		s.badgeMsg = TaskbarButtonCreated()
	}
	return SetTaskbarBadge(h.window, n, h.dpi, strconv.Itoa(n))
}

// badgeMessage sets the overlay again when the taskbar button's made
// again (the window shown, Explorer restarted).
func (h *Host) badgeMessage(m uint32) {
	if s := h.svc.notes; s != nil && s.badgeMsg != 0 && m == s.badgeMsg && s.badge > 0 && !Packaged() {
		SetTaskbarBadge(h.window, s.badge, h.dpi, strconv.Itoa(s.badge))
	}
}

// notifyModule is asuka/sdk/notify's module.
func (h *Host) notifyModule() map[string]asyncMethod {
	s := h.notes()
	ok := func(_ []byte, reply func([]byte, error)) { reply(nil, nil) }
	with := func(f func(t *Toaster, args []byte, reply func([]byte, error))) asyncMethod {
		return func(args []byte, reply func([]byte, error)) {
			t, err := h.toasts()
			if err != nil {
				reply(nil, err)
				return
			}
			f(t, args, reply)
		}
	}
	return map[string]asyncMethod{
		"request": with(func(t *Toaster, _ []byte, reply func([]byte, error)) {
			reply([]byte{b2u(t.Enabled())}, nil)
		}),
		"schedule": with(func(t *Toaster, args []byte, reply func([]byte, error)) {
			var sp notifyspec.Spec
			if err := json.Unmarshal(args, &sp); err != nil {
				reply(nil, err)
				return
			}
			if sp.ID == "" {
				sp.ID = time.Now().Format("20060102150405.000000000")
			}
			t.Cancel(sp.ID) // one of the same ID due is replaced
			add := func(image string) {
				if sp.Seconds <= 0 {
					h.showToast(t, sp, image, reply)
					return
				}
				at := time.Now().Add(time.Duration(max(1, sp.Seconds) * float64(time.Second))).UnixMilli()
				reply(nil, t.Show(h.toastXML(sp, shownOfSpec(sp, at), image), sp.ID, at, false))
			}
			if sp.Image == "" {
				add("")
				return
			}
			h.localFiles([]source{{Source: sp.Image}}, func(paths []string, err error) {
				if err != nil {
					add("")
					return
				}
				add(paths[0])
			})
		}),
		"cancel": with(func(t *Toaster, args []byte, reply func([]byte, error)) {
			t.Cancel(string(args))
			t.Remove(string(args))
			reply(nil, nil)
		}),
		"categories": func(args []byte, reply func([]byte, error)) {
			s.categories, s.categoriesRead = append([]byte(nil), args...), true
			p := h.categoriesFile()
			if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
				os.WriteFile(p, s.categories, 0o644)
			}
			reply(nil, nil)
		},
		// The badge (args: u32).
		"badge": func(args []byte, reply func([]byte, error)) {
			n := 0
			if len(args) >= 4 {
				n = int(binary.LittleEndian.Uint32(args))
			}
			reply(nil, h.setBadge(n))
		},
		"showing": with(func(t *Toaster, _ []byte, reply func([]byte, error)) {
			list, err := h.toastsShowing(t)
			if err != nil {
				reply(nil, err)
				return
			}
			b, err := json.Marshal(list)
			reply(b, err)
		}),
		"remove": with(func(t *Toaster, args []byte, reply func([]byte, error)) {
			var ids []string
			json.Unmarshal(args, &ids)
			for _, id := range ids {
				t.Remove(id)
			}
			reply(nil, nil)
		}),
		"removeThread": with(func(t *Toaster, args []byte, reply func([]byte, error)) {
			list, err := h.toastsShowing(t)
			if err != nil {
				reply(nil, err)
				return
			}
			for _, n := range list {
				if n.Thread == string(args) {
					t.Remove(n.ID)
				}
			}
			reply(nil, nil)
		}),
		"removeAll": with(func(t *Toaster, _ []byte, reply func([]byte, error)) {
			t.RemoveAll()
			reply(nil, nil)
		}),
		"present": func(args []byte, reply func([]byte, error)) {
			s.presenting = len(args) > 0 && args[0] == 1
			if !s.presenting {
				for t, d := range s.pendingPresent {
					delete(s.pendingPresent, t)
					d(0)
				}
			}
			reply(nil, nil)
		},
		"nextPresent": func(_ []byte, reply func([]byte, error)) { s.presents.next(reply) },
		// args: u8 how (0 banner, 1 list, 2 none), then the token.
		"presented": func(args []byte, reply func([]byte, error)) {
			if len(args) > 0 {
				if d := s.pendingPresent[string(args[1:])]; d != nil {
					delete(s.pendingPresent, string(args[1:]))
					d(args[0])
				}
			}
			reply(nil, nil)
		},
		"nextAction": func(_ []byte, reply func([]byte, error)) {
			h.startToasts()
			s.actions.next(reply)
		},
		"actionDone": func(args []byte, reply func([]byte, error)) {
			if d := s.pendingActions[string(args)]; d != nil {
				d()
			}
			reply(nil, nil)
		},
		"stop": func(args []byte, reply func([]byte, error)) {
			switch string(args) {
			case "nextPresent":
				s.presents.stop()
			case "nextAction":
				s.actions.stop()
			}
			reply(nil, nil)
		},
		// Android's: Windows has no channels.
		"channels":      ok,
		"channelGroups": ok,
	}
}
