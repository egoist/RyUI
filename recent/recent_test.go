package recent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ui"
)

// Add records an absolute path, Clear and List ask the host's module.
func TestRecent(t *testing.T) {
	h := fake.New()
	var list []string
	h.Modules = map[string]func(string, []byte) ([]byte, error){
		"recent": func(method string, args []byte) ([]byte, error) {
			switch method {
			case "add":
				list = append([]string{string(args)}, list...)
			case "clear":
				list = nil
			case "list":
				return json.Marshal(list)
			}
			return nil, nil
		},
	}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	var errs []error
	Add("/notes/a.md", func(err error) { errs = append(errs, err) })
	Add("b.md", nil)
	var got []string
	List(func(paths []string, err error) { got, errs = paths, append(errs, err) })
	r.Flush()
	h.RunPosted()
	wd, _ := filepath.Abs(".")
	if fmt.Sprint(got) != fmt.Sprint([]string{filepath.Join(wd, "b.md"), "/notes/a.md"}) || fmt.Sprint(errs) != "[<nil> <nil>]" {
		t.Fatalf("List = %q, errors %v", got, errs)
	}
	Clear(nil)
	List(func(paths []string, err error) { got = paths })
	r.Flush()
	h.RunPosted()
	if len(got) != 0 {
		t.Fatalf("after Clear: %q", got)
	}
}
