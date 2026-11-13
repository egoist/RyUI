package dialog_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/asuka/dialog"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ui"
)

// sent is a dialog call's arguments, as the hosts read them.
type sent struct {
	Title, Button, Start, Name string
	Filters                    []struct {
		Name       string
		Types      []string
		Extensions []string
	}
	Multiple, Hidden bool
	View             uint32
}

// mountWith mounts a button on a fake host whose dialog module answers
// with answer, and records each call's method.
func mountWith(t *testing.T, answer func(method string, a sent) ([]byte, error)) (*fake.Host, *ui.Root, *ui.Node, *[]string) {
	t.Helper()
	h := fake.New()
	var methods []string
	h.Modules = map[string]func(string, []byte) ([]byte, error){"dialog": func(m string, b []byte) ([]byte, error) {
		var a sent
		if err := json.Unmarshal(b, &a); err != nil {
			t.Fatalf("%s: %v (%q)", m, err, b)
		}
		methods = append(methods, m)
		return answer(m, a)
	}}
	button := new(ui.Node)
	r := ui.Mount(h, func() ui.Node {
		*button = ui.Button(ui.ButtonProps{Title: ui.Val("Open")})
		return *button
	})
	t.Cleanup(r.Dispose)
	return h, r, button, &methods
}

// Open sends the options (filters with names, extensions without dots,
// the view whose window it belongs to) and reads the paths; nothing is a
// cancel.
func TestOpen(t *testing.T) {
	var got sent
	reply := `["/Users/ada/a.png","/Users/ada/b.md"]`
	h, r, button, _ := mountWith(t, func(m string, a sent) ([]byte, error) {
		got = a
		return []byte(reply), nil
	})
	var paths []string
	calls := 0
	dialog.Open(dialog.OpenOptions{
		Title: "Import", Button: "Import", Start: "/Users/ada",
		Filters: []dialog.Filter{
			{Name: "Images", Types: []string{"image/*"}},
			{Types: []string{"Text/Markdown"}, Extensions: []string{".md", "*.markdown"}},
			{}, // nothing in it: dropped
		},
		Multiple: true, ShowHidden: true, From: *button,
	}, func(p []string, err error) {
		if err != nil {
			t.Fatal(err)
		}
		calls++
		paths = p
	})
	r.Flush()
	h.RunPosted()
	if calls != 1 || !reflect.DeepEqual(paths, []string{"/Users/ada/a.png", "/Users/ada/b.md"}) {
		t.Fatalf("%d answers, paths %q", calls, paths)
	}
	if got.Title != "Import" || got.Button != "Import" || got.Start != "/Users/ada" || !got.Multiple || !got.Hidden ||
		got.View == 0 || got.View != ui.ViewID(*button) {
		t.Fatalf("sent %+v (button %d)", got, ui.ViewID(*button))
	}
	if len(got.Filters) != 2 || got.Filters[0].Name != "Images" || !reflect.DeepEqual(got.Filters[0].Types, []string{"image/*"}) ||
		got.Filters[1].Name != "text/markdown, *.md, *.markdown" || !reflect.DeepEqual(got.Filters[1].Extensions, []string{"md", "markdown"}) {
		t.Fatalf("filters %+v", got.Filters)
	}

	for _, cancel := range []string{"", "[]"} {
		reply = cancel
		paths = []string{"x"}
		got = sent{}
		dialog.Open(dialog.OpenOptions{}, func(p []string, err error) {
			if err != nil {
				t.Fatal(err)
			}
			paths = p
		})
		r.Flush()
		h.RunPosted()
		if paths != nil || got.View != 0 || got.Filters != nil {
			t.Fatalf("cancelled (%q): paths %q, sent %+v", cancel, paths, got)
		}
	}
}

// Save sends the suggested name and the filters, and reads the path;
// nothing is a cancel. A host that hasn't the dialog says "unsupported".
func TestSave(t *testing.T) {
	var got sent
	var reply []byte
	var replyErr error
	h, r, _, methods := mountWith(t, func(m string, a sent) ([]byte, error) {
		got = a
		return reply, replyErr
	})
	save := func(o dialog.SaveOptions) (string, error) {
		var path string
		var err error
		calls := 0
		dialog.Save(o, func(p string, e error) { path, err = p, e; calls++ })
		r.Flush()
		h.RunPosted()
		if calls != 1 {
			t.Fatalf("%d answers", calls)
		}
		return path, err
	}
	reply = []byte("/Users/ada/Notes.md")
	path, err := save(dialog.SaveOptions{Name: "Notes.md", Start: "/Users/ada", Filters: []dialog.Filter{{Name: "Markdown", Extensions: []string{"md"}}}})
	if err != nil || path != "/Users/ada/Notes.md" || got.Name != "Notes.md" || got.Start != "/Users/ada" ||
		len(got.Filters) != 1 || got.Filters[0].Extensions[0] != "md" || (*methods)[0] != "save" {
		t.Fatalf("saved %q, %v; sent %+v (%v)", path, err, got, *methods)
	}
	reply = nil
	if path, err := save(dialog.SaveOptions{}); path != "" || err != nil {
		t.Fatalf("cancelled: %q, %v", path, err)
	}
	replyErr = errors.New("unsupported")
	if _, err := save(dialog.SaveOptions{}); !errors.Is(err, dialog.ErrUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
	replyErr = errors.New("the dialog failed")
	if _, err := save(dialog.SaveOptions{}); err == nil || err.Error() != "the dialog failed" {
		t.Fatalf("an error: %v", err)
	}
}

// Folder asks for folders, one or several.
func TestFolder(t *testing.T) {
	var got sent
	h, r, _, methods := mountWith(t, func(m string, a sent) ([]byte, error) {
		got = a
		return []byte(`["/Users/ada/Photos","/Users/ada/Music"]`), nil
	})
	var paths []string
	dialog.Folder(dialog.FolderOptions{Multiple: true, Button: "Add"}, func(p []string, err error) { paths = p })
	r.Flush()
	h.RunPosted()
	if (*methods)[0] != "folder" || !got.Multiple || got.Button != "Add" || len(paths) != 2 || paths[1] != "/Users/ada/Music" {
		t.Fatalf("%v sent %+v, paths %q", *methods, got, paths)
	}
}

// A phone's host has no dialog module: Open is the document picker
// (device.PickFiles), its filters as MIME types, the copies' paths;
// Save and Folder are ErrUnsupported.
func TestOnPhones(t *testing.T) {
	h := fake.New()
	var picked []byte
	h.Modules = map[string]func(string, []byte) ([]byte, error){
		"dialog": func(m string, _ []byte) ([]byte, error) { return nil, errors.New("no module method dialog." + m) },
		"files": func(m string, a []byte) ([]byte, error) {
			picked = a
			return []byte(`[{"path":"/caches/asuka-files/1/a.pdf","name":"a.pdf","type":"application/pdf","size":3}]`), nil
		},
	}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	settle := func() {
		for range 2 { // the dialog's answer, then the picker's
			r.Flush()
			h.RunPosted()
		}
	}
	var paths []string
	dialog.Open(dialog.OpenOptions{Multiple: true, Filters: []dialog.Filter{{Types: []string{"application/pdf"}, Extensions: []string{"png"}}}},
		func(p []string, err error) {
			if err != nil {
				t.Fatal(err)
			}
			paths = p
		})
	settle()
	if string(picked) != "\x01application/pdf\nimage/png" || len(paths) != 1 || paths[0] != "/caches/asuka-files/1/a.pdf" {
		t.Fatalf("picked with %q: %q", picked, paths)
	}
	// An extension the phone can't name: any file.
	dialog.Open(dialog.OpenOptions{Filters: []dialog.Filter{{Types: []string{"image/*"}, Extensions: []string{"nosuchextension"}}}}, func([]string, error) {})
	settle()
	if string(picked) != "\x00" {
		t.Fatalf("picked with %q", picked)
	}

	var saveErr, folderErr error
	dialog.Save(dialog.SaveOptions{}, func(_ string, err error) { saveErr = err })
	dialog.Folder(dialog.FolderOptions{}, func(_ []string, err error) { folderErr = err })
	settle()
	if !errors.Is(saveErr, dialog.ErrUnsupported) || !errors.Is(folderErr, dialog.ErrUnsupported) {
		t.Fatalf("save: %v, folder: %v", saveErr, folderErr)
	}
}
