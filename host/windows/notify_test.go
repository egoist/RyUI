//go:build windows

package windows

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/ryui/internal/notifyspec"
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/notify"
	"github.com/egoist/ryui/ui"
)

// toastCategories are the tests' categories: a message's reply and
// buttons, one opening the app, one destructive.
var toastCategories = []byte(`[{"id":"msg","actions":[
	{"id":"reply","title":"Reply","reply":{"button":"Send","placeholder":"Message"}},
	{"id":"open","title":"Open","open":true},
	{"id":"del","title":"Delete","destructive":true}]}]`)

// A toast with a category's buttons and reply shows; a click on it, on
// a button, or a reply sent comes through the app's COM activator (as the
// shell calls it) as notify's Response: a tap opens the app and its URL,
// a background action waits for Go to be done. Showing lists the toasts
// in the notification center with what they carry; Remove takes one
// away.
func TestToastActions(t *testing.T) {
	var urls []string
	h, _ := mount(t, func() ui.Node {
		ui.OnOpenURL(func(u string) { urls = append(urls, u) })
		return ui.StaticText("x")
	})
	onMain(t, func() {
		await(h, "notify", "categories", toastCategories)
		spec, _ := json.Marshal(notifyspec.Spec{ID: "ryui-actions", Title: "Ann", Body: "Lunch?", Category: "msg",
			Thread: "chat-1", URL: "myapp://chat/1", Data: map[string]string{"k": "v"}, Silent: true})
		if _, err, _ := await(h, "notify", "schedule", spec); err != nil {
			fatalf("notify.schedule with actions: %v", err)
		}
		x := h.toastXML(notifyspec.Spec{Category: "msg", Body: "b"}, notifyspec.Shown{ID: "i", Title: "t"}, "")
		for _, want := range []string{`<input id="r:reply" type="text" placeHolderContent="Message"/>`, `content="Send"`,
			`hint-inputId="r:reply"`, `activationType="foreground"`, `activationType="background"`, `hint-buttonStyle="Critical"`, `useButtonStyle="true"`} {
			if !strings.Contains(x, want) {
				fatalf("the toast's XML has no %s: %s", want, x)
			}
		}

		// The notification center has it, as it was sent.
		b, err, _ := await(h, "notify", "showing", nil)
		var list []notifyspec.Shown
		if err != nil || json.Unmarshal(b, &list) != nil {
			fatalf("notify.showing: %s, %v", b, err)
		}
		t.Logf("showing: %s", b)
		found := false
		for _, n := range list {
			if n.ID == "ryui-actions" {
				found = n.Title == "Ann" && n.Thread == "chat-1" && n.Data["k"] == "v" && n.URL == "myapp://chat/1" && n.Time > 0
				if !found {
					fatalf("showing's toast: %+v", n)
				}
			}
		}
		if !found && h.notes().toaster.Enabled() {
			fatalf("notify.showing: no ryui-actions in %s", b)
		}

		var got []notifyspec.Response
		next := func() {
			h.asyncModule("notify")["nextAction"](nil, func(b []byte, err error) {
				var r notifyspec.Response
				if err == nil && json.Unmarshal(b, &r) == nil {
					got = append(got, r)
				}
			})
		}
		shown := shownOfSpec(notifyspec.Spec{ID: "ryui-actions", Title: "Ann", URL: "myapp://chat/1", Category: "msg", Thread: "chat-1"}, 1)
		clsid, aumid := h.toastActivatorID(), h.appUserModelID()

		// A reply, through COM: a background action.
		next()
		if err := ActivateToast(clsid, aumid, toastArgsJSON(shown, "reply"), map[string]string{"r:reply": "Sure"}); err != nil {
			fatalf("activating: %v", err)
		}
		if !pumpUntil(func() bool { return len(got) == 1 }) {
			fatalf("no response to the reply")
		}
		if r := got[0]; r.Action != "reply" || r.Reply != "Sure" || r.Notification.ID != "ryui-actions" || r.Notification.Thread != "chat-1" || r.Seconds != actionSeconds {
			fatalf("the reply's response: %+v", r)
		}
		if h.notes().pendingActions[got[0].Token] == nil {
			fatalf("a background action isn't waited for")
		}
		await(h, "notify", "actionDone", []byte(got[0].Token))
		if len(h.notes().pendingActions) != 0 {
			fatalf("actionDone: still waiting")
		}

		// A tap: the app opens its URL.
		next()
		if err := ActivateToast(clsid, aumid, toastArgsJSON(shown, ""), nil); err != nil {
			fatalf("activating: %v", err)
		}
		if !pumpUntil(func() bool { return len(got) == 2 && len(urls) == 1 }) {
			fatalf("a tap: %+v, urls %v", got, urls)
		}
		if got[1].Action != "" || urls[0] != "myapp://chat/1" || len(h.notes().pendingActions) != 0 {
			fatalf("a tap: %+v, urls %v", got[1], urls)
		}

		// One that opens the app isn't waited for.
		next()
		ActivateToast(clsid, aumid, toastArgsJSON(shown, "open"), nil)
		if !pumpUntil(func() bool { return len(got) == 3 }) || got[2].Action != "open" || len(h.notes().pendingActions) != 0 {
			fatalf("an action that opens the app: %+v", got)
		}

		ids, _ := json.Marshal([]string{"ryui-actions"})
		await(h, "notify", "remove", ids)
		b, _, _ = await(h, "notify", "showing", nil)
		if strings.Contains(string(b), "ryui-actions") {
			fatalf("removed, still showing: %s", b)
		}

		// RemoveThread finds the thread's toasts.
		spec, _ = json.Marshal(notifyspec.Spec{ID: "ryui-thread", Title: "T", Thread: "chat-2", Silent: true})
		await(h, "notify", "schedule", spec)
		await(h, "notify", "removeThread", []byte("chat-2"))
		b, _, _ = await(h, "notify", "showing", nil)
		if strings.Contains(string(b), "ryui-thread") {
			fatalf("its thread removed, still showing: %s", b)
		}
	})
}

// Present decides how a toast shown while the app's in front shows:
// "none" shows nothing.
func TestToastPresent(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		active := appActive
		appActive = func(*Host) bool { return true }
		defer func() { appActive = active }()
		await(h, "notify", "present", []byte{1})
		var present notifyspec.Present
		h.asyncModule("notify")["nextPresent"](nil, func(b []byte, err error) { json.Unmarshal(b, &present) })
		spec, _ := json.Marshal(notifyspec.Spec{ID: "ryui-present", Title: "P", Silent: true})
		answered := false
		h.asyncModule("notify")["schedule"](spec, func(_ []byte, err error) {
			if err != nil {
				fatalf("schedule: %v", err)
			}
			answered = true
		})
		if !pumpUntil(func() bool { return present.Token != "" }) || present.Notification.ID != "ryui-present" || answered {
			fatalf("present: %+v (answered %v)", present, answered)
		}
		await(h, "notify", "presented", append([]byte{2}, present.Token...))
		if !answered {
			fatalf("presented: the schedule isn't answered")
		}
		b, _, _ := await(h, "notify", "showing", nil)
		if strings.Contains(string(b), "ryui-present") {
			fatalf("presented none, showing: %s", b)
		}
		await(h, "notify", "present", []byte{0})
	})
}

// The badge is the taskbar button's overlay: a count drawn in a red
// circle.
func TestBadge(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		ShowWindow(h.window, SW_SHOWNOACTIVATE)
		defer ShowWindow(h.window, SW_HIDE)
		if _, err, _ := await(h, "notify", "badge", binary.LittleEndian.AppendUint32(nil, 7)); err != nil {
			fatalf("notify.badge: %v", err)
		}
		if _, err, _ := await(h, "notify", "badge", binary.LittleEndian.AppendUint32(nil, 0)); err != nil {
			fatalf("notify.badge 0: %v", err)
		}
		for _, n := range []int{7, 42, 1000} {
			px := BadgePixels(n, 32)
			at := func(x, y int) []byte { i := (y*32 + x) * 4; return px[i : i+4] }
			if c := at(0, 0); c[3] != 0 {
				fatalf("%d: the corner isn't clear: %v", n, c)
			}
			red, white := 0, 0
			for i := 0; i < len(px); i += 4 {
				switch {
				case px[i+3] == 255 && px[i+2] == 0xC4 && px[i+1] == 0x2B:
					red++
				case px[i+3] == 255 && px[i] > 230 && px[i+1] > 230 && px[i+2] > 230:
					white++
				}
			}
			if red < 200 || white < 10 {
				fatalf("%d: %d red pixels, %d white", n, red, white)
			}
		}
		if BadgeText(1000) != "99+" || BadgeText(5) != "5" {
			fatalf("BadgeText")
		}
	})
}

// toastServer is the process COM starts for a click on a toast of the
// tests' (TestToastColdLaunch): it runs as an app does, and writes what
// OnAction got, and whether its window showed, to the file the
// notification's data names.
func toastServer() {
	Run(func() ui.Node {
		notify.OnAction(func(r notify.Response) {
			out := r.Notification.Data["out"]
			b, _ := json.Marshal(map[string]any{"action": r.Action, "reply": r.Reply, "id": r.Notification.ID,
				"visible": IsWindowVisible(theHost.window)})
			os.WriteFile(out, b, 0o644)
		})
		return ui.StaticText("toast server")
	})
}

// A click on a background action while the app isn't running: COM starts
// it (-ToastActivated), its Go code gets the Response without its window
// showing, and it quits once done.
func TestToastColdLaunch(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	out := filepath.Join(os.TempDir(), "ryui-toast-cold.json")
	os.Remove(out)
	onMain(t, func() {
		if _, err := h.toasts(); err != nil {
			fatalf("toasts: %v", err)
		}
		await(h, "notify", "categories", toastCategories)
		// This process isn't the activator: COM starts another.
		StopToastActivator()
		h.notes().activator = false
		defer h.startActivator()
		shown := notifyspec.Shown{ID: "ryui-cold", Category: "msg", Data: map[string]string{"out": out}}
		if err := ActivateToast(h.toastActivatorID(), h.appUserModelID(), toastArgsJSON(shown, "reply"), map[string]string{"r:reply": "From cold"}); err != nil {
			fatalf("activating: %v", err)
		}
	})
	var b []byte
	for range 600 {
		if b, _ = os.ReadFile(out); len(b) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var got map[string]any
	if json.Unmarshal(b, &got) != nil || got["action"] != "reply" || got["reply"] != "From cold" || got["id"] != "ryui-cold" || got["visible"] != false {
		t.Fatalf("the app COM started: %s", b)
	}
}
