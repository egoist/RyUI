//go:build e2e

// Package e2e drives the example apps on a simulator and an emulator,
// through the platform's widgets, not the core. The tests build each app
// with `ryui dev -once` (its Go code embedded, a debug build), then tap
// and read the screen through the debug hosts' automation
// (platform/ios/Sources/RyUIHost/Automation.swift, a notification;
// platform/android/ryui/src/main/java/dev/ryui/Automation.kt, a broadcast;
// platform/web/src/automation.js, through ryui dev -web's server).
//
//	go test -tags e2e ./e2e -run IOS      # a booted simulator, else the newest iPhone
//	go test -tags e2e ./e2e -run Android  # a running emulator or a device
//	go test -tags e2e ./e2e -run Web      # headless Chrome (RYUI_CHROME: its path)
//	RYUI_DEVICE=emulator-5556 go test -tags e2e ./e2e -run Android  # one of several
//
// They wait for what they expect (bounded, so a broken app fails rather
// than hangs), never for a fixed time.
package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// within is how long a test waits for the screen to show what it expects.
const within = 60 * time.Second

func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// appDir is an app's directory from the repository's root: an example's
// name, or a path (conformance/app).
func appDir(app string) string {
	if strings.Contains(app, "/") {
		return app
	}
	return "examples/" + app
}

// launch builds and launches an app (appDir) on the platform with ryui
// dev -once, and returns what it printed. RYUI_DEVICE picks the
// simulator, emulator or device (ryui dev's -device), when there's more
// than one.
func launch(t *testing.T, platform, app string) string {
	t.Helper()
	args := []string{"run", "./cmd/ryui", "dev", "-platform", platform, "-once"}
	if d := os.Getenv("RYUI_DEVICE"); d != "" {
		args = append(args, "-device", d)
	}
	c := exec.Command("go", append(args, appDir(app))...)
	c.Dir = root(t)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("ryui dev -platform %s -once %s: %v\n%s", platform, appDir(app), err, out)
	}
	return string(out)
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// screen is what an app's screen shows, and input to it.
type screen interface {
	texts() []string
	tap(text string)
	command(cmds string) // the automation's: type <text>, submit...
	relaunch()           // quits the app and starts it again
}

// waitFor waits until the screen shows every one of want and none of
// not; it fails the test if it doesn't within the bound.
func waitFor(t *testing.T, s screen, want []string, not ...string) {
	t.Helper()
	deadline := time.Now().Add(within)
	var got []string
	for time.Now().Before(deadline) {
		got = s.texts()
		if hasAll(got, want) && !hasAny(got, not) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the screen doesn't show %q (without %q); it shows %q", want, not, got)
}

func hasAll(got, want []string) bool {
	for _, w := range want {
		if !hasAny(got, []string{w}) {
			return false
		}
	}
	return true
}

func hasAny(got, any []string) bool {
	for _, g := range got {
		for _, a := range any {
			if g == a {
				return true
			}
		}
	}
	return false
}

// counter is the same test on each platform: the counter counts, shows
// a note past ten, and resets.
func counter(t *testing.T, s screen) {
	waitFor(t, s, []string{"Count: 0", "Add", "Reset"}, "That's a lot")
	for i := 1; i <= 10; i++ {
		s.tap("Add")
		waitFor(t, s, []string{fmt.Sprintf("Count: %d", i)})
	}
	waitFor(t, s, []string{"That's a lot"})
	s.tap("Reset")
	waitFor(t, s, []string{"Count: 0"}, "That's a lot")
}

// notes is the same test on each platform: a note typed and submitted
// is written to SQLite and listed, and is still there when the app
// starts again.
func notes(t *testing.T, s screen) {
	waitFor(t, s, []string{"Add"})
	note := fmt.Sprintf("e2e note %d", time.Now().UnixNano())
	s.command("type " + note)
	s.command("submit")
	waitFor(t, s, []string{note})
	s.relaunch()
	waitFor(t, s, []string{"Add", note})
}

// navigation is the same test on each platform: a row pushes its
// screen; a button there presents a sheet, whose field and Save button
// write back to the screen; back pops it; a tab shows another stack.
func navigation(t *testing.T, s screen) {
	waitFor(t, s, []string{"Message 20", "Message 19"})
	s.tap("Message 20")
	waitFor(t, s, []string{"Add a note", "No note yet."})
	s.tap("Add a note")
	waitFor(t, s, []string{"Save", "Note"})
	s.command("type hello")
	s.tap("Save")
	waitFor(t, s, []string{"Note: hello"}, "Save")
	s.command("back")
	waitFor(t, s, []string{"Message 19"}, "Add a note", "Note: hello")
	s.command("tab Settings")
	waitFor(t, s, []string{"Notifications", "About"}, "Message 19")
	s.tap("About")
	waitFor(t, s, []string{"Native widgets, driven by Go and fine-grained signals."})
	s.command("back")
	waitFor(t, s, []string{"About"}, "Native widgets, driven by Go and fine-grained signals.")
	s.command("tab Inbox")
	waitFor(t, s, []string{"Message 19"}, "Notifications")
}

// languages is the same test on each platform, from an app in English: its
// languages menu changes what the screen says at once, keeping the state the
// user made, and the choice is there when the app starts again.
func languages(t *testing.T, s screen) {
	waitFor(t, s, []string{"Hello, Ada!", "3 unread messages"})
	s.tap("More")
	waitFor(t, s, []string{"4 unread messages"})
	s.command("tab Settings")
	waitFor(t, s, []string{"English", "Deutsch", "العربية"}, "Hello, Ada!")
	s.tap("العربية")
	waitFor(t, s, []string{"مثل النظام"})
	s.command("tab الوارد")
	waitFor(t, s, []string{"مرحبًا، Ada!", "4 رسائل غير مقروءة"}, "Hello, Ada!")
	s.command("tab الإعدادات")
	s.tap("Deutsch")
	s.command("tab Eingang")
	waitFor(t, s, []string{"Hallo, Ada!", "4 ungelesene Nachrichten"}, "مرحبًا، Ada!")
	s.relaunch()
	waitFor(t, s, []string{"Hallo, Ada!", "3 ungelesene Nachrichten"})
	s.command("tab Einstellungen")
	s.tap("English")
	s.command("tab Inbox")
	waitFor(t, s, []string{"Hello, Ada!"}, "Hallo, Ada!")
}

// chat is the same test on each platform: the conversation opens at its
// newest message (a list anchored at its bottom); a message sent from
// the composer shows, and so does the answer that comes after it, the
// list staying at its end.
func chat(t *testing.T, s screen) {
	waitFor(t, s, []string{"The sticky headers look right on Android now.", "Send"})
	msg := fmt.Sprintf("e2e %d", time.Now().UnixNano())
	s.command("type " + msg)
	s.tap("Send")
	waitFor(t, s, []string{msg, "Got it: “" + msg + "”"})
}

// iOS

type iosScreen struct {
	t      *testing.T
	udid   string
	bundle string
	tmp    string // the app's data container's tmp, where the automation reads and writes
}

func (s *iosScreen) command(cmds string) {
	file := filepath.Join(s.tmp, "ryui-commands.txt")
	// The app removes the file as it reads it. One it hasn't read yet (its main
	// thread is showing the keyboard, say) mustn't be written over, which would
	// lose that command. An app just launched may never read one sent before it
	// listened, so this is bounded.
	for range 500 {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(file, []byte(cmds+"\n"), 0o644); err != nil {
		s.t.Fatal(err)
	}
	run(s.t, "xcrun", "simctl", "spawn", s.udid, "notifyutil", "-p", "dev.ryui.automation")
}

func (s *iosScreen) texts() []string {
	dump := filepath.Join(s.tmp, "ryui-dump.txt")
	os.Remove(dump)
	s.command("dump")
	for range 50 {
		if b, err := os.ReadFile(dump); err == nil {
			return strings.Split(strings.TrimSpace(string(b)), "\n")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *iosScreen) tap(text string) { s.command("tap " + text) }

func (s *iosScreen) relaunch() {
	os.Remove(filepath.Join(s.tmp, "ryui-commands.txt")) // for the run that's ending
	run(s.t, "xcrun", "simctl", "launch", "--terminate-running-process", s.udid, s.bundle)
}

func iosApp(t *testing.T, example, bundle string) *iosScreen {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode")
	}
	out := launch(t, "ios", example)
	m := regexp.MustCompile(`\(([0-9A-F-]{36})\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no simulator in:\n%s", out)
	}
	data := strings.TrimSpace(run(t, "xcrun", "simctl", "get_app_container", m[1], bundle, "data"))
	return &iosScreen{t: t, udid: m[1], bundle: bundle, tmp: filepath.Join(data, "tmp")}
}

func TestCounterIOS(t *testing.T) { counter(t, iosApp(t, "counter", "dev.ryui.counter")) }
func TestNotesIOS(t *testing.T)   { notes(t, iosApp(t, "notes", "dev.ryui.notes")) }
func TestChatIOS(t *testing.T)    { chat(t, iosApp(t, "chat", "dev.ryui.chat")) }
func TestNavigationIOS(t *testing.T) {
	navigation(t, iosApp(t, "navigation", "dev.ryui.navigation"))
}

// TestLanguagesIOS changes examples/i18n's language: at once, from its
// menu, and kept for the next launch (AppleLanguages) until it follows the
// system again. The simulator's language is English, as for the other tests.
func TestLanguagesIOS(t *testing.T) {
	s := iosApp(t, "i18n", "dev.ryui.i18n")
	reset := func() {
		exec.Command("xcrun", "simctl", "spawn", s.udid, "defaults", "delete", s.bundle, "AppleLanguages").Run() // none to delete is fine
	}
	reset()
	t.Cleanup(reset)
	s.relaunch()
	waitFor(t, s, []string{"Hello, Ada!", "More"})
	s.command("tab Settings")
	waitFor(t, s, []string{"As the system's", "Deutsch"}, "Hello, Ada!")
	s.tap("Deutsch")
	waitFor(t, s, []string{"Wie das System", "Deutsch"}, "As the system's")
	s.command("tab Eingang")
	waitFor(t, s, []string{"Hallo, Ada!", "Mehr"}, "Hello, Ada!")
	s.relaunch()
	waitFor(t, s, []string{"Hallo, Ada!", "Mehr"}, "Hello, Ada!")
	s.command("tab Einstellungen")
	waitFor(t, s, []string{"Wie das System"})
	s.tap("العربية")
	waitFor(t, s, []string{"مثل النظام"}, "Wie das System")
	s.command("tab الوارد")
	waitFor(t, s, []string{"المزيد"}, "Mehr")
	s.command("tab الإعدادات")
	waitFor(t, s, []string{"مثل النظام"}, "المزيد")
	s.tap("مثل النظام")
	waitFor(t, s, []string{"As the system's"}, "مثل النظام")
	s.relaunch()
	waitFor(t, s, []string{"Hello, Ada!", "More"})
}

// Android

type androidScreen struct {
	t      *testing.T
	serial string
	pkg    string
}

func (s *androidScreen) adb(args ...string) (string, error) {
	out, err := exec.Command("adb", append([]string{"-s", s.serial}, args...)...).CombinedOutput()
	return string(out), err
}

func (s *androidScreen) command(cmds string) {
	// adb shell joins its arguments into a command line: quoted once more.
	q := "'" + strings.ReplaceAll(cmds, "'", `'\''`) + "'"
	// adb fails now and then just after an install: tried again.
	var out string
	var err error
	for range 5 {
		if out, err = s.adb("shell", "am", "broadcast", "-a", "dev.ryui.automation", "-p", s.pkg, "--es", "commands", q); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.t.Fatalf("broadcast %q: %v\n%s", cmds, err, out)
}

func (s *androidScreen) texts() []string {
	s.adb("shell", "run-as", s.pkg, "rm", "-f", "files/ryui-dump.txt")
	s.command("dump")
	for range 50 {
		if out, err := s.adb("exec-out", "run-as", s.pkg, "cat", "files/ryui-dump.txt"); err == nil && strings.HasSuffix(out, "\n") {
			return strings.Split(strings.TrimSpace(out), "\n")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *androidScreen) tap(text string) { s.command("tap " + text) }

func (s *androidScreen) relaunch() {
	s.adb("shell", "am", "force-stop", s.pkg)
	// Not monkey: it refuses to run on recent emulator images, which have no system keys.
	if out, err := s.adb("shell", "am", "start", "-n", s.pkg+"/dev.ryui.RyUIActivity"); err != nil {
		s.t.Fatalf("launch: %v\n%s", err, out)
	}
}

func androidApp(t *testing.T, example, pkg string) *androidScreen {
	if _, err := exec.LookPath("adb"); err != nil {
		t.Skip("no adb")
	}
	out := launch(t, "android", example)
	m := regexp.MustCompile(`(?m)^ryui: \S+ on (\S+)$`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no device in:\n%s", out)
	}
	return &androidScreen{t: t, serial: m[1], pkg: pkg}
}

func TestCounterAndroid(t *testing.T) { counter(t, androidApp(t, "counter", "dev.ryui.counter")) }
func TestNotesAndroid(t *testing.T)   { notes(t, androidApp(t, "notes", "dev.ryui.notes")) }
func TestChatAndroid(t *testing.T)    { chat(t, androidApp(t, "chat", "dev.ryui.chat")) }
func TestNavigationAndroid(t *testing.T) {
	navigation(t, androidApp(t, "navigation", "dev.ryui.navigation"))
}
func TestLanguagesAndroid(t *testing.T) {
	s := androidApp(t, "i18n", "dev.ryui.i18n")
	// The language a run before this one kept (Android 13's; before it the
	// test leaves English): what the test starts from is the app's own.
	s.adb("shell", "cmd", "locale", "set-app-locales", s.pkg, "--locales", "en")
	s.relaunch()
	languages(t, s)
}

// Web

// webScreen is a page ryui dev -web serves, open in headless Chrome; the
// automation's commands go through the dev server to the page.
type webScreen struct {
	t   *testing.T
	url string
}

func (s *webScreen) post(cmds string) (string, error) {
	resp, err := http.Post(s.url+"__ryui/automation", "text/plain", strings.NewReader(cmds))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", resp.Status, b)
	}
	return string(b), nil
}

func (s *webScreen) command(cmds string) {
	// The page may not be listening yet (just loaded): tried again.
	var err error
	for range 50 {
		if _, err = s.post(cmds); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.t.Fatalf("%q: %v", cmds, err)
}

func (s *webScreen) texts() []string {
	out, err := s.post("dump")
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

func (s *webScreen) tap(text string) { s.command("tap " + text) }

func (s *webScreen) relaunch() {
	s.command("reload")
	time.Sleep(time.Second) // the page goes; the next command waits for the new one
}

// chrome is Chrome's (or Chromium's) executable.
func chrome() string {
	if p := os.Getenv("RYUI_CHROME"); p != "" {
		return p
	}
	for _, p := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

func webApp(t *testing.T, example string) *webScreen {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (RYUI_CHROME)")
	}
	url := webServer(t, example)
	page := exec.Command(browser, "--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-gpu",
		"--user-data-dir="+t.TempDir(), "--window-size=430,932", url)
	if err := page.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { page.Process.Kill(); page.Wait() })
	return &webScreen{t: t, url: url}
}

// webServer serves an app (appDir) with ryui dev -platform web, and
// returns its URL.
func webServer(t *testing.T, example string) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	// The CLI built, not go run: killing it then stops the server it is.
	cli := filepath.Join(t.TempDir(), "ryui")
	build := exec.Command("go", "build", "-o", cli, "./cmd/ryui")
	build.Dir = root(t)
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the CLI: %v\n%s", err, b)
	}
	dev := exec.Command(cli, "dev", "-platform", "web", "-no-open", "-vet=false", "-port", fmt.Sprint(port), appDir(example))
	dev.Dir = root(t)
	out, _ := dev.StdoutPipe()
	var logs strings.Builder
	dev.Stderr = &logs
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("ryui dev -web:\n%s", logs.String())
		}
	})
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Process.Kill(); dev.Wait() })
	url := ""
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if m := regexp.MustCompile(`at (http://\S+/)`).FindStringSubmatch(sc.Text()); m != nil {
			url = m[1]
			break
		}
	}
	if url == "" {
		t.Fatal("ryui dev -web printed no URL")
	}
	go io.Copy(io.Discard, out)
	return url
}

func TestCounterWeb(t *testing.T)    { counter(t, webApp(t, "counter")) }
func TestChatWeb(t *testing.T)       { chat(t, webApp(t, "chat")) }
func TestNavigationWeb(t *testing.T) { navigation(t, webApp(t, "navigation")) }
func TestLanguagesWeb(t *testing.T)  { languages(t, webApp(t, "i18n")) }
