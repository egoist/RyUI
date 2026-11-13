//go:build windows

package windows

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/ryui/ui"
)

// await calls an async module's method and runs the thread's messages
// until it answers (a download, WebView2, the posted answer).
func await(h *Host, module, m string, args []byte) ([]byte, error, bool) {
	var out []byte
	var err error
	answered := false
	h.asyncModule(module)[m](args, func(b []byte, e error) { out, err, answered = b, e, true })
	pumpUntil(func() bool { return answered })
	return out, err, answered
}

func TestDocuments(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		missing, _ := json.Marshal(map[string]any{"files": []map[string]string{{"source": `C:\nonexistent\a.pdf`}}})
		if _, err, _ := await(h, "preview", "show", missing); err == nil || !strings.Contains(err.Error(), "nonexistent") {
			fatalf("preview of a missing file: %v", err)
		}
		if _, err, _ := await(h, "compose", "message", nil); err == nil || err.Error() != "unavailable" {
			fatalf("compose.message: %v", err)
		}
		if b, _, _ := await(h, "compose", "can", nil); len(b) != 4 || b[1] != 0 || b[2] != 0 {
			fatalf("compose.can: %v", b)
		}
		args, _ := json.Marshal(map[string]any{"html": "x", "width": 100, "height": 100, "left": 60, "right": 60})
		if _, err, _ := await(h, "print", "pdf", args); err == nil {
			fatalf("print.pdf without room: no error")
		}
		args, _ = json.Marshal(map[string]any{"html": "<title>Report</title><h1>Report</h1>" + strings.Repeat("<p>A line of a report long enough for pages.</p>", 120), "title": "Report",
			"width": 595.28, "height": 841.89, "top": 36, "right": 36, "bottom": 36, "left": 36})
		b, err, answered := await(h, "print", "pdf", args)
		if err != nil && (err.Error() == "unavailable" || webEnvErr != nil) {
			return // no WebView2 runtime
		}
		if !answered || err != nil {
			fatalf("print.pdf: %v (answered %v)", err, answered)
		}
		defer os.RemoveAll(filepath.Dir(string(b)))
		if filepath.Base(string(b)) != "Report.pdf" {
			fatalf("the PDF is %s, want it named after the page's title", b)
		}
		data, err := os.ReadFile(string(b))
		if err != nil || !strings.HasPrefix(string(data), "%PDF") {
			fatalf("print.pdf wrote %q: %v, %d bytes", b, err, len(data))
		}
	})
}

// The modules: secureStore keeps a value in the Credential Manager and
// deletes it; device.info says Windows; ryui/js runs a script (its value,
// a promise's, what it threw) in a WebView2 of its own.
func TestModules(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		sync := func(module, m string, args []byte) ([]byte, error) { return h.module(module)(m, args) }
		key := fmt.Sprintf("ryui-test-%d", time.Now().UnixNano())
		if b, err := sync("secureStore", "get", []byte(key)); err != nil || len(b) != 1 || b[0] != 0 {
			fatalf("a missing key: %v, %v", b, err)
		}
		args := binary.LittleEndian.AppendUint32(nil, uint32(len(key)))
		args = append(append(args, key...), "secret"...)
		if _, err := sync("secureStore", "set", args); err != nil {
			fatalf("set: %v", err)
		}
		if b, err := sync("secureStore", "get", []byte(key)); err != nil || string(b) != "\x01secret" {
			fatalf("get after set: %q, %v", b, err)
		}
		if _, err := sync("secureStore", "delete", []byte(key)); err != nil {
			fatalf("delete: %v", err)
		}
		if b, _ := sync("secureStore", "get", []byte(key)); len(b) != 1 || b[0] != 0 {
			fatalf("after delete: %q", b)
		}
		if b, err := sync("device", "info", nil); err != nil || !strings.Contains(string(b), `"system":"Windows"`) {
			fatalf("device.info: %s, %v", b, err)
		}
		run := func(src string) ([]byte, error) {
			b, err, _ := await(h, "js", "eval", append(binary.LittleEndian.AppendUint32(nil, 20000), src...))
			return b, err
		}
		if b, err := run("1 + 2"); webEnvErr == nil && (err != nil || string(b) != "3") {
			fatalf("1 + 2: %s, %v", b, err)
		}
		if webEnvErr != nil {
			return // no WebView2 runtime
		}
		if b, err := run("Promise.resolve({a: [1, 'x']})"); err != nil || string(b) != `{"a":[1,"x"]}` {
			fatalf("a promise: %s, %v", b, err)
		}
		if _, err := run("null.x"); err == nil || !strings.Contains(err.Error(), "TypeError") {
			fatalf("a throw: %v", err)
		}
	})
}

// fileFilters: a family's extensions, a MIME type's, any file.
func TestFileFilters(t *testing.T) {
	if f := fileFilters(nil); f != nil {
		t.Errorf("no types: %v", f)
	}
	if f := fileFilters([]string{"image/*", "application/pdf"}); len(f) != 2 || !strings.Contains(f[0].Patterns, "*.png") || !strings.Contains(f[0].Patterns, "*.pdf") {
		t.Errorf("image/*, application/pdf: %v", f)
	}
	if f := fileFilters([]string{"*/*"}); f != nil {
		t.Errorf("*/*: %v", f)
	}
}
