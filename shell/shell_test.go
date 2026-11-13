package shell_test

import (
	"errors"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/shell"
	"github.com/egoist/asuka/ui"
)

// Each function sends its path to the host's shell module, and maps its
// answers: "unsupported" (or a phone's host, which has none), "no app".
func TestShell(t *testing.T) {
	h := fake.New()
	type call struct{ method, arg string }
	var calls []call
	h.Modules = map[string]func(string, []byte) ([]byte, error){"shell": func(m string, a []byte) ([]byte, error) {
		calls = append(calls, call{m, string(a)})
		return nil, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	run := func(f func(string, func(error)), path string) error {
		var err error
		n := 0
		f(path, func(e error) { err = e; n++ })
		r.Flush()
		h.RunPosted()
		if n != 1 {
			t.Fatalf("%d answers", n)
		}
		return err
	}
	for _, c := range []struct {
		f    func(string, func(error))
		path string
	}{{shell.Reveal, "/Users/ada/a.pdf"}, {shell.Open, "/Users/ada"}, {shell.Trash, "/Users/ada/old.txt"}} {
		if err := run(c.f, c.path); err != nil {
			t.Fatal(err)
		}
	}
	shell.Beep()
	r.Flush()
	h.RunPosted()
	want := []call{{"reveal", "/Users/ada/a.pdf"}, {"open", "/Users/ada"}, {"trash", "/Users/ada/old.txt"}, {"beep", ""}}
	if len(calls) != len(want) {
		t.Fatalf("calls %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls %v, want %v", calls, want)
		}
	}

	for answer, want := range map[string]error{
		"unsupported":                  shell.ErrUnsupported,
		"no module method shell.trash": shell.ErrUnsupported,
		"no app":                       shell.ErrNoApp,
	} {
		h.Modules["shell"] = func(string, []byte) ([]byte, error) { return nil, errors.New(answer) }
		if err := run(shell.Trash, "/x"); !errors.Is(err, want) {
			t.Fatalf("%q: %v", answer, err)
		}
	}
	h.Modules["shell"] = func(string, []byte) ([]byte, error) { return nil, errors.New("no file at /x") }
	if err := run(shell.Open, "/x"); err == nil || err.Error() != "no file at /x" {
		t.Fatalf("an error: %v", err)
	}
	// No path: answered without asking the host.
	asked := false
	h.Modules["shell"] = func(string, []byte) ([]byte, error) { asked = true; return nil, nil }
	if err := run(shell.Reveal, ""); err == nil || asked {
		t.Fatalf("no path: %v (asked %v)", err, asked)
	}
	// done may be nil.
	shell.Open("/x", nil)
	r.Flush()
	h.RunPosted()
}
