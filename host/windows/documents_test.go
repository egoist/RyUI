//go:build windows && amd64

package windows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ui"
)

// await calls an async module's method and runs the thread's messages
// until it answers (a download, WebView2, the posted answer).
func await(h *Host, module, m string, args []byte) ([]byte, error, bool) {
	var out []byte
	var err error
	answered := false
	h.asyncModule(module)[m](args, func(b []byte, e error) { out, err, answered = b, e, true })
	var msg Msg
	for i := 0; !answered && i < 2_000_000; i++ { // a safety net, not what passes
		if PeekMessage(&msg) {
			TranslateMessage(&msg)
			DispatchMessage(&msg)
		}
	}
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
