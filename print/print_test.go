package print

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ui"
)

// File, HTML and WebView send what to print and the dialog's options, and
// read the hosts' results and errors.
func TestPrint(t *testing.T) {
	h := fake.New()
	var method string
	var got map[string]any
	reply := []byte{1}
	var fail error
	h.Modules = map[string]func(string, []byte) ([]byte, error){"print": func(m string, a []byte) ([]byte, error) {
		method = m
		got = nil
		json.Unmarshal(a, &got)
		return reply, fail
	}}
	var web ui.Node
	r := ui.Mount(h, func() ui.Node {
		web = ui.WebView(ui.WebViewProps{HTML: ui.Val("<p>hi</p>")})
		return web
	})
	defer r.Dispose()
	run := func(f func(done func(Result, error))) (Result, error) {
		var res Result
		var err error
		calls := 0
		f(func(r Result, e error) { res, err = r, e; calls++ })
		r.Flush()
		h.RunPosted()
		if calls != 1 {
			t.Fatalf("done called %d times", calls)
		}
		return res, err
	}

	res, err := run(func(d func(Result, error)) { File("/tmp/a.pdf", Options{Job: "Report", Orientation: Landscape}, d) })
	if res != Printed || err != nil || method != "file" || got["source"] != "/tmp/a.pdf" || got["type"] != "application/pdf" ||
		got["job"] != "Report" || got["landscape"] != true || got["from"] != 0.0 {
		t.Fatalf("file: %v %v %s %v", res, err, method, got)
	}
	reply = []byte{0}
	res, err = run(func(d func(Result, error)) { HTML("<h1>Hi</h1>", Options{From: web}, d) })
	if res != Cancelled || err != nil || method != "html" || got["html"] != "<h1>Hi</h1>" || got["landscape"] != false ||
		got["from"] != float64(ui.ViewID(web)) {
		t.Fatalf("html: %v %v %s %v", res, err, method, got)
	}
	reply = []byte{2}
	res, err = run(func(d func(Result, error)) { WebView(web, Options{}, d) })
	if res != Unknown || err != nil || method != "webView" || got["view"] != float64(ui.ViewID(web)) || ui.ViewID(web) == 0 {
		t.Fatalf("webView: %v %v %s %v", res, err, method, got)
	}

	// Not a WebView on screen: an error, without a call.
	method = ""
	_, err = run(func(d func(Result, error)) { WebView(ui.Node{}, Options{}, d) })
	if err == nil || method != "" {
		t.Fatalf("no WebView: %v, called %q", err, method)
	}

	// The hosts' errors.
	for e, want := range map[string]error{"unsupported": ErrUnsupported, "unavailable": ErrUnavailable} {
		fail = errors.New(e)
		if _, err := run(func(d func(Result, error)) { File("a.docx", Options{}, d) }); !errors.Is(err, want) {
			t.Fatalf("%s: %v", e, err)
		}
	}
}

// PDF's page: the region's paper, turned for landscape; margins default
// to half an inch.
func TestEncodePDF(t *testing.T) {
	read := func(b []byte) map[string]float64 {
		var m map[string]any
		json.Unmarshal(b, &m)
		out := map[string]float64{}
		for k, v := range m {
			if f, ok := v.(float64); ok {
				out[k] = f
			}
		}
		return out
	}
	us := read(encodePDF("x", PDFOptions{}, "en-US-u-fw-sun"))
	if us["width"] != 612 || us["height"] != 792 || us["top"] != 36 || us["left"] != 36 {
		t.Fatalf("en-US: %v", us)
	}
	if de := read(encodePDF("x", PDFOptions{}, "de-DE")); de["width"] != A4.Width {
		t.Fatalf("de-DE: %v", de)
	}
	if en := read(encodePDF("x", PDFOptions{}, "en")); en["width"] != A4.Width {
		t.Fatalf("en: %v", en)
	}
	if ca := read(encodePDF("x", PDFOptions{}, "fr-CA")); ca["width"] != 612 {
		t.Fatalf("fr-CA: %v", ca)
	}
	l := read(encodePDF("x", PDFOptions{Page: A5, Landscape: true, Margins: &Margins{Top: 10, Right: -4}}, "en-US"))
	if l["width"] != A5.Height || l["height"] != A5.Width || l["top"] != 10 || l["right"] != 0 || l["bottom"] != 0 {
		t.Fatalf("landscape A5: %v", l)
	}
	if w := read(encodePDF("x", PDFOptions{Page: Page{800, 400}, Margins: Uniform(0)}, "en-US")); w["width"] != 800 || w["top"] != 0 {
		t.Fatalf("a wide page: %v", w)
	}
}

// PDF waits for the host's path.
func TestPDF(t *testing.T) {
	h := fake.New()
	var got []byte
	h.Modules = map[string]func(string, []byte) ([]byte, error){"print": func(m string, a []byte) ([]byte, error) {
		got = a
		return []byte("/caches/ryui-print/x.pdf"), nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	type res struct {
		path string
		err  error
	}
	done := make(chan res, 1)
	go func() {
		p, err := PDF("<p>hi</p>", PDFOptions{Page: Letter})
		done <- res{p, err}
	}()
	for range 5000 { // a safety net, not what passes
		h.RunPosted()
		r.Flush()
		select {
		case v := <-done:
			if v.err != nil || v.path != "/caches/ryui-print/x.pdf" || len(got) == 0 {
				t.Fatalf("%v %q", v.err, v.path)
			}
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("no answer")
}

func TestTitle(t *testing.T) {
	for markup, want := range map[string]string{
		"<html><head><TITLE>Q3 &amp; more\n report</TITLE></head>": "Q3 & more report",
		`<title lang="en">Receipt</title>`:                         "Receipt",
		"<h1>No title</h1>":                                        "",
		"<title>Unclosed":                                          "",
	} {
		if got := title(markup); got != want {
			t.Errorf("%q: %q, want %q", markup, got, want)
		}
	}
}
