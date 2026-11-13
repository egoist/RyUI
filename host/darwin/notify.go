//go:build darwin && !ios

package darwin

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
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/notifyspec"
)

// asuka/sdk/notify on macOS: UNUserNotificationCenter, as iOS's host has it
// (platform/ios's Notify.swift), the module's methods the same. What macOS
// lacks of it: Android's channels (ignored, as on iOS) and communication
// notifications (a message shows as a notification, its sender's name its
// title). Pushes (APNs) shown or tapped go to asuka/sdk/push too (push.go),
// their data the payload's keys beside aps. The center needs the
// app's bundle (asuka build --platform mac): an executable run on its own (go run,
// asuka dev) has no bundle ID, and gets an error.

// Presentation options (UNNotificationPresentationOptions).
const (
	unBadge  = 1 << 0
	unSound  = 1 << 1
	unList   = 1 << 3
	unBanner = 1 << 4
)

// waitQueue is what Go waits for ("next..." methods), as the iOS host's
// AsukaWaitQueue. Main thread.
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

// notifier is the center's delegate's state. Main thread.
type notifier struct {
	presenting       bool
	presents         waitQueue
	actions          waitQueue
	pendingPresent   map[string]func(uint)
	pendingActions   map[string]func()
	categories       []byte
	tokens           int
	installedOnce    sync.Once
	installed        bool
	delegate, center ID
}

var notes = &notifier{pendingPresent: map[string]func(uint){}, pendingActions: map[string]func(){}}

// actionSeconds: what a background action has, as on iOS.
const actionSeconds = 25

const categoriesKey = "asuka.notify.categories"

// bundled reports whether the process is an app bundle, which the
// center needs.
func bundled() bool {
	return Send(Send(Class("NSBundle"), "mainBundle"), "bundleIdentifier") != 0
}

var errNoBundle = errors.New("asuka/sdk/notify: macOS's notifications need the app's bundle: build it with asuka build --platform mac")

// installNotifications makes the center's delegate, if the app is a
// bundle: before it finishes launching, to see a response that launched
// it.
func (h *Host) installNotifications() {
	notes.installedOnce.Do(func() {
		if !bundled() || Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications") == 0 || !HasClass("UNUserNotificationCenter") {
			return
		}
		cls := DefineClass("AsukaNotifyDelegate", "NSObject", []string{"UNUserNotificationCenterDelegate"}, []objc.MethodDef{
			Method("userNotificationCenter:willPresentNotification:withCompletionHandler:", func(self ID, _ SEL, center, n, handler ID) {
				done := Retain(Send(handler, "copy"))
				Retain(n)
				h.Post(func() {
					defer Release(n)
					h.willPresent(n, func(opts uint) { CallBlock(uintptr(done), uintptr(opts)); Release(done) })
				})
			}),
			Method("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:", func(self ID, _ SEL, center, r, handler ID) {
				done := Retain(Send(handler, "copy"))
				Retain(r)
				h.Post(func() {
					defer Release(r)
					h.didReceive(r, func() { CallBlock(uintptr(done)); Release(done) })
				})
			}),
		})
		notes.center = Send(Class("UNUserNotificationCenter"), "currentNotificationCenter")
		notes.delegate = Retain(Send(cls, "new"))
		Send(notes.center, "setDelegate:", uintptr(notes.delegate))
		if d := Send(Send(Class("NSUserDefaults"), "standardUserDefaults"), "dataForKey:", uintptr(String(categoriesKey))); d != 0 {
			notes.categories = Bytes(d)
			Send(notes.center, "setNotificationCategories:", uintptr(categorySet(notes.categories)))
		}
		notes.installed = true
	})
}

// presentOptions are a presentation's (notify.Presentation) options.
func presentOptions(how byte) uint {
	switch how {
	case 1:
		return unList | unBadge
	case 2:
		return 0
	}
	return unBanner | unList | unSound | unBadge
}

func (h *Host) willPresent(n ID, done func(uint)) {
	pushShown(n)
	if !notes.presenting {
		done(presentOptions(0))
		return
	}
	notes.tokens++
	token := time.Now().Format("150405.000000000") + "-" + itoa(notes.tokens)
	notes.pendingPresent[token] = done
	b, _ := json.Marshal(notifyspec.Present{Token: token, Notification: shownOf(n)})
	notes.presents.deliver(b)
	// A second at most: then it shows.
	time.AfterFunc(time.Second, func() {
		h.Post(func() {
			if d := notes.pendingPresent[token]; d != nil {
				delete(notes.pendingPresent, token)
				d(presentOptions(0))
			}
		})
	})
}

func (h *Host) didReceive(r ID, done func()) {
	pushOpened(r)
	n := Send(r, "notification")
	content := Send(Send(n, "request"), "content")
	action := GoString(Send(r, "actionIdentifier"))
	if action == "com.apple.UNNotificationDefaultActionIdentifier" {
		action = ""
	}
	reply := ""
	if RespondsTo(r, "userText") {
		reply = GoString(Send(r, "userText"))
	}
	notes.tokens++
	token := time.Now().Format("150405.000000000") + "-" + itoa(notes.tokens)
	b, _ := json.Marshal(notifyspec.Response{Token: token, Action: action, Reply: reply, Seconds: actionSeconds, Notification: shownOf(n)})
	notes.actions.deliver(b)
	if action == "" {
		if u := GoString(Send(Send(content, "userInfo"), "objectForKey:", uintptr(String("url")))); u != "" && h.core != nil {
			h.openURL(u)
		}
	}
	if action == "" || opensApp(GoString(Send(content, "categoryIdentifier")), action) {
		done()
		return
	}
	// A background action: the app keeps running until Go is done.
	notes.pendingActions[token] = done
	time.AfterFunc(actionSeconds*time.Second, func() {
		h.Post(func() {
			if d := notes.pendingActions[token]; d != nil {
				delete(notes.pendingActions, token)
				d()
			}
		})
	})
}

// opensApp reports whether category's action opens the app.
func opensApp(category, action string) bool {
	var cs []struct {
		ID      string `json:"id"`
		Actions []struct {
			ID   string `json:"id"`
			Open bool   `json:"open"`
		} `json:"actions"`
	}
	json.Unmarshal(notes.categories, &cs)
	for _, c := range cs {
		if c.ID != category {
			continue
		}
		for _, a := range c.Actions {
			if a.ID == action {
				return a.Open
			}
		}
	}
	return false
}

// shownOf is what's reported of a notification (a notifyspec.Shown).
func shownOf(n ID) notifyspec.Shown {
	req := Send(n, "request")
	c := Send(req, "content")
	info := Send(c, "userInfo")
	data := map[string]string{}
	remote := isPush(n)
	if remote {
		data = pushData(jsonObject(info))
	} else if d := Send(info, "objectForKey:", uintptr(String("asuka.data"))); d != 0 {
		for _, k := range ArrayItems(Send(d, "allKeys")) {
			data[GoString(k)] = GoString(Send(d, "objectForKey:", uintptr(k)))
		}
	}
	secs := MsgFloat(Send(n, "date"), Sel("timeIntervalSince1970"))
	return notifyspec.Shown{
		ID: GoString(Send(req, "identifier")), Title: GoString(Send(c, "title")), Subtitle: GoString(Send(c, "subtitle")),
		Body: GoString(Send(c, "body")), URL: GoString(Send(info, "objectForKey:", uintptr(String("url")))), Data: data,
		Category: GoString(Send(c, "categoryIdentifier")), Thread: GoString(Send(c, "threadIdentifier")),
		Time: int64(secs * 1000), Remote: remote,
	}
}

// categorySet is the categories' JSON (notify.SetCategories) as an
// NSSet of UNNotificationCategory.
func categorySet(b []byte) ID {
	var cs []struct {
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
	json.Unmarshal(b, &cs)
	set := Send(Class("NSMutableSet"), "set")
	icons := HasClass("UNNotificationActionIcon")
	for _, c := range cs {
		actions := Send(Class("NSMutableArray"), "array")
		for _, a := range c.Actions {
			var opts uint
			if a.Unlock {
				opts |= 1 // UNNotificationActionOptionAuthenticationRequired
			}
			if a.Destructive {
				opts |= 2
			}
			if a.Open {
				opts |= 4
			}
			var icon ID
			if icons && a.Icon != "" {
				icon = Send(Class("UNNotificationActionIcon"), "iconWithSystemImageName:", uintptr(String(a.Icon)))
			}
			var act ID
			switch {
			case a.Reply != nil && icons:
				act = Send(Class("UNTextInputNotificationAction"), "actionWithIdentifier:title:options:icon:textInputButtonTitle:textInputPlaceholder:",
					uintptr(String(a.ID)), uintptr(String(a.Title)), uintptr(opts), uintptr(icon), uintptr(String(a.Reply.Button)), uintptr(String(a.Reply.Placeholder)))
			case a.Reply != nil:
				act = Send(Class("UNTextInputNotificationAction"), "actionWithIdentifier:title:options:textInputButtonTitle:textInputPlaceholder:",
					uintptr(String(a.ID)), uintptr(String(a.Title)), uintptr(opts), uintptr(String(a.Reply.Button)), uintptr(String(a.Reply.Placeholder)))
			case icons:
				act = Send(Class("UNNotificationAction"), "actionWithIdentifier:title:options:icon:", uintptr(String(a.ID)), uintptr(String(a.Title)), uintptr(opts), uintptr(icon))
			default:
				act = Send(Class("UNNotificationAction"), "actionWithIdentifier:title:options:", uintptr(String(a.ID)), uintptr(String(a.Title)), uintptr(opts))
			}
			Send(actions, "addObject:", uintptr(act))
		}
		cat := Send(Class("UNNotificationCategory"), "categoryWithIdentifier:actions:intentIdentifiers:options:",
			uintptr(String(c.ID)), uintptr(actions), uintptr(Array()), 0)
		Send(set, "addObject:", uintptr(cat))
	}
	return set
}

// imageFile is the local copy of an image source (as ui.Image's: an
// http(s) URL, a file, an asset), which the system moves into its store,
// and its extension. Off the main thread.
func imageFile(src string) (string, error) {
	dir := filepath.Join(os.TempDir(), "asuka-notify")
	os.MkdirAll(dir, 0o755)
	var r io.Reader
	ext := filepath.Ext(src)
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		c := &http.Client{Timeout: 15 * time.Second}
		resp, err := c.Get(src)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", errors.New(resp.Status)
		}
		if exts, _ := mime.ExtensionsByType(resp.Header.Get("Content-Type")); len(exts) > 0 {
			ext = exts[0]
		}
		r = resp.Body
	default:
		path := strings.TrimPrefix(src, "file://")
		if !filepath.IsAbs(path) {
			path = filepath.Join(assetDir(), path)
		}
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		r = f
	}
	out, err := os.CreateTemp(dir, "*"+ext)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return out.Name(), err
}

// assetDir is where the app's assets are: in its bundle's Resources, or
// beside the executable run on its own.
func assetDir() string {
	if p := GoString(Send(Send(Class("NSBundle"), "mainBundle"), "resourcePath")); p != "" {
		if _, err := os.Stat(filepath.Join(p, "assets")); err == nil {
			return filepath.Join(p, "assets")
		}
	}
	return "assets"
}

// content is a spec's notification content (autoreleased); image is its
// attachment's file, or "".
func content(s notifyspec.Spec, image string) ID {
	c := Send(Class("UNMutableNotificationContent"), "new")
	Autorelease(c)
	title := s.Title
	if title == "" && s.Sender != nil {
		title = s.Sender.Name
	}
	Send(c, "setTitle:", uintptr(String(title)))
	Send(c, "setSubtitle:", uintptr(String(s.Subtitle)))
	Send(c, "setBody:", uintptr(String(s.Body)))
	if !s.Silent {
		Send(c, "setSound:", uintptr(Send(Class("UNNotificationSound"), "defaultSound")))
	}
	var kv []ID
	for k, v := range s.Data {
		kv = append(kv, String(k), String(v))
	}
	info := []ID{String("asuka.data"), Dict(kv...)}
	if s.URL != "" {
		info = append(info, String("url"), String(s.URL))
	}
	Send(c, "setUserInfo:", uintptr(Dict(info...)))
	Send(c, "setCategoryIdentifier:", uintptr(String(s.Category)))
	Send(c, "setThreadIdentifier:", uintptr(String(s.Thread)))
	if s.Badge > 0 {
		Send(c, "setBadge:", uintptr(NumberInt(s.Badge)))
	}
	if image != "" {
		a := Send(Class("UNNotificationAttachment"), "attachmentWithIdentifier:URL:options:error:",
			uintptr(String("image")), uintptr(FileURL(image)), 0, 0)
		if a != 0 {
			Send(c, "setAttachments:", uintptr(Array(a)))
		}
	}
	return c
}

// notifyModule is asuka/sdk/notify's module.
func (h *Host) notifyModule() map[string]method {
	center := func(reply func([]byte, error)) bool {
		h.installNotifications()
		if !notes.installed {
			reply(nil, errNoBundle)
			return false
		}
		return true
	}
	ok := func(_ []byte, reply func([]byte, error)) { reply(nil, nil) }
	errBlock := func(reply func([]byte, error)) objc.Block {
		var b objc.Block
		b = NewBlock(func(_ objc.Block, err ID) {
			msg := ""
			if err != 0 {
				msg = GoString(Send(err, "localizedDescription"))
			}
			h.Post(func() {
				b.Release()
				if msg != "" {
					reply(nil, errors.New(msg))
				} else {
					reply(nil, nil)
				}
			})
		})
		return b
	}
	return map[string]method{
		"request": func(_ []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			var b objc.Block
			b = NewBlock(func(_ objc.Block, granted uintptr, err ID) {
				msg := ""
				if err != 0 {
					msg = GoString(Send(err, "localizedDescription"))
				}
				h.Post(func() {
					b.Release()
					if msg != "" {
						reply(nil, errors.New(msg))
						return
					}
					reply([]byte{byte(granted & 1)}, nil)
				})
			})
			Send(notes.center, "requestAuthorizationWithOptions:completionHandler:", unBadge|unSound|4, uintptr(b))
		},
		"schedule": func(args []byte, reply func([]byte, error)) {
			if !center(reply) {
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
			add := func(image string) {
				WithPool(func() {
					var trigger ID
					if s.Seconds > 0 {
						trigger = objc.Send[ID](Class("UNTimeIntervalNotificationTrigger"), Sel("triggerWithTimeInterval:repeats:"), max(1, s.Seconds), false)
					}
					req := Send(Class("UNNotificationRequest"), "requestWithIdentifier:content:trigger:", uintptr(String(s.ID)), uintptr(content(s, image)), uintptr(trigger))
					Send(notes.center, "addNotificationRequest:withCompletionHandler:", uintptr(req), uintptr(errBlock(reply)))
				})
			}
			if s.Image == "" {
				add("")
				return
			}
			go func() {
				f, _ := imageFile(s.Image)
				h.Post(func() { add(f) })
			}()
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			ids := Array(String(string(args)))
			Send(notes.center, "removePendingNotificationRequestsWithIdentifiers:", uintptr(ids))
			Send(notes.center, "removeDeliveredNotificationsWithIdentifiers:", uintptr(ids))
			reply(nil, nil)
		},
		"categories": func(args []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			notes.categories = append([]byte(nil), args...)
			Send(Send(Class("NSUserDefaults"), "standardUserDefaults"), "setObject:forKey:", uintptr(Data(args)), uintptr(String(categoriesKey)))
			Send(notes.center, "setNotificationCategories:", uintptr(categorySet(args)))
			reply(nil, nil)
		},
		// The Dock's badge: the app's icon there.
		"badge": func(args []byte, reply func([]byte, error)) {
			n := 0
			if len(args) >= 4 {
				n = int(binary.LittleEndian.Uint32(args))
			}
			label := ""
			if n > 0 {
				label = itoa(n)
			}
			tile := Send(Send(Class("NSApplication"), "sharedApplication"), "dockTile")
			Send(tile, "setBadgeLabel:", uintptr(String(label)))
			reply(nil, nil)
		},
		"showing": func(_ []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			h.delivered(func(ns []ID) {
				list := make([]notifyspec.Shown, 0, len(ns))
				for _, n := range ns {
					list = append(list, shownOf(n))
				}
				sort.SliceStable(list, func(i, j int) bool { return list[i].Time > list[j].Time })
				b, err := json.Marshal(list)
				reply(b, err)
			})
		},
		"remove": func(args []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			var ids []string
			json.Unmarshal(args, &ids)
			arr := make([]ID, len(ids))
			for i, id := range ids {
				arr[i] = String(id)
			}
			Send(notes.center, "removeDeliveredNotificationsWithIdentifiers:", uintptr(Array(arr...)))
			reply(nil, nil)
		},
		"removeThread": func(args []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			thread := string(args)
			h.delivered(func(ns []ID) {
				var ids []ID
				for _, n := range ns {
					if c := Send(Send(n, "request"), "content"); GoString(Send(c, "threadIdentifier")) == thread {
						ids = append(ids, Send(Send(n, "request"), "identifier"))
					}
				}
				Send(notes.center, "removeDeliveredNotificationsWithIdentifiers:", uintptr(Array(ids...)))
				reply(nil, nil)
			})
		},
		"removeAll": func(_ []byte, reply func([]byte, error)) {
			if !center(reply) {
				return
			}
			Send(notes.center, "removeAllDeliveredNotifications")
			reply(nil, nil)
		},
		"present": func(args []byte, reply func([]byte, error)) {
			notes.presenting = len(args) > 0 && args[0] == 1
			if !notes.presenting {
				for t, d := range notes.pendingPresent {
					delete(notes.pendingPresent, t)
					d(presentOptions(0))
				}
			}
			reply(nil, nil)
		},
		"nextPresent": func(_ []byte, reply func([]byte, error)) { notes.presents.next(reply) },
		"presented": func(args []byte, reply func([]byte, error)) {
			if len(args) > 0 {
				if d := notes.pendingPresent[string(args[1:])]; d != nil {
					delete(notes.pendingPresent, string(args[1:]))
					d(presentOptions(args[0]))
				}
			}
			reply(nil, nil)
		},
		"nextAction": func(_ []byte, reply func([]byte, error)) {
			h.installNotifications()
			notes.actions.next(reply)
		},
		"actionDone": func(args []byte, reply func([]byte, error)) {
			if d := notes.pendingActions[string(args)]; d != nil {
				delete(notes.pendingActions, string(args))
				d()
			}
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
		// Android's: macOS has no channels.
		"channels":      ok,
		"channelGroups": ok,
	}
}

// delivered gets the app's notifications showing, on the main thread.
func (h *Host) delivered(f func([]ID)) {
	var b objc.Block
	b = NewBlock(func(_ objc.Block, arr ID) {
		Retain(arr)
		h.Post(func() {
			b.Release()
			defer Release(arr)
			f(ArrayItems(arr))
		})
	})
	Send(notes.center, "getDeliveredNotificationsWithCompletionHandler:", uintptr(b))
}
