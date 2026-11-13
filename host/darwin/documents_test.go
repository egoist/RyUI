//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// await calls m and runs the main run loop until it answers (the
// modules answer from the run loop: downloads, WebKit, print runs).
func await(h *Host, module, m string, args []byte) ([]byte, error) {
	var out []byte
	var err error
	answered := false
	h.module(module)[m](args, func(b []byte, e error) { out, err, answered = b, e, true })
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for i := 0; !answered && i < 20000; i++ { // a safety net, not what passes
		until := MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSinceNow:"), 0.01)
		if ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1); ev != 0 {
			Send(app, "sendEvent:", uintptr(ev))
		}
	}
	if !answered {
		return nil, errNoAnswer
	}
	return out, err
}

var errNoAnswer = os.ErrDeadlineExceeded

// print.pdf lays HTML out on pages of the size asked with WebKit, and
// writes a PDF; files that aren't there, or don't print, fail.
func TestDocuments(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		var body strings.Builder
		body.WriteString("<!doctype html><title>Report</title><h1>Report</h1>")
		for i := range 120 {
			body.WriteString("<p>Line " + string(rune('A'+i%26)) + " of a report long enough for a few pages.</p>")
		}
		args, _ := json.Marshal(map[string]any{"html": body.String(), "title": "Report", "width": 595.28, "height": 841.89,
			"top": 36, "right": 36, "bottom": 36, "left": 36})
		b, err := await(h, "print", "pdf", args)
		if err != nil {
			fatal(t, "print.pdf:", err)
		}
		path := string(b)
		defer os.RemoveAll(filepath.Dir(path))
		if filepath.Base(path) != "Report.pdf" {
			fatalf(t, "the PDF is %s, want it named after the page's title", path)
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.HasPrefix(string(data), "%PDF") {
			fatalf(t, "print.pdf wrote %s: %v, %.8q", path, err, data)
		}
		Dlopen("/System/Library/Frameworks/PDFKit.framework/PDFKit")
		doc := Send(Send(Class("PDFDocument"), "alloc"), "initWithURL:", uintptr(FileURL(path)))
		defer Release(doc)
		if n := SendInt(doc, "pageCount"); n < 2 {
			fatalf(t, "%d pages, want a few", n)
		}
		page := Send(doc, "pageAtIndex:", 0)
		// boundsForBox: kPDFDisplayBoxMediaBox (an int in, a rect out; the second int unused).
		r := MsgLineFragment(page, Sel("boundsForBox:"), 0, 0)
		if int(r.Size.Width+0.5) != 595 || int(r.Size.Height+0.5) != 842 {
			fatalf(t, "a page of %v, want A4", r.Size)
		}
		if text := GoString(Send(doc, "string")); !strings.Contains(text, "Report") {
			fatalf(t, "the PDF's text: %.80q", text)
		}

		// Margins wider than the page.
		args, _ = json.Marshal(map[string]any{"html": "x", "width": 100, "height": 100, "left": 60, "right": 60})
		if _, err := await(h, "print", "pdf", args); err == nil {
			fatal(t, "print.pdf without room: no error")
		}
		// A file that isn't there; one that isn't a PDF or an image.
		missing, _ := json.Marshal(map[string]any{"files": []map[string]string{{"source": "/nonexistent/a.pdf"}}})
		if _, err := await(h, "preview", "show", missing); err == nil || !strings.Contains(err.Error(), "/nonexistent/a.pdf") {
			fatalf(t, "preview of a missing file: %v", err)
		}
		txt := filepath.Join(t.TempDir(), "a.txt")
		os.WriteFile(txt, []byte("hi"), 0o644)
		args, _ = json.Marshal(map[string]any{"source": txt, "type": "text/plain"})
		if _, err := await(h, "print", "file", args); err == nil || err.Error() != "unsupported" {
			fatalf(t, "print.file of text: %v", err)
		}
		if b, err := await(h, "compose", "can", nil); err != nil || len(b) != 4 || b[1] != 0 {
			fatalf(t, "compose.can: %v, %v", b, err)
		}
		// CC with attachments: the share service has none.
		args, _ = json.Marshal(map[string]any{"to": []string{"a@example.com"}, "cc": []string{"b@example.com"},
			"attachments": []map[string]string{{"source": txt, "type": "text/plain", "name": "a.txt"}}})
		if _, err := await(h, "compose", "mail", args); err == nil || err.Error() != "unavailable" {
			fatalf(t, "compose.mail with CC and attachments: %v", err)
		}
	})
}
