//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/asuka/push/webpush"
)

// The test app's VAPID keys (e2e/testdata/webpush/asuka.json has the
// public one): a server's, made with webpush.GenerateKeys.
var testVAPID = webpush.Keys{
	Public:  "BKpYsAT0ADj2pHQQZeNsXmA3fgij4OZ_7ZqkS63fvpscgjHLZys0UDBluROJdtDaGlE3WW1_IlFyzVzC2LlHyO0",
	Private: "aSlzAHu9DNqIS4bVRboSjMzQb5isUzU8INUM6tcHZmw",
}

// TestWebPushWeb drives Web Push end to end in headless Chrome, but for
// the browser's own push service (Chrome's is Google's, which a test
// can't stand in for): the page's PushManager is stubbed to subscribe
// with a key pair the test holds, at a fake push service; push.Register
// gives the app that subscription; the server's side (asuka/push/webpush)
// encrypts and signs a push to it; the fake service checks the VAPID
// token, decrypts the push as a browser would and delivers it to the
// app's service worker (DevTools' ServiceWorker.deliverPushMessage), which
// shows it and hands it to the page (push.OnReceive). Its click goes
// through the worker's own handler (DevTools has no way to click a
// notification): OnAction, OnOpen, OnOpenURL. Then a notification
// scheduled with asuka/notify, which comes due while no page is open,
// shows as the app's page opens again. What shows is read from the
// showNotification calls (recorded), not getNotifications: Chrome
// reconciles that with what the system displays, and may leave out one
// it's still showing.
func TestWebPushWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	pageURL := webServer(t, "e2e/testdata/webpush")
	u, _ := url.Parse(pageURL)
	origin := u.Scheme + "://" + u.Host
	c := launchCDP(t, browser)

	// The subscription the stub gives: the browser's keys are the test's.
	ua, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	b64 := base64.RawURLEncoding

	// The fake push service: what a browser's would check and do.
	var page, registrationID string
	delivered := make(chan error, 8)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		err := func() error {
			if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" {
				return fmt.Errorf("headers %v", r.Header)
			}
			if err := checkVAPID(r.Header.Get("Authorization"), "http://"+r.Host, testVAPID.Public); err != nil {
				return err
			}
			plain, err := decryptPush(body, ua, auth)
			if err != nil {
				return fmt.Errorf("decrypting: %w", err)
			}
			var res struct{}
			return c.call("", "ServiceWorker.deliverPushMessage", map[string]any{"origin": origin, "registrationId": registrationID, "data": string(plain)}, &res, page)
		}()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		delivered <- err
	}))
	t.Cleanup(service.Close)
	endpoint := service.URL + "/push/1"

	// Notifications allowed, PushManager stubbed, before the page loads.
	var none struct{}
	if err := c.call("", "Browser.grantPermissions", map[string]any{"origin": origin, "permissions": []string{"notifications"}}, &none, ""); err != nil {
		t.Fatal(err)
	}
	var created struct{ TargetID string }
	if err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"}, &created, ""); err != nil {
		t.Fatal(err)
	}
	page = c.attach(t, created.TargetID)
	sub, _ := json.Marshal(map[string]any{"endpoint": endpoint, "expirationTime": nil, "keys": map[string]string{"p256dh": b64.EncodeToString(ua.PublicKey().Bytes()), "auth": b64.EncodeToString(auth)}})
	stub := `(() => {
  const sub = ` + string(sub) + `;
  let current = null;
  PushManager.prototype.getSubscription = async function () { return current; };
  PushManager.prototype.subscribe = async function (o) {
    const key = new Uint8Array(o.applicationServerKey);
    globalThis.asukaTestKey = btoa(String.fromCharCode(...key)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    globalThis.asukaTestVisibleOnly = o.userVisibleOnly;
    current = { endpoint: sub.endpoint, options: { applicationServerKey: key.buffer, userVisibleOnly: o.userVisibleOnly }, toJSON: () => sub, unsubscribe: async () => { current = null; return true; } };
    return current;
  };
  // What the page shows (notify's, through the worker's registration).
  const show = ServiceWorkerRegistration.prototype.showNotification;
  ServiceWorkerRegistration.prototype.showNotification = function (title, o) {
    // Kept across the tab's pages (the test leaves the app and comes back).
    const log = JSON.parse(sessionStorage.getItem("asukaTestShown") || "[]");
    log.push(o.tag + "|" + title + "|" + (o.data && o.data.shown.time));
    sessionStorage.setItem("asukaTestShown", JSON.stringify(log));
    return show.call(this, title, o);
  };
})();`
	for _, m := range []struct {
		method string
		params any
	}{
		{"Page.enable", map[string]any{}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": stub}},
		{"ServiceWorker.enable", map[string]any{}},
		{"Page.navigate", map[string]any{"url": pageURL}},
	} {
		var res json.RawMessage
		if err := c.call(page, m.method, m.params, &res, ""); err != nil {
			t.Fatal(err)
		}
	}
	s := &webScreen{t: t, url: pageURL}
	showsLine(t, s, "status: ready")

	// The page registered the app's worker (sw.js), scoped to the app.
	if got := c.eval(t, page, `navigator.serviceWorker.ready.then((r) => r.active.scriptURL + " " + r.scope)`); got != pageURL+"sw.js "+pageURL {
		t.Fatalf("the worker: %q", got)
	}
	registrationID = c.registration(t, pageURL)
	// What the worker shows, recorded as it shows it: what it asks
	// showNotification for (getNotifications, which Chrome reconciles with
	// what's displayed, may miss one it's still showing).
	worker := c.workerSession(t, pageURL)
	if got := c.eval(t, worker, `(() => {
  globalThis.shownLog = [];
  const show = registration.showNotification.bind(registration);
  registration.showNotification = (title, o) => { shownLog.push({ title, o }); return show(title, o); };
  return "ok";
})()`); got != "ok" {
		t.Fatal(got)
	}

	// push.Register: subscribed with the app's key; the token is the subscription.
	s.tap("Register")
	showsLine(t, s, "status: registered webpush")
	if got := c.eval(t, page, `globalThis.asukaTestKey + " " + globalThis.asukaTestVisibleOnly`); got != testVAPID.Public+" true" {
		t.Fatalf("subscribed with %q", got)
	}
	token := strings.TrimPrefix(lineOf(s, "token: "), "token: ")
	got, err := webpush.ParseSubscription(token)
	if err != nil || got.Endpoint != endpoint {
		t.Fatalf("the token %q: %v", token, err)
	}

	// The server sends a push; it shows, with the chat category's actions, and the page gets it.
	sender := &webpush.Sender{Keys: testVAPID, Subject: "mailto:e2e@example.com"}
	send := func(n webpush.Notification) {
		t.Helper()
		if err := sender.Send(context.Background(), got, n, nil); err != nil {
			t.Fatal(err)
		}
		if err := <-delivered; err != nil {
			t.Fatal(err)
		}
	}
	send(webpush.Notification{ID: "m1", Title: "Ada", Body: "Lunch?", Category: "chat", Data: map[string]string{"k": "v1"}})
	showsLine(t, s, "received: Ada v1")
	shown := func() string {
		return c.eval(t, worker, `shownLog.map(({ title, o }) => o.tag + "|" + title + "|" + o.body + "|" + o.actions.map((a) => a.action + (a.type === "text" ? ":text" : "")).join(",")).join(";")`)
	}
	until(t, func() bool { return shown() == "m1|Ada|Lunch?|like,reply:text" }, func() string { return shown() + "\n" + c.workerState(t, pageURL) })

	// An inline reply, through the worker's notificationclick, which closes it.
	click := func(tag, action, reply string) {
		t.Helper()
		js := fmt.Sprintf(`(async () => {
  const { o } = shownLog.find(({ o }) => o.tag === %[1]q);
  let closed = false;
  await asukaWorker.notificationclick({ notification: { tag: %[1]q, data: o.data, close() { closed = true; } }, action: %[2]q, reply: %[3]q });
  return closed ? "closed" : "open";
})()`, tag, action, reply)
		if got := c.eval(t, worker, js); got != "closed" {
			t.Fatalf("click: %q", got)
		}
	}
	click("m1", "reply", "On my way")
	showsLine(t, s, "action: [reply] [On my way] m1")
	showsLine(t, s, "opened: Ada")

	// A tap: OnAction's default action, and the push's data's URL.
	send(webpush.Notification{ID: "m2", Title: "Grace", Body: "Look", Data: map[string]string{"k": "v2", "url": "asukatest://chat/2"}})
	showsLine(t, s, "received: Grace v2")
	until(t, func() bool { return strings.HasSuffix(shown(), ";m2|Grace|Look|") }, func() string { return shown() + "\n" + c.workerState(t, pageURL) })
	click("m2", "", "")
	showsLine(t, s, "action: [] [] m2")
	showsLine(t, s, "opened: Grace")
	showsLine(t, s, "url: asukatest://chat/2")

	// A notification due while no page of the app is open shows as one opens.
	s.tap("Schedule")
	showsLine(t, s, "status: scheduled")
	var res json.RawMessage
	if err := c.call(page, "Page.navigate", map[string]any{"url": "about:blank"}, &res, ""); err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(5 * time.Second)
	for time.Now().Before(due) {
		time.Sleep(100 * time.Millisecond) // until it's due, the page closed
	}
	reopened := time.Now().UnixMilli()
	if err := c.call(page, "Page.navigate", map[string]any{"url": pageURL}, &res, ""); err != nil {
		t.Fatal(err)
	}
	showsLine(t, s, "status: ready")
	reminder := func() string {
		return c.eval(t, page, `JSON.parse(sessionStorage.getItem("asukaTestShown") || "[]").filter((l) => l.startsWith("later|")).map((l) => l.split("|")[1] + "|" + (+l.split("|")[2] >= `+fmt.Sprint(reopened)+`)).join(";")`)
	}
	until(t, func() bool { return reminder() == "Reminder|true" }, reminder)
}

// showsLine waits for the screen to show want (a line).
func showsLine(t *testing.T, s *webScreen, want string) {
	t.Helper()
	until(t, func() bool { return lineOf(s, want) == want }, func() string { return strings.Join(s.texts(), "\n") })
}

// lineOf is the first line on screen that starts with prefix.
func lineOf(s *webScreen, prefix string) string {
	for _, l := range s.texts() {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// until waits for ok, bounded; state says what there is if it never comes.
func until(t *testing.T, ok func() bool, state func() string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out; last:\n%s", state())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// cdp is a headless Chrome's browser-wide DevTools connection, with
// sessions on its targets (flattened): one reader, calls from any
// goroutine.
type cdp struct {
	d      *devtools
	sendMu sync.Mutex
	mu     sync.Mutex
	id     int
	waits  map[int]chan cdpMessage
	events []cdpMessage
}

type cdpMessage struct {
	ID        int
	Method    string
	SessionID string
	Params    json.RawMessage
	Result    json.RawMessage
	Error     *struct{ Message string }
}

func launchCDP(t *testing.T, browser string) *cdp {
	t.Helper()
	// Chrome's own notification center, not the system's (macOS's, which
	// may not show, or keep, a test's notifications: Chrome then forgets
	// them, and getNotifications lists none).
	cmd := exec.Command(browser, "--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-gpu",
		"--disable-features=NativeNotifications,SystemNotifications",
		"--user-data-dir="+t.TempDir(), "--window-size=430,932", "--remote-debugging-port=0", "about:blank")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	ws := ""
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if m := regexp.MustCompile(`DevTools listening on (ws://\S+)`).FindStringSubmatch(sc.Text()); m != nil {
			ws = m[1]
			break
		}
	}
	if ws == "" {
		t.Fatal("Chrome printed no DevTools address")
	}
	go io.Copy(io.Discard, stderr)
	d, err := dialWebSocket(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.conn.Close() })
	c := &cdp{d: d, waits: map[int]chan cdpMessage{}}
	go c.read()
	return c
}

func (c *cdp) read() {
	for {
		b, err := c.d.receive()
		if err != nil {
			c.mu.Lock()
			for id, w := range c.waits {
				close(w)
				delete(c.waits, id)
			}
			c.mu.Unlock()
			return
		}
		var m cdpMessage
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		c.mu.Lock()
		if m.ID != 0 {
			if w := c.waits[m.ID]; w != nil {
				w <- m
				delete(c.waits, m.ID)
			}
		} else {
			c.events = append(c.events, m)
		}
		c.mu.Unlock()
	}
}

// call sends method to session ("" the browser; or, with via, the
// browser-level method sent on that session) and decodes its result.
func (c *cdp) call(session, method string, params, result any, via string) error {
	if via != "" {
		session = via
	}
	c.mu.Lock()
	c.id++
	id := c.id
	w := make(chan cdpMessage, 1)
	c.waits[id] = w
	c.mu.Unlock()
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	c.sendMu.Lock()
	err := c.d.send(b)
	c.sendMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case m, ok := <-w:
		if !ok {
			return errors.New("DevTools closed")
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		return json.Unmarshal(m.Result, result)
	case <-time.After(within):
		return fmt.Errorf("%s: no answer", method)
	}
}

func (c *cdp) attach(t *testing.T, target string) string {
	t.Helper()
	var a struct{ SessionID string }
	if err := c.call("", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &a, ""); err != nil {
		t.Fatal(err)
	}
	return a.SessionID
}

// eval evaluates js in session, awaiting it, and returns its value as text.
func (c *cdp) eval(t *testing.T, session, js string) string {
	t.Helper()
	var r struct {
		Result           struct{ Value any }
		ExceptionDetails *struct {
			Text      string
			Exception struct{ Description string }
		}
	}
	if err := c.call(session, "Runtime.evaluate", map[string]any{"expression": js, "awaitPromise": true, "returnByValue": true}, &r, ""); err != nil {
		t.Fatal(err)
	}
	if e := r.ExceptionDetails; e != nil {
		t.Fatalf("%s: %s %s", js, e.Text, e.Exception.Description)
	}
	return fmt.Sprint(r.Result.Value)
}

// registration is the ID of the worker registration for scope, as
// DevTools' ServiceWorker events say.
func (c *cdp) registration(t *testing.T, scope string) string {
	t.Helper()
	id := ""
	until(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, e := range c.events {
			if e.Method != "ServiceWorker.workerRegistrationUpdated" {
				continue
			}
			var p struct {
				Registrations []struct {
					RegistrationID, ScopeURL string
					IsDeleted                bool
				}
			}
			json.Unmarshal(e.Params, &p)
			for _, r := range p.Registrations {
				if r.ScopeURL == scope && !r.IsDeleted {
					id = r.RegistrationID
				}
			}
		}
		return id != ""
	}, func() string { return "no registration for " + scope })
	return id
}

// workerSession attaches to the app's service worker (started, if it
// stopped), to run its handlers.
func (c *cdp) workerSession(t *testing.T, scope string) string {
	t.Helper()
	var none json.RawMessage
	target := ""
	until(t, func() bool {
		c.call("", "ServiceWorker.startWorker", map[string]any{"scopeURL": scope}, &none, "")
		var ts struct {
			TargetInfos []struct{ TargetID, Type, URL string }
		}
		if err := c.call("", "Target.getTargets", map[string]any{}, &ts, ""); err != nil {
			return false
		}
		for _, ti := range ts.TargetInfos {
			if ti.Type == "service_worker" && strings.HasPrefix(ti.URL, scope) {
				target = ti.TargetID
			}
		}
		return target != ""
	}, func() string { return "no service worker target" })
	return c.attach(t, target)
}

// workerState is what the app's worker sees and said, for a failure.
func (c *cdp) workerState(t *testing.T, scope string) string {
	w := c.workerSession(t, scope)
	var none json.RawMessage
	c.call(w, "Runtime.enable", map[string]any{}, &none, "")
	shown := c.eval(t, w, `registration.getNotifications().then((ns) => "worker shows " + ns.map((n) => n.tag).join(","))`)
	time.Sleep(500 * time.Millisecond) // the console's messages, replayed as Runtime is enabled
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{shown}
	for _, e := range c.events {
		if e.SessionID == w && (e.Method == "Runtime.consoleAPICalled" || e.Method == "Runtime.exceptionThrown") {
			out = append(out, string(e.Params))
		}
	}
	return strings.Join(out, "\n")
}

// checkVAPID checks a push's Authorization header as a push service does
// (RFC 8292): the key is the app's, the token's signature is the key's,
// for this service's origin, not expired.
func checkVAPID(h, aud, key string) error {
	rest, ok := strings.CutPrefix(h, "vapid t=")
	jwt, k, ok2 := strings.Cut(rest, ", k=")
	if !ok || !ok2 || k != key {
		return fmt.Errorf("authorization %q", h)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return fmt.Errorf("token %q", jwt)
	}
	b64 := base64.RawURLEncoding
	pk, _ := b64.DecodeString(k)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pk)
	if err != nil {
		return err
	}
	sig, _ := b64.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return errors.New("the token's signature doesn't verify")
	}
	cb, _ := b64.DecodeString(parts[1])
	var claims struct {
		Aud string
		Exp int64
		Sub string
	}
	json.Unmarshal(cb, &claims)
	if claims.Aud != aud || claims.Exp < time.Now().Unix() || claims.Exp > time.Now().Add(24*time.Hour).Unix() || claims.Sub == "" {
		return fmt.Errorf("claims %+v (want aud %s)", claims, aud)
	}
	return nil
}

// decryptPush is a browser's side of RFC 8291 (aes128gcm).
func decryptPush(body []byte, ua *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("short")
	}
	salt, idlen := body[:16], int(body[20])
	if binary.BigEndian.Uint32(body[16:20]) < 18 || len(body) < 21+idlen {
		return nil, errors.New("bad header")
	}
	as, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		return nil, err
	}
	secret, err := ua.ECDH(as)
	if err != nil {
		return nil, err
	}
	prkKey, _ := hkdf.Extract(sha256.New, secret, auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(as.Bytes()), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		return nil, err
	}
	i := bytes.LastIndexByte(plain, 2)
	if i < 0 {
		return nil, errors.New("no padding delimiter")
	}
	return plain[:i], nil
}
