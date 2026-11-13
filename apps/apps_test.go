package apps_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/egoist/asuka/apps"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ui"
)

func mount(t *testing.T) (*fake.Host, func()) {
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	return h, func() {
		r.Flush()
		h.RunPosted()
	}
}

// List asks the host's apps module, and sorts what it answers by name
// (case and accents aside), each app once.
func TestList(t *testing.T) {
	h, run := mount(t)
	var asked []string
	h.Modules = map[string]func(string, []byte) ([]byte, error){"apps": func(m string, a []byte) ([]byte, error) {
		asked = append(asked, m+":"+string(a))
		return json.Marshal([]map[string]string{
			{"name": "zed", "id": "dev.zed", "path": "/Applications/Zed.app"},
			{"name": "Éclair", "id": "e", "path": "/Applications/Eclair.app"},
			{"name": "Calculator", "id": "com.apple.calculator", "path": "/System/Applications/Calculator.app"},
			{"name": "calendar", "id": "cal", "path": "/Applications/calendar.app"},
			{"name": "Calculator", "id": "com.apple.calculator", "path": "/Users/ada/Applications/Calculator.app"}, // again: the first stays
			{"name": "Echo", "id": "", "path": "/Applications/Echo.app"},
			{"name": "Echo", "id": "", "path": "/Applications/More/Echo.app"}, // no ID: another app
			{"name": "", "id": "x", "path": "/Applications/x.app"},            // no name: left out
		})
	}}
	var got []apps.App
	var err error
	n := 0
	apps.List(func(l []apps.App, e error) { got, err = l, e; n++ })
	run()
	if n != 1 || err != nil {
		t.Fatalf("%d answers, %v", n, err)
	}
	want := []apps.App{
		{"Calculator", "com.apple.calculator", "/System/Applications/Calculator.app"},
		{"calendar", "cal", "/Applications/calendar.app"},
		{"Echo", "", "/Applications/Echo.app"},
		{"Echo", "", "/Applications/More/Echo.app"},
		{"Éclair", "e", "/Applications/Eclair.app"},
		{"zed", "dev.zed", "/Applications/Zed.app"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("List: %v\nwant %v", got, want)
	}
	if !slices.Equal(asked, []string{"list:"}) {
		t.Fatalf("asked %v", asked)
	}

	// A bad answer is an error, not apps.
	h.Modules["apps"] = func(string, []byte) ([]byte, error) { return []byte("{"), nil }
	apps.List(func(l []apps.App, e error) { got, err = l, e })
	run()
	if err == nil || got != nil {
		t.Fatalf("a bad answer: %v, %v", got, err)
	}
	// Nothing found is no apps.
	h.Modules["apps"] = func(string, []byte) ([]byte, error) { return []byte("[]"), nil }
	apps.List(func(l []apps.App, e error) { got, err = l, e })
	run()
	if err != nil || len(got) != 0 {
		t.Fatalf("none: %v, %v", got, err)
	}
	apps.List(nil) // done may be nil
	run()
}

// Icon sends the app's path and the size, and gives the file's path;
// Open sends the app's path.
func TestIconOpen(t *testing.T) {
	h, run := mount(t)
	var asked []string
	h.Modules = map[string]func(string, []byte) ([]byte, error){"apps": func(m string, a []byte) ([]byte, error) {
		asked = append(asked, m+":"+string(a))
		if m == "icon" {
			return []byte("/cache/asuka-apps/1.png"), nil
		}
		return nil, nil
	}}
	calc := apps.App{Name: "Calculator", ID: "com.apple.calculator", Path: "/System/Applications/Calculator.app"}
	var path string
	var err error
	apps.Icon(calc, 32, func(p string, e error) { path, err = p, e })
	run()
	if path != "/cache/asuka-apps/1.png" || err != nil {
		t.Fatalf("Icon: %q, %v", path, err)
	}
	opened := false
	apps.Open(calc, func(e error) { err = e; opened = true })
	run()
	if !opened || err != nil {
		t.Fatalf("Open: %v (answered %v)", err, opened)
	}
	want := []string{`icon:{"path":"/System/Applications/Calculator.app","size":32}`, "open:/System/Applications/Calculator.app"}
	if !slices.Equal(asked, want) {
		t.Fatalf("asked %q\nwant %q", asked, want)
	}

	// Without a path or a size: an error, the host not asked.
	asked = nil
	for _, c := range []struct {
		app  apps.App
		size float32
	}{{apps.App{Name: "x"}, 32}, {calc, 0}, {calc, -1}} {
		err = nil
		apps.Icon(c.app, c.size, func(_ string, e error) { err = e })
		if err == nil {
			t.Fatalf("Icon(%v, %v): no error", c.app, c.size)
		}
	}
	err = nil
	apps.Open(apps.App{Name: "x"}, func(e error) { err = e })
	run()
	if err == nil || len(asked) != 0 {
		t.Fatalf("Open without a path: %v (asked %v)", err, asked)
	}
	apps.Open(calc, nil) // done may be nil
	apps.Icon(calc, 16, nil)
	run()

	// The host's errors come through.
	h.Modules["apps"] = func(string, []byte) ([]byte, error) { return nil, errors.New("no app at /x") }
	apps.Open(calc, func(e error) { err = e })
	run()
	if err == nil || err.Error() != "no app at /x" {
		t.Fatalf("an error: %v", err)
	}
}

// A host without the module (phones, the web, host/fake without one)
// answers ErrUnsupported to each.
func TestUnsupported(t *testing.T) {
	h, run := mount(t)
	calc := apps.App{Name: "Calculator", Path: "/x.app"}
	check := func(what string) {
		t.Helper()
		var errs []error
		apps.List(func(l []apps.App, e error) {
			if l != nil {
				t.Errorf("%s: List gave %v", what, l)
			}
			errs = append(errs, e)
		})
		apps.Icon(calc, 16, func(_ string, e error) { errs = append(errs, e) })
		apps.Open(calc, func(e error) { errs = append(errs, e) })
		run()
		if len(errs) != 3 {
			t.Fatalf("%s: %d answers", what, len(errs))
		}
		for _, e := range errs {
			if !errors.Is(e, apps.ErrUnsupported) {
				t.Fatalf("%s: %v", what, e)
			}
		}
	}
	check("no module")
	for _, answer := range []string{
		"unsupported",
		"no module method apps.list",          // iOS, Android
		"asuka: apps.list isn't on the web",   // the web
		"asuka: apps.list isn't on macOS yet", // a desktop host without it
	} {
		h.Modules = map[string]func(string, []byte) ([]byte, error){"apps": func(m string, _ []byte) ([]byte, error) {
			return nil, errors.New(answer)
		}}
		check(answer)
	}
}
