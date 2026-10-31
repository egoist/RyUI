package device_test

import (
	"testing"

	"github.com/egoist/ryui/device"
	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ui"
)

// PickFiles sends whether several may be picked and the types, and reads
// the host's JSON; none is a cancel.
func TestPickFiles(t *testing.T) {
	h := fake.New()
	var args []byte
	reply := `[{"path":"/c/1/a.pdf","name":"a.pdf","type":"application/pdf","size":12}]`
	h.Modules = map[string]func(string, []byte) ([]byte, error){"files": func(m string, a []byte) ([]byte, error) {
		args = a
		return []byte(reply), nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	var got []device.File
	device.PickFiles(device.FileOptions{Types: []string{"application/pdf", "image/*"}, Multiple: true}, func(f []device.File, err error) {
		if err != nil {
			t.Fatal(err)
		}
		got = f
	})
	r.Flush()
	h.RunPosted()
	if string(args) != "\x01application/pdf\nimage/*" {
		t.Fatalf("args %q", args)
	}
	if len(got) != 1 || got[0] != (device.File{Path: "/c/1/a.pdf", Name: "a.pdf", Type: "application/pdf", Size: 12}) {
		t.Fatalf("files %+v", got)
	}
	reply = ""
	got = []device.File{{}}
	device.PickFiles(device.FileOptions{}, func(f []device.File, err error) { got = f })
	r.Flush()
	h.RunPosted()
	if string(args) != "\x00" || got != nil {
		t.Fatalf("cancel: args %q, files %v", args, got)
	}
}
