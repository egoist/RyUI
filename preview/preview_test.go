package preview_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/preview"
	"github.com/egoist/asuka/ui"
)

// Show sends the files with their types, the start and the view it grows
// from; the hosts' "no viewer" is ErrNoViewer.
func TestShow(t *testing.T) {
	h := fake.New()
	var got []byte
	viewer := true
	h.Modules = map[string]func(string, []byte) ([]byte, error){"preview": func(m string, a []byte) ([]byte, error) {
		got = a
		if !viewer {
			return nil, errors.New("no viewer")
		}
		return nil, nil
	}}
	var button ui.Node
	r := ui.Mount(h, func() ui.Node {
		button = ui.Button(ui.ButtonProps{Title: ui.Val("Open")})
		return button
	})
	defer r.Dispose()

	calls := 0
	var gotErr error
	preview.Show([]string{"/tmp/a.pdf", "docs/b.txt", "https://example.com/c.jpg"}, preview.Options{Start: 2, From: button},
		func(err error) { calls++; gotErr = err })
	r.Flush()
	h.RunPosted()
	if calls != 1 || gotErr != nil {
		t.Fatalf("done %d times, %v", calls, gotErr)
	}
	var a struct {
		Files []struct{ Source, Type string }
		Start int
		From  uint32
	}
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Files) != 3 || a.Files[0].Source != "/tmp/a.pdf" || a.Files[0].Type != "application/pdf" ||
		a.Files[1].Type != "text/plain" || a.Files[2].Source != "https://example.com/c.jpg" || a.Files[2].Type != "image/jpeg" {
		t.Fatalf("files %+v", a.Files)
	}
	if a.Start != 2 || a.From == 0 || a.From != ui.ViewID(button) {
		t.Fatalf("start %d, from %d (button %d)", a.Start, a.From, ui.ViewID(button))
	}

	// A start out of range is the first; no viewer is ErrNoViewer.
	viewer = false
	preview.Show([]string{"/tmp/a.pdf"}, preview.Options{Start: 5}, func(err error) { gotErr = err })
	r.Flush()
	h.RunPosted()
	json.Unmarshal(got, &a)
	if a.Start != 0 || a.From != 0 || !errors.Is(gotErr, preview.ErrNoViewer) {
		t.Fatalf("start %d, from %d, %v", a.Start, a.From, gotErr)
	}

	// No files: an error, and no call.
	got = nil
	preview.Show(nil, preview.Options{}, func(err error) { gotErr = err })
	r.Flush()
	h.RunPosted()
	if got != nil || gotErr == nil {
		t.Fatalf("no files: called with %q, %v", got, gotErr)
	}
}
