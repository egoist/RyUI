//go:build darwin && !ios

package darwin

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/push on a Mac: APNs, as on iOS. "register" asks for notifications
// (the user notification center: the app's bundle), registers with APNs
// (NSApplication's registerForRemoteNotifications) and answers JSON
// (token, service, allowed); "nextReceived" and "nextOpened" answer with
// the next message (push.Message's JSON), kept until asked; "stop" (the
// method's name) fails the waiting ones with "stopped". The center's
// delegate (notify.go) hands it the pushes shown and tapped; the
// application delegate, the data messages. APNs needs the app signed
// with the aps-environment entitlement, which only a provisioning
// profile grants: without it, registering fails with APNs's error.
// push.Rewrite needs a notification service extension, which a Mac app
// from asuka build doesn't have.

var pushState = struct {
	registering []func([]byte, error)
	allowed     bool
	queues      map[string]*waitQueue
}{queues: map[string]*waitQueue{"nextReceived": {}, "nextOpened": {}}}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["push"] = h.pushModule() })
	targetMethods = append(targetMethods,
		Method("application:didRegisterForRemoteNotificationsWithDeviceToken:", func(self ID, _ SEL, app, token ID) {
			pushRegistered(Bytes(token), nil)
		}),
		Method("application:didFailToRegisterForRemoteNotificationsWithError:", func(self ID, _ SEL, app, err ID) {
			pushRegistered(nil, errorText(err, "APNs refused the registration"))
		}),
		Method("application:didReceiveRemoteNotification:", func(self ID, _ SEL, app, info ID) {
			pushDeliver("nextReceived", pushMessage(info, "", "", ""))
		}),
	)
}

func (h *Host) pushModule() map[string]method {
	q := pushState.queues
	return map[string]method{
		"register": func(_ []byte, reply func([]byte, error)) {
			if !bundled() {
				reply(nil, errors.New("asuka/push: APNs needs the app's bundle: build it with asuka build -platform mac"))
				return
			}
			h.installNotifications()
			if notes.center == 0 {
				reply(nil, errors.New("asuka/push: no user notification center"))
				return
			}
			var done objc.Block
			done = NewBlock(func(_ objc.Block, granted bool, err ID) {
				h.Post(func() {
					done.Release()
					pushState.allowed = granted
					pushState.registering = append(pushState.registering, reply)
					Send(Send(Class("NSApplication"), "sharedApplication"), "registerForRemoteNotifications")
				})
			})
			const alert, sound, badge = 1 << 2, 1 << 1, 1 << 0
			Send(notes.center, "requestAuthorizationWithOptions:completionHandler:", alert|sound|badge, uintptr(done))
		},
		"nextReceived": func(_ []byte, reply func([]byte, error)) { q["nextReceived"].next(reply) },
		"nextOpened":   func(_ []byte, reply func([]byte, error)) { q["nextOpened"].next(reply) },
		"stop": func(args []byte, reply func([]byte, error)) {
			if w := q[string(args)]; w != nil {
				w.stop()
			}
			reply(nil, nil)
		},
		"nextRewrite": func(_ []byte, reply func([]byte, error)) {
			reply(nil, errors.New("asuka/push: rewriting needs a notification service extension, which a Mac app doesn't have"))
		},
		"rewritten": answerNil,
	}
}

// pushRegistered is APNs's answer to registering: the device token, or
// why not.
func pushRegistered(token []byte, err error) {
	var out []byte
	if err == nil {
		out, _ = json.Marshal(map[string]any{"token": hex.EncodeToString(token), "service": "apns", "allowed": pushState.allowed})
	}
	rs := pushState.registering
	pushState.registering = nil
	for _, r := range rs {
		r(out, err)
	}
}

func pushDeliver(kind string, m []byte) {
	if q := pushState.queues[kind]; q != nil {
		q.deliver(m)
	}
}

// isPush reports whether a notification came from APNs.
func isPush(n ID) bool {
	t := Send(Send(n, "request"), "trigger")
	return t != 0 && HasClass("UNPushNotificationTrigger") &&
		SendBool(t, "isKindOfClass:", uintptr(Class("UNPushNotificationTrigger")))
}

// pushShown is a push shown as the app runs (from the center's delegate).
func pushShown(n ID) {
	if !isPush(n) {
		return
	}
	req := Send(n, "request")
	c := Send(req, "content")
	pushDeliver("nextReceived", pushMessage(Send(c, "userInfo"), GoString(Send(req, "identifier")), GoString(Send(c, "title")), GoString(Send(c, "body"))))
}

// pushOpened is a push tapped (or its action chosen), not dismissed.
func pushOpened(r ID) {
	n := Send(r, "notification")
	if !isPush(n) || GoString(Send(r, "actionIdentifier")) == "com.apple.UNNotificationDismissActionIdentifier" {
		return
	}
	req := Send(n, "request")
	c := Send(req, "content")
	pushDeliver("nextOpened", pushMessage(Send(c, "userInfo"), GoString(Send(req, "identifier")), GoString(Send(c, "title")), GoString(Send(c, "body"))))
}

// jsonObject is a property list object (a push's payload) as Go values.
func jsonObject(o ID) map[string]any {
	if o == 0 || !SendBool(Class("NSJSONSerialization"), "isValidJSONObject:", uintptr(o)) {
		return nil
	}
	d := Send(Class("NSJSONSerialization"), "dataWithJSONObject:options:error:", uintptr(o), 0, 0)
	var m map[string]any
	json.Unmarshal(Bytes(d), &m)
	return m
}

// pushData is a push's data: its payload's keys beside aps, as strings.
func pushData(payload map[string]any) map[string]string {
	data := map[string]string{}
	for k, v := range payload {
		if k == "aps" {
			continue
		}
		switch x := v.(type) {
		case string:
			data[k] = x
		case float64:
			data[k] = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			data[k] = strconv.FormatBool(x)
		default:
			b, _ := json.Marshal(x)
			data[k] = string(b)
		}
	}
	return data
}

// pushMessage is a push as push.Message's JSON: its id, title, body,
// data, and what its aps says (subtitle, category, thread-id, badge).
func pushMessage(userInfo ID, id, title, body string) []byte {
	return pushMessageOf(jsonObject(userInfo), id, title, body)
}

func pushMessageOf(payload map[string]any, id, title, body string) []byte {
	aps, _ := payload["aps"].(map[string]any)
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	sub := ""
	switch a := aps["alert"].(type) {
	case string:
		if body == "" {
			body = a
		}
	case map[string]any:
		if title == "" && body == "" {
			title, body = str(a, "title"), str(a, "body")
		}
		sub = str(a, "subtitle")
	}
	badge, _ := aps["badge"].(float64)
	b, _ := json.Marshal(map[string]any{
		"id": id, "title": title, "body": body, "subtitle": sub, "data": pushData(payload),
		"category": str(aps, "category"), "thread": str(aps, "thread-id"), "badge": int(badge),
	})
	return b
}
