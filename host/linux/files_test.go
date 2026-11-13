//go:build linux && !android

package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/fileurl"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ui"
)

// The application is unique (one instance, which gets the others'
// command lines) unless it has no ID, runs for automation or asuka dev,
// or asked to run as often as it's started.
func TestAppFlags(t *testing.T) {
	none := func(string) string { return "" }
	if f := appFlags("com.example.Notes", "", none); f&appNonUnique != 0 || f&appHandlesCommandLine == 0 || f&appHandlesOpen == 0 {
		t.Fatalf("an app with an ID: %#x", f)
	}
	for _, c := range []struct {
		id, multiple string
		env          string
	}{{"", "", ""}, {"com.example.Notes", "1", ""}, {"com.example.Notes", "", "ASUKA_STATE"}, {"com.example.Notes", "", "ASUKA_SNAPSHOT"}} {
		getenv := func(k string) string {
			if k == c.env {
				return "x"
			}
			return ""
		}
		if appFlags(c.id, c.multiple, getenv)&appNonUnique == 0 {
			t.Errorf("%+v: unique", c)
		}
	}
	if got := launchArgs([]string{"/bin/notes", boot.LoginArg, "a.md"}); fmt.Sprint(got) != "[a.md]" {
		t.Errorf("launchArgs = %q", got)
	}
}

// Another launch's arguments reach OnSecondInstance, and its files (from
// its working directory) OnOpenFiles, together; the files GApplication's
// open gives are paths too, its links URLs.
func TestLaunchedAgain(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a b.md", "c.md"} {
		os.WriteFile(filepath.Join(dir, f), nil, 0o644)
	}
	var files [][]string
	var urls []string
	var second []string
	prev := boot.SecondInstance
	boot.SecondInstance = func(args []string, cwd string) { second = append(second, fmt.Sprint(args, " in ", cwd)) }
	t.Cleanup(func() { onMain(func() { boot.SecondInstance = prev }) })
	h, _ := mount(t, func() ui.Node {
		ui.OnOpenFiles(func(ps []string) { files = append(files, ps) })
		ui.OnOpenURL(func(u string) { urls = append(urls, u) })
		return ui.StaticText("x")
	})
	onMain(func() {
		h.launchedAgain([]string{"--new-window", "a b.md", "missing.md", "c.md"}, dir)
		f := Sys.FileNewForPath(filepath.Join(dir, "c.md"))
		u := Sys.FileNewForURI("myapp://note/1")
		h.incomingURL(openedString([]Ptr{f, u}))
		ObjectUnref(f)
		ObjectUnref(u)
	})
	settle(t, h, func() bool { return len(files) == 2 })
	onMain(func() {
		want := fmt.Sprint([][]string{{filepath.Join(dir, "a b.md"), filepath.Join(dir, "c.md")}, {filepath.Join(dir, "c.md")}})
		if fmt.Sprint(files) != want || fmt.Sprint(urls) != "[myapp://note/1]" {
			fatalf(t, "files %q, urls %q", files, urls)
		}
		if fmt.Sprint(second) != fmt.Sprintf("[[--new-window a b.md missing.md c.md] in %s]", dir) {
			fatalf(t, "OnSecondInstance got %q", second)
		}
	})
}

// A quit asks OnQuit's handlers first: one that cancels keeps the app
// and its last window open; once allowed, it's allowed (the session
// ending can't be refused).
func TestQuitAsks(t *testing.T) {
	prev := boot.QuitRequested
	t.Cleanup(func() { onMain(func() { boot.QuitRequested = prev }) })
	var asked []bool
	cancel := true
	onMain(func() {
		boot.QuitRequested = func(ending bool) bool {
			asked = append(asked, ending)
			return cancel && !ending
		}
	})
	h, _ := mount(t, emptyRoot)
	onMain(func() {
		WindowClose(h.main.window)
		if h.wins[h.main.window] == nil || h.quitting {
			fatalf(t, "a canceled quit closed the last window")
		}
		h.requestQuit()
		if h.quitting {
			fatalf(t, "a canceled app.Quit quit")
		}
		if !h.askQuit(true) || !h.quitAllowed {
			fatalf(t, "the session ending was refused")
		}
		cancel = false
		h.askQuit(false) // allowed already: not asked again
		if fmt.Sprint(asked) != "[false false true]" {
			fatalf(t, "asked %v", asked)
		}
	})
}

// asuka/sdk/recent: the app's recent files, newest first, those that exist;
// clearing takes the app off them, the user's other recent files stay.
func TestRecentModule(t *testing.T) {
	dir := t.TempDir()
	xbel := filepath.Join(dir, "share", "recently-used.xbel")
	prev := recentFile
	recentFile = func() string { return xbel }
	t.Cleanup(func() { recentFile = prev })
	h := &Host{appName: "Recent Test"}
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b b.txt")
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	call := func(name string, args string) string {
		var out string
		onMain(func() {
			h.recentModule()[name]([]byte(args), func(r []byte, err error) {
				if err != nil {
					fatalf(t, "%s: %v", name, err)
				}
				out = string(r)
			})
		})
		return out
	}
	call("add", a)
	time.Sleep(20 * time.Millisecond)
	call("add", b)
	call("add", filepath.Join(dir, "gone.md"))
	want := fmt.Sprintf("[%q,%q]", b, a)
	if got := call("list", ""); got != want {
		t.Fatalf("list = %s, want %s", got, want)
	}
	time.Sleep(20 * time.Millisecond)
	call("add", a) // again: first
	if got := call("list", ""); got != fmt.Sprintf("[%q,%q]", a, b) {
		t.Fatalf("list after adding a again = %s", got)
	}
	// Another app recorded a too.
	onMain(func() {
		bm, err := LoadBookmarks(xbel)
		if err != nil {
			fatalf(t, "%v", err)
		}
		bm.Add(fileurl.FromPath(a), "text/markdown", "Other", "other %u")
		bm.Save(xbel)
		bm.Free()
	})
	call("clear", "")
	if got := call("list", ""); got != "[]" {
		t.Fatalf("list after clear = %s", got)
	}
	onMain(func() {
		bm, err := LoadBookmarks(xbel)
		if err != nil {
			fatalf(t, "%v", err)
		}
		defer bm.Free()
		if us := bm.URIs(); len(us) != 1 || us[0] != fileurl.FromPath(a) || fmt.Sprint(bm.Applications(us[0])) != "[Other]" {
			fatalf(t, "after clear: %q", us)
		}
	})
	if b, _ := os.ReadFile(xbel); !strings.Contains(string(b), "xbel") {
		t.Fatalf("not an XBEL file:\n%s", b)
	}
}

// asuka/sdk/login: an XDG autostart entry, which the user can turn off.
func TestLoginModule(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	prev := inFlatpak
	inFlatpak = func() bool { return false }
	t.Cleanup(func() { inFlatpak = prev })
	h := &Host{appName: "Login Test", appID: "dev.asuka.LoginTest"}
	call := func(name string, args ...byte) []byte {
		var out []byte
		h.loginModule()[name](args, func(r []byte, err error) {
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out = r
		})
		return out
	}
	path := filepath.Join(cfg, "autostart", "dev.asuka.LoginTest.desktop")
	if call("enabled")[0] != 0 {
		t.Fatal("enabled before set")
	}
	call("set", 1)
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "Exec=") || !strings.Contains(string(b), boot.LoginArg) || !strings.Contains(string(b), "Name=Login Test\n") {
		t.Fatalf("autostart entry %q, %v", b, err)
	}
	if call("enabled")[0] != 1 {
		t.Fatal("not enabled after set")
	}
	os.WriteFile(path, append(b, "X-GNOME-Autostart-enabled=false\n"...), 0o644)
	if call("enabled")[0] != 0 {
		t.Fatal("enabled though turned off in the settings")
	}
	call("set", 0)
	if _, err := os.Stat(path); !os.IsNotExist(err) || call("enabled")[0] != 0 {
		t.Fatalf("the entry stays: %v", err)
	}
	if call("openedAtLogin")[0] != 0 {
		t.Fatal("opened at login")
	}
}

func TestDesktopExec(t *testing.T) {
	got := desktopValue(desktopExec([]string{"/home/a b/My $App", "--asuka-opened-at-login"}))
	if want := `"/home/a b/My \\$App" --asuka-opened-at-login`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := desktopExec([]string{"/opt/notes/bin/notes", "100%"}); got != `/opt/notes/bin/notes "100%%"` {
		t.Fatalf("got %s", got)
	}
}

// The child of TestSingleInstance: the app with an ID, run as itself,
// which logs what it's opened with and quits after its second launch.
func init() {
	log := os.Getenv("ASUKA_LINUX_INSTANCE_CHILD")
	if log == "" {
		return
	}
	write := func(s string) {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			f.WriteString(s + "\n")
			f.Close()
		}
	}
	boot.ID = "dev.asuka.InstanceTest"
	boot.SecondInstance = func(args []string, dir string) { write(fmt.Sprint("second ", args, " in ", dir)) }
	opened := 0
	Run(func() ui.Node {
		ui.OnOpenFiles(func(ps []string) {
			write(fmt.Sprint("files ", ps))
			opened++
			if opened == 2 {
				boot.Quit()
			}
		})
		return ui.StaticText("instance")
	})
}

// A unique app gets its own command line's files, then another launch's,
// which hands them over and ends.
func TestSingleInstance(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	os.WriteFile(a, nil, 0o644)
	os.WriteFile(b, nil, 0o644)
	env := []string{"ASUKA_LINUX_INSTANCE_CHILD=" + log, "ASUKA_DATA=" + filepath.Join(dir, "data")}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ASUKA_STATE=") && !strings.HasPrefix(kv, "ASUKA_SNAPSHOT=") {
			env = append(env, kv)
		}
	}
	first := exec.Command(os.Args[0], "-test.run=^$", a)
	first.Env = env
	var out1 strings.Builder
	first.Stdout, first.Stderr = &out1, &out1
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	read := func() string { s, _ := os.ReadFile(log); return string(s) }
	waitFor := func(what string) {
		t.Helper()
		for i := 0; i < 1000 && !strings.Contains(read(), what); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		if !strings.Contains(read(), what) {
			first.Process.Kill()
			t.Fatalf("never logged %q:\n%s\nits output:\n%s", what, read(), out1.String())
		}
	}
	waitFor(fmt.Sprint("files ", []string{a}))
	second := exec.Command(os.Args[0], "-test.run=^$", "--new-window", "b.md")
	second.Dir = dir
	second.Env = env
	if out, err := second.CombinedOutput(); err != nil {
		first.Process.Kill()
		t.Fatalf("the second launch: %v\n%s", err, out)
	}
	waitFor(fmt.Sprint("files ", []string{b}))
	done := make(chan error, 1)
	go func() { done <- first.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the first instance: %v\n%s", err, out1.String())
		}
	case <-time.After(10 * time.Second):
		first.Process.Kill()
		t.Fatalf("app.Quit didn't quit:\n%s", out1.String())
	}
	want := fmt.Sprintf("files [%s]\nsecond [-test.run=^$ --new-window b.md] in %s\nfiles [%s]\n", a, dir, b)
	if got := read(); got != want {
		t.Fatalf("log:\n%s\nwant:\n%s", got, want)
	}
}
