//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/notifyspec"
)

// asuka/notify on Linux: GNotification through the app's GApplication
// (GNOME Shell's notifications, the portal's in a sandbox, the
// freedesktop server elsewhere), the module's methods as the other
// hosts have them. A notification's tap and its buttons are actions of
// the application ("app.asuka-notify", the notification's ID and the
// action's as its target), which reach the app while it runs.
//
// A notification due later is a systemd user timer's, which shows it
// whether the app runs or not (notify_timer.go), else a timer of the
// app's, kept while it runs. What Linux lacks: a list of what's showing (the host keeps what it
// showed and wasn't withdrawn or tapped), inline replies (a reply action
// is a button that opens the app), Android's channels (ignored, as on
// iOS) and pushes (asuka/push isn't on Linux). The badge is the launcher
// API's (com.canonical.Unity.LauncherEntry: Dash to Dock, Plasma's task
// manager); GNOME's own dash has none.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["notify"] = h.notifyModule()
	})
}

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

// shownNote is a notification the host showed: what's reported of it.
type shownNote struct {
	shown notifyspec.Shown
}

// notifier is the module's state. Main thread.
type notifier struct {
	installed      bool
	presenting     bool
	presents       waitQueue
	actions        waitQueue
	pendingPresent map[string]func(byte)
	pendingActions map[string]bool
	categories     []byte
	tokens         int
	timers         map[string]uint32     // due later, by ID
	showing        map[string]*shownNote // shown, by ID
	badgePath      string
	actionsAdded   bool // the application's actions (installNotifyActions)

	// A run with ASUKA_NOTIFY's (notify_timer.go): its notification tapped,
	// closed.
	headlessTap    func(notifyTarget)
	headlessClosed func(id string)

	// The freedesktop server's, used directly when GLib's GNotification
	// can't be (an app without a desktop file, which GNOME Shell turns
	// away): its IDs, by ours, and ours by its.
	fdo     bool
	fdoSubs bool // its signals listened to
	fdoIDs  map[string]uint32
	fdoBack map[uint32]string
}

var notes = &notifier{
	pendingPresent: map[string]func(byte){},
	pendingActions: map[string]bool{},
	timers:         map[string]uint32{},
	showing:        map[string]*shownNote{},
	fdoIDs:         map[string]uint32{},
	fdoBack:        map[uint32]string{},
}

// notifyBackend picks how notifications are sent: "" GNotification
// when the app has a desktop file (or runs in a sandbox), else the
// freedesktop server; "gio" or "fdo" force one (tests).
var notifyBackend = ""

// hasDesktopFile reports whether the system has the app's desktop file.
func hasDesktopFile(appID string) bool {
	f := Sym("g_desktop_app_info_new")
	if f == 0 {
		return false
	}
	name := CString(appID + ".desktop")
	info := Call(f, Addr(name))
	if info == 0 {
		return false
	}
	ObjectUnref(info)
	return true
}

// actionSeconds: what a background action has, as on iOS.
const actionSeconds = 25

// notifyAction is the application's action notifications activate.
const notifyAction = "asuka-notify"

// notifyTarget is an activation's target: the notification and the
// action (empty: the notification itself).
type notifyTarget struct {
	ID     string `json:"id"`
	Action string `json:"action,omitempty"`
	// Shown is the notification, from a process that showed it while
	// the app didn't run (it isn't the app's showing).
	Shown *notifyspec.Shown `json:"shown,omitempty"`
}

// appActive reports whether one of the app's windows has the focus
// (tests replace it).
var appActive = func(h *Host) bool {
	for _, w := range h.wins {
		if WindowIsActive(w.window) {
			return true
		}
	}
	return h.main != nil && WindowIsActive(h.main.window)
}

// installNotifications adds the application's action, once.
func (h *Host) installNotifications() error {
	if notes.installed {
		return nil
	}
	if h.app == 0 || !Sys.ApplicationGetIsRegistered(h.app) {
		return errors.New("asuka/notify: the application isn't registered (no session bus?)")
	}
	h.installNotifyActions()
	switch notifyBackend {
	case "gio":
		notes.fdo = false
	case "fdo":
		notes.fdo = true
	default:
		notes.fdo = os.Getenv("FLATPAK_ID") == "" && !hasDesktopFile(h.appID)
	}
	if notes.fdo {
		if err := h.installFdo(); err != nil {
			return err
		}
	}
	notes.installed = true
	return nil
}

const (
	fdoDest  = "org.freedesktop.Notifications"
	fdoPath  = "/org/freedesktop/Notifications"
	fdoIface = "org.freedesktop.Notifications"
)

// installFdo listens to the freedesktop server's signals: a tap or a
// button (ActionInvoked), a notification gone (NotificationClosed).
func (h *Host) installFdo() error {
	if notes.fdoSubs {
		return nil
	}
	conn, err := Bus(BusSession)
	if err != nil {
		return err
	}
	notes.fdoSubs = true
	DBusSubscribe(conn, fdoDest, fdoIface, "ActionInvoked", fdoPath, func(sig DBusSignal) {
		a, _ := VariantGo(sig.Params).([]any)
		if len(a) < 2 {
			return
		}
		n, _ := a[0].(uint32)
		key, _ := a[1].(string)
		id, ok := notes.fdoBack[n]
		if !ok || theHost == nil {
			return
		}
		if key == "default" {
			key = ""
		}
		theHost.notificationActivated(notifyTarget{ID: id, Action: key})
	})
	DBusSubscribe(conn, fdoDest, fdoIface, "NotificationClosed", fdoPath, func(sig DBusSignal) {
		a, _ := VariantGo(sig.Params).([]any)
		if len(a) < 1 {
			return
		}
		n, _ := a[0].(uint32)
		if id, ok := notes.fdoBack[n]; ok {
			delete(notes.fdoBack, n)
			delete(notes.fdoIDs, id)
			delete(notes.showing, id)
			if notes.headlessClosed != nil {
				notes.headlessClosed(id)
			}
		}
	})
	return nil
}

// fdoMarkup escapes text for a server's body markup.
func fdoMarkup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// fdoNotify shows a notification through the freedesktop server
// (replacing one of the same ID); done gets whether it showed.
func (h *Host) fdoNotify(s notifyspec.Spec, image string, quiet bool, done func(bool)) {
	conn, err := Bus(BusSession)
	if err != nil {
		done(false)
		return
	}
	title := s.Title
	if title == "" && s.Sender != nil {
		title = s.Sender.Name
	}
	body := s.Body
	if s.Subtitle != "" {
		body = strings.TrimSpace(s.Subtitle + "\n" + s.Body)
	}
	actions := []string{"default", ""}
	if c := categoryNamed(s.Category); c != nil {
		for _, a := range c.Actions {
			label := a.Title
			if label == "" && a.Reply != nil {
				label = a.Reply.Button
			}
			actions = append(actions, a.ID, label)
		}
	}
	hints := map[string]Ptr{"desktop-entry": VStr(h.appID)}
	if image != "" {
		hints["image-path"] = VStr(image)
	}
	if quiet {
		hints["urgency"] = Sys.VariantNewByte(0)
	}
	if s.Silent {
		hints["suppress-sound"] = VBool(true)
	}
	if s.Sender != nil {
		hints["category"] = VStr("im.received")
	}
	replaces := notes.fdoIDs[s.ID]
	DBusCall(conn, fdoDest, fdoPath, fdoIface, "Notify", VTuple(
		VStr(h.appName), VU32(replaces), VStr(""), VStr(title), VStr(fdoMarkup(body)),
		VStrings(actions), VDict(hints), VI32(-1),
	), -1, func(r Ptr, err error) {
		if err != nil {
			done(false)
			return
		}
		a, _ := VariantGo(r).([]any)
		if len(a) == 0 {
			done(false)
			return
		}
		n, _ := a[0].(uint32)
		if old, ok := notes.fdoIDs[s.ID]; ok && old != n {
			delete(notes.fdoBack, old)
		}
		notes.fdoIDs[s.ID] = n
		notes.fdoBack[n] = s.ID
		done(true)
	})
}

// sysWithdraw takes a shown notification away from the system's list.
func (h *Host) sysWithdraw(id string) {
	if !notes.fdo {
		Sys.ApplicationWithdrawNotif(h.app, id)
		return
	}
	n, ok := notes.fdoIDs[id]
	if !ok {
		return
	}
	delete(notes.fdoIDs, id)
	delete(notes.fdoBack, n)
	if conn, err := Bus(BusSession); err == nil {
		DBusCall(conn, fdoDest, fdoPath, fdoIface, "CloseNotification", VTuple(VU32(n)), -1, func(Ptr, error) {})
	}
}

func (h *Host) newToken() string {
	notes.tokens++
	return time.Now().Format("150405.000000000") + "-" + strconv.Itoa(notes.tokens)
}

// notificationActivated is a tap on a notification, or on one of its
// buttons.
func (h *Host) notificationActivated(t notifyTarget) {
	if notes.headlessTap != nil {
		notes.headlessTap(t)
		return
	}
	shown := notifyspec.Shown{ID: t.ID, Data: map[string]string{}}
	if t.Shown != nil {
		shown = *t.Shown
		if shown.Data == nil {
			shown.Data = map[string]string{}
		}
	}
	if s := notes.showing[t.ID]; s != nil {
		shown = s.shown
		delete(notes.showing, t.ID) // the server takes a tapped one away
	}
	token := h.newToken()
	b, _ := json.Marshal(notifyspec.Response{Token: token, Action: t.Action, Seconds: actionSeconds, Notification: shown})
	notes.actions.deliver(b)
	if t.Action == "" || opensApp(shown.Category, t.Action) {
		if h.main != nil {
			WindowPresent(h.main.window)
		}
		if t.Action == "" && shown.URL != "" {
			h.incomingURL(shown.URL) // ui.OnOpenURL's, as on the other platforms
		}
		return
	}
	// A background action: Go says when it's done (the app runs anyway).
	notes.pendingActions[token] = true
	TimeoutAdd(actionSeconds*1000, func() bool {
		delete(notes.pendingActions, token)
		return false
	})
}

// notifyCategory is a category's JSON (notify.SetCategories).
type notifyCategory struct {
	ID      string `json:"id"`
	Actions []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Icon        string `json:"icon"`
		Destructive bool   `json:"destructive"`
		Unlock      bool   `json:"unlock"`
		Open        bool   `json:"open"`
		Reply       *struct {
			Button      string `json:"button"`
			Placeholder string `json:"placeholder"`
		} `json:"reply"`
	} `json:"actions"`
}

func categoryNamed(id string) *notifyCategory {
	var cs []notifyCategory
	json.Unmarshal(notes.categories, &cs)
	for i := range cs {
		if cs[i].ID == id {
			return &cs[i]
		}
	}
	return nil
}

// opensApp reports whether category's action opens the app (a reply
// does: Linux has no inline reply).
func opensApp(category, action string) bool {
	c := categoryNamed(category)
	if c == nil {
		return false
	}
	for _, a := range c.Actions {
		if a.ID == action {
			return a.Open || a.Reply != nil
		}
	}
	return false
}

// shownOfSpec is what's reported of a notification shown from a spec.
func shownOfSpec(s notifyspec.Spec) notifyspec.Shown {
	data := s.Data
	if data == nil {
		data = map[string]string{}
	}
	title := s.Title
	if title == "" && s.Sender != nil {
		title = s.Sender.Name
	}
	return notifyspec.Shown{ID: s.ID, Title: title, Subtitle: s.Subtitle, Body: s.Body, URL: s.URL, Data: data,
		Category: s.Category, Thread: s.Thread, Time: time.Now().UnixMilli()}
}

// gNotification is a spec as a GNotification (image: its icon's file,
// or ""); quiet shows it without a banner.
func gNotification(s notifyspec.Spec, image string, quiet bool) Ptr {
	title := s.Title
	if title == "" && s.Sender != nil {
		title = s.Sender.Name
	}
	n := Sys.NotificationNew(title)
	body := s.Body
	if s.Subtitle != "" {
		body = strings.TrimSpace(s.Subtitle + "\n" + s.Body)
	}
	if body != "" {
		Sys.NotificationSetBody(n, body)
	}
	if image != "" {
		f := Sys.FileNewForPath(image)
		icon := Sys.FileIconNew(f)
		Sys.NotificationSetIcon(n, icon)
		ObjectUnref(icon)
		ObjectUnref(f)
	}
	if quiet {
		Sys.NotificationSetPriority(n, 1) // G_NOTIFICATION_PRIORITY_LOW: in the list, no banner
	}
	if s.Sender != nil && Sys.NotificationSetCategory != nil {
		Sys.NotificationSetCategory(n, "im.received")
	}
	target := func(action string) Ptr {
		b, _ := json.Marshal(notifyTarget{ID: s.ID, Action: action})
		return VStr(string(b))
	}
	Sys.NotificationSetDefaultTV(n, "app."+notifyAction, target(""))
	if c := categoryNamed(s.Category); c != nil {
		for _, a := range c.Actions {
			label := a.Title
			if a.Reply != nil && a.Reply.Button != "" && label == "" {
				label = a.Reply.Button
			}
			Sys.NotificationAddButtonTV(n, label, "app."+notifyAction, target(a.ID))
		}
	}
	return n
}

// deliver shows a notification now: if the app is in front and Go
// presents (notify.OnPresent), it decides how first (a second at most).
func (h *Host) deliver(s notifyspec.Spec, image string) {
	delete(notes.timers, s.ID)
	shown := shownOfSpec(s)
	show := func(how byte) {
		if how == 2 { // none
			return
		}
		if s.Badge > 0 {
			h.setBadge(s.Badge)
		}
		if notes.fdo {
			h.fdoNotify(s, image, how == 1, func(ok bool) {
				if ok {
					notes.showing[s.ID] = &shownNote{shown: shown}
				}
			})
			return
		}
		n := gNotification(s, image, how == 1)
		Sys.ApplicationSendNotification(h.app, s.ID, n)
		ObjectUnref(n)
		notes.showing[s.ID] = &shownNote{shown: shown}
	}
	if !notes.presenting || !appActive(h) {
		show(0)
		return
	}
	token := h.newToken()
	notes.pendingPresent[token] = show
	b, _ := json.Marshal(notifyspec.Present{Token: token, Notification: shown})
	notes.presents.deliver(b)
	TimeoutAdd(1000, func() bool {
		if d := notes.pendingPresent[token]; d != nil {
			delete(notes.pendingPresent, token)
			d(0)
		}
		return false
	})
}

// notifyLater shows a notification once its seconds pass, while the
// app runs.
func (h *Host) notifyLater(s notifyspec.Spec, image string) {
	ms := uint32(max(1, s.Seconds) * 1000)
	notes.timers[s.ID] = TimeoutAdd(ms, func() bool {
		h.deliver(s, image)
		return false
	})
}

// withdraw takes notifications away (and what's due of them).
func (h *Host) withdraw(ids ...string) {
	for _, id := range ids {
		if t := notes.timers[id]; t != 0 {
			SourceRemove(t)
			delete(notes.timers, id)
		}
		delete(notes.showing, id)
		h.sysWithdraw(id)
		h.cancelSystemTimer(id)
	}
}

// setBadge sets the launcher's count for the app (0: none).
func (h *Host) setBadge(n int) error {
	conn, err := Bus(BusSession)
	if err != nil {
		return err
	}
	if notes.badgePath == "" {
		notes.badgePath = "/" + strings.ReplaceAll(h.appID, ".", "/") + "/LauncherEntry"
	}
	return DBusEmit(conn, notes.badgePath, "com.canonical.Unity.LauncherEntry", "Update", VTuple(
		VStr("application://"+h.appID+".desktop"),
		VDict(map[string]Ptr{"count": VI64(int64(n)), "count-visible": VBool(n > 0)}),
	))
}

// notifyImageFile is the local copy of an image source (an http(s) URL,
// a file, an asset) for a notification's icon. Off the main thread.
func notifyImageFile(src, assets, cache string) (string, error) {
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		dir := filepath.Join(cache, "asuka-notify")
		os.MkdirAll(dir, 0o755)
		c := &http.Client{Timeout: 15 * time.Second}
		resp, err := c.Get(src)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", errors.New(resp.Status)
		}
		ext := filepath.Ext(src)
		if exts, _ := mime.ExtensionsByType(resp.Header.Get("Content-Type")); len(exts) > 0 {
			ext = exts[0]
		}
		out, err := os.CreateTemp(dir, "*"+ext)
		if err != nil {
			return "", err
		}
		defer out.Close()
		_, err = io.Copy(out, resp.Body)
		return out.Name(), err
	default:
		path := strings.TrimPrefix(src, "file://")
		if !filepath.IsAbs(path) {
			path = filepath.Join(assets, path)
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
}

// notifyModule is asuka/notify's module.
func (h *Host) notifyModule() map[string]method {
	ready := func(reply func([]byte, error)) bool {
		if err := h.installNotifications(); err != nil {
			reply(nil, err)
			return false
		}
		return true
	}
	ok := func(_ []byte, reply func([]byte, error)) { reply(nil, nil) }
	return map[string]method{
		// Linux doesn't ask: notifications are allowed unless the user
		// turned the app's off in Settings, which apps aren't told.
		"request": func(_ []byte, reply func([]byte, error)) {
			if !ready(reply) {
				return
			}
			reply([]byte{1}, nil)
		},
		"schedule": func(args []byte, reply func([]byte, error)) {
			if !ready(reply) {
				return
			}
			var s notifyspec.Spec
			if err := json.Unmarshal(args, &s); err != nil {
				reply(nil, err)
				return
			}
			if s.ID == "" {
				s.ID = time.Now().Format("20060102150405.000000000")
			}
			// One of the same ID due: replaced (one showing is, as this shows).
			if t := notes.timers[s.ID]; t != 0 {
				SourceRemove(t)
				delete(notes.timers, s.ID)
			}
			add := func(image string) {
				if s.Seconds > 0 && systemdTimers() {
					h.scheduleSystemTimer(s, image, func(err error) {
						if err != nil { // the app's timer, then
							h.notifyLater(s, image)
						}
						reply(nil, nil)
					})
					return
				}
				h.cancelSystemTimer(s.ID)
				if s.Seconds > 0 {
					h.notifyLater(s, image)
				} else {
					h.deliver(s, image)
				}
				reply(nil, nil)
			}
			if s.Image == "" {
				add("")
				return
			}
			assets, cache := h.assets, h.modCacheDir()
			go func() {
				f, _ := notifyImageFile(s.Image, assets, cache)
				h.Post(func() { add(f) })
			}()
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			if !ready(reply) {
				return
			}
			h.withdraw(string(args))
			reply(nil, nil)
		},
		"categories": func(args []byte, reply func([]byte, error)) {
			notes.categories = append([]byte(nil), args...)
			reply(nil, nil)
		},
		// The launcher's count (args: u32).
		"badge": func(args []byte, reply func([]byte, error)) {
			n := 0
			if len(args) >= 4 {
				n = int(binary.LittleEndian.Uint32(args))
			}
			reply(nil, h.setBadge(n))
		},
		"showing": func(_ []byte, reply func([]byte, error)) {
			list := make([]notifyspec.Shown, 0, len(notes.showing))
			for _, s := range notes.showing {
				list = append(list, s.shown)
			}
			sort.SliceStable(list, func(i, j int) bool {
				if list[i].Time != list[j].Time {
					return list[i].Time > list[j].Time
				}
				return list[i].ID < list[j].ID
			})
			b, err := json.Marshal(list)
			reply(b, err)
		},
		"remove": func(args []byte, reply func([]byte, error)) {
			var ids []string
			json.Unmarshal(args, &ids)
			for _, id := range ids {
				if notes.timers[id] == 0 && !systemDue(id) { // shown ones only
					delete(notes.showing, id)
					h.sysWithdraw(id)
				}
			}
			reply(nil, nil)
		},
		"removeThread": func(args []byte, reply func([]byte, error)) {
			thread := string(args)
			for id, s := range notes.showing {
				if s.shown.Thread == thread {
					delete(notes.showing, id)
					h.sysWithdraw(id)
				}
			}
			reply(nil, nil)
		},
		"removeAll": func(_ []byte, reply func([]byte, error)) {
			for id := range notes.showing {
				delete(notes.showing, id)
				h.sysWithdraw(id)
			}
			reply(nil, nil)
		},
		"present": func(args []byte, reply func([]byte, error)) {
			notes.presenting = len(args) > 0 && args[0] == 1
			if !notes.presenting {
				for t, d := range notes.pendingPresent {
					delete(notes.pendingPresent, t)
					d(0)
				}
			}
			reply(nil, nil)
		},
		"nextPresent": func(_ []byte, reply func([]byte, error)) { notes.presents.next(reply) },
		// args: u8 how (0 banner, 1 list, 2 none), then the token.
		"presented": func(args []byte, reply func([]byte, error)) {
			if len(args) > 0 {
				if d := notes.pendingPresent[string(args[1:])]; d != nil {
					delete(notes.pendingPresent, string(args[1:]))
					d(args[0])
				}
			}
			reply(nil, nil)
		},
		"nextAction": func(_ []byte, reply func([]byte, error)) {
			if !ready(reply) {
				return
			}
			notes.actions.next(reply)
		},
		"actionDone": func(args []byte, reply func([]byte, error)) {
			delete(notes.pendingActions, string(args))
			reply(nil, nil)
		},
		"stop": func(args []byte, reply func([]byte, error)) {
			switch string(args) {
			case "nextPresent":
				notes.presents.stop()
			case "nextAction":
				notes.actions.stop()
			}
			reply(nil, nil)
		},
		// Android's: Linux has no channels.
		"channels":      ok,
		"channelGroups": ok,
	}
}
