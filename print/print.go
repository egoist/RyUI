// Package print prints with the system's print dialog, and makes PDFs
// from HTML: a file (a PDF, an image), HTML markup, or what a WebView
// shows. The dialog has the platform's printers and options (copies,
// pages, orientation, paper, a PDF); the app gives what to print.
//
// iOS shows its print sheet (UIPrintInteractionController), a popover
// on the iPad; Android its print screen (PrintManager), whose "Save as
// PDF" is a printer too; the Mac its print panel (NSPrintOperation), a
// sheet on the window; Windows its print dialog, then WebView2 prints
// HTML and WebViews, and a file's app prints a file.
package print

import (
	"encoding/json"
	"errors"
	"html"
	"mime"
	"path/filepath"
	"strings"

	"github.com/egoist/ryui/ui"
)

// Result is what the user did in the print dialog.
type Result uint8

const (
	// Cancelled: they closed the dialog without printing.
	Cancelled Result = iota
	// Printed: the job went to the printer's queue (or was saved as a
	// PDF from the dialog). Whether the printer printed it, the
	// platforms don't say.
	Printed
	// Unknown: the job was handed to another app, which doesn't say
	// (Windows's files, printed by their app).
	Unknown
)

func (r Result) String() string {
	switch r {
	case Printed:
		return "printed"
	case Unknown:
		return "unknown"
	}
	return "cancelled"
}

// ErrUnavailable: the device can't print (an iOS device with printing
// off by its profile).
var ErrUnavailable = errors.New("print: printing isn't available")

// ErrUnsupported: the file isn't of a type the platform prints (File
// prints PDFs and images; on Windows, what the file's app prints).
var ErrUnsupported = errors.New("print: can't print this file")

// Orientation is the orientation the dialog starts with; the user can
// change it.
type Orientation uint8

const (
	// Portrait, unless the platform picks: Android and iOS turn an image
	// wider than tall.
	Portrait Orientation = iota
	Landscape
)

// Options say how a print dialog starts.
type Options struct {
	// Job is the job's name in the print queue and the dialog. Default:
	// the file's name for File, the page's title for HTML and WebView,
	// else the app's name.
	Job string
	// Orientation is the orientation the dialog starts with.
	Orientation Orientation
	// From is the view the dialog points at where it's a popover (the
	// iPad): the button that prints. Default: the screen's middle.
	From ui.Node
}

// File prints the file at source: a path, an asset's (as ui.Image takes
// it), or an http(s) URL, which the host downloads first. PDFs and
// images print everywhere; on Windows the file's app prints it (the
// shell's "print" verb), whatever its type, and the result is Unknown.
// done (if not nil) gets the result on the main thread once the dialog
// is closed. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func File(source string, o Options, done func(Result, error)) {
	t, _, _ := strings.Cut(mime.TypeByExtension(filepath.Ext(source)), ";")
	call("file", map[string]any{"source": ui.AssetURL(source), "type": t}, o, done)
}

// HTML prints markup, laid out for the paper the user picks, page after
// page, as a browser prints a page (CSS's @media print and break-before
// apply). Its images and styles come from absolute URLs (https:, data:).
// done (if not nil) gets the result on the main thread once the dialog
// is closed. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func HTML(html string, o Options, done func(Result, error)) {
	call("html", map[string]any{"html": html}, o, done)
}

// WebView prints the page a ui.WebView shows, as its browser would. done
// (if not nil) gets the result on the main thread once the dialog is
// closed, or an error if web isn't a WebView on screen. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func WebView(web ui.Node, o Options, done func(Result, error)) {
	id := ui.ViewID(web)
	if id == 0 {
		if done != nil {
			done(Cancelled, errors.New("print: not a WebView on screen"))
		}
		return
	}
	call("webView", map[string]any{"view": id}, o, done)
}

func call(method string, args map[string]any, o Options, done func(Result, error)) {
	args["job"] = o.Job
	args["landscape"] = o.Orientation == Landscape
	args["from"] = ui.ViewID(o.From)
	// The region's paper, for a dialog that starts with a size (Android's,
	// to start in landscape).
	args["letter"] = regionPage(ui.FormatLocale()) == Letter
	b, _ := json.Marshal(args)
	ui.Call("print", method, b, func(r []byte, err error) {
		if done == nil {
			return
		}
		if err != nil {
			done(Cancelled, mapErr(err))
			return
		}
		res := Cancelled
		if len(r) > 0 && r[0] <= byte(Unknown) {
			res = Result(r[0])
		}
		done(res, nil)
	})
}

func mapErr(err error) error {
	switch err.Error() {
	case "unavailable":
		return ErrUnavailable
	case "unsupported":
		return ErrUnsupported
	}
	return err
}

// Page is a sheet of paper's size, in points (1/72 inch).
type Page struct {
	Width, Height float64
}

// The usual sizes of paper.
var (
	A3     = Page{841.89, 1190.55}
	A4     = Page{595.28, 841.89}
	A5     = Page{419.53, 595.28}
	Letter = Page{612, 792}
	Legal  = Page{612, 1008}
)

// Margins are the space around a page's content, in points.
type Margins struct {
	Top, Right, Bottom, Left float64
}

// Uniform is margins of pt on each side.
func Uniform(pt float64) *Margins { return &Margins{pt, pt, pt, pt} }

// PDFOptions say how PDF lays out the pages.
type PDFOptions struct {
	// Page is the paper's size. Default: the user's region's (Letter in
	// the Americas that use it, A4 elsewhere), from CLDR's data.
	Page Page
	// Landscape turns the page: its width is the longer side.
	Landscape bool
	// Margins are the space around each page's content. Default: half an
	// inch (36 points) on each side; Uniform(0) for none.
	Margins *Margins
}

// PDF lays out markup on pages, as HTML prints it (without a dialog),
// and writes them as a PDF file in the app's caches, whose path it
// returns: for ui.ShareFiles, preview.Show, compose.Mail's attachments,
// or the app to move. Not on the main thread.
//
// iOS and the Mac lay it out with WebKit, Android with its WebView,
// Windows with WebView2: the text is text in the PDF, searchable and
// selectable.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func PDF(html string, o PDFOptions) (path string, err error) {
	b, err := ui.CallWait("print", "pdf", encodePDF(html, o, ui.FormatLocale()))
	if err != nil {
		return "", mapErr(err)
	}
	return string(b), nil
}

func encodePDF(markup string, o PDFOptions, locale string) []byte {
	p := o.Page
	if p.Width <= 0 || p.Height <= 0 {
		p = regionPage(locale)
	}
	if o.Landscape && p.Width < p.Height {
		p.Width, p.Height = p.Height, p.Width
	}
	m := Margins{36, 36, 36, 36}
	if o.Margins != nil {
		m = *o.Margins
	}
	pos := func(f float64) float64 { return max(f, 0) }
	b, _ := json.Marshal(map[string]any{
		"html": markup, "title": title(markup), "width": p.Width, "height": p.Height,
		"top": pos(m.Top), "right": pos(m.Right), "bottom": pos(m.Bottom), "left": pos(m.Left),
	})
	return b
}

// letterRegions use US Letter (CLDR's supplemental measurementData,
// paperSize); the rest of the world, A4.
var letterRegions = map[string]bool{
	"BZ": true, "CA": true, "CL": true, "CO": true, "CR": true, "GT": true, "MX": true,
	"NI": true, "PA": true, "PH": true, "PR": true, "SV": true, "US": true, "VE": true,
}

// regionPage is the paper of locale's region (a BCP 47 tag, as
// ui.FormatLocale has it).
func regionPage(locale string) Page {
	for i, sub := range strings.Split(locale, "-") {
		if i == 0 {
			continue
		}
		if len(sub) == 1 { // an extension: the region comes before
			break
		}
		if len(sub) == 2 && letterRegions[strings.ToUpper(sub)] {
			return Letter
		}
	}
	return A4
}

// title is markup's <title>, which names its PDF ("" if it has none).
func title(markup string) string {
	lower := strings.ToLower(markup)
	i := strings.Index(lower, "<title")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(lower[i:], '>')
	if j < 0 {
		return ""
	}
	start := i + j + 1
	end := strings.Index(lower[start:], "</title")
	if end < 0 {
		return ""
	}
	return strings.Join(strings.Fields(html.UnescapeString(markup[start:start+end])), " ")
}
