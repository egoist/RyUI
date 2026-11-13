//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/internal/localfile"
	"github.com/egoist/ryui/ops"
)

// ryui/print and ryui/compose on Linux.
//
// Printing: a file goes to the print portal (its dialog, then the
// printer: what CUPS takes, PDFs and images among them), or, without the
// portal, to GTK's print dialog and a print job; markup and web views
// print with WebKit (its operation, GTK's dialog), and print.PDF lays the
// markup out with WebKit and prints it to a file. Answers: u8 0
// cancelled, 1 printed.
//
// Mail: the email portal (the user's mail app's composer, with the
// attachments), else xdg-email, else the mailto: link (no attachments).
// Answered 3, unknown: the mail app doesn't say. No text messages.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["print"] = map[string]method{
			"file":    func(args []byte, reply func([]byte, error)) { h.print("file", args, reply) },
			"html":    func(args []byte, reply func([]byte, error)) { h.print("html", args, reply) },
			"webView": func(args []byte, reply func([]byte, error)) { h.print("webView", args, reply) },
			"pdf":     func(args []byte, reply func([]byte, error)) { h.print("pdf", args, reply) },
		}
		m["compose"] = map[string]method{
			"can":     func(_ []byte, reply func([]byte, error)) { reply(h.composeCan(), nil) },
			"mail":    h.composeMail,
			"message": func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("unavailable")) },
		}
	})
}

type printArgs struct {
	Source    string  `json:"source"`
	Type      string  `json:"type"`
	HTML      string  `json:"html"`
	Title     string  `json:"title"`
	View      uint32  `json:"view"`
	Job       string  `json:"job"`
	Landscape bool    `json:"landscape"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	Top       float64 `json:"top"`
	Right     float64 `json:"right"`
	Bottom    float64 `json:"bottom"`
	Left      float64 `json:"left"`
}

func (h *Host) print(method string, args []byte, reply func([]byte, error)) {
	var a printArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("print."+method+": bad arguments"))
		return
	}
	job := func(fallback string) string {
		switch {
		case a.Job != "":
			return a.Job
		case fallback != "":
			return fallback
		}
		return h.appName
	}
	result := func(ok bool) {
		if ok {
			reply([]byte{1}, nil)
		} else {
			reply([]byte{0}, nil)
		}
	}
	switch method {
	case "file":
		h.localFiles([]modSource{{Source: a.Source, Type: a.Type}}, func(paths []string, err error) {
			if err != nil {
				reply(nil, err)
				return
			}
			h.printFile(paths[0], job(filepath.Base(paths[0])), a.Landscape, func(ok bool, err error) {
				if err != nil {
					reply(nil, err)
					return
				}
				result(ok)
			})
		})
	case "webView":
		n := h.nodes[ops.NodeID(a.View)]
		w, ok := n.extWeb()
		if !ok || w.view == 0 {
			reply(nil, errors.New("not a WebView"))
			return
		}
		h.afterCore(func() {
			ok, err := h.printView(w.view, a.Landscape, nil)
			if err != nil {
				reply(nil, err)
				return
			}
			result(ok)
		})
	case "html":
		h.loadPrintPage(a.HTML, func(v Ptr, err error) {
			if err != nil {
				reply(nil, err)
				return
			}
			ok, err := h.printView(v, a.Landscape, func() { ObjectUnref(v) })
			if err != nil {
				reply(nil, err)
				return
			}
			result(ok)
		})
	case "pdf":
		if a.Width <= 0 || a.Height <= 0 || a.Left+a.Right >= a.Width || a.Top+a.Bottom >= a.Height {
			reply(nil, errors.New("the margins leave no room on the page"))
			return
		}
		path, err := localfile.NewPDF(h.modCacheDir(), a.Title)
		if err != nil {
			reply(nil, err)
			return
		}
		h.loadPrintPage(a.HTML, func(v Ptr, err error) {
			if err != nil {
				reply(nil, err)
				return
			}
			h.printPDF(v, a, path, func(err error) {
				ObjectUnref(v)
				if err != nil {
					reply(nil, err)
					return
				}
				reply([]byte(path), nil)
			})
		})
	default:
		reply(nil, errors.New("ryui: print."+method+" isn't on Linux"))
	}
}

// loadPrintPage loads markup in a web view that's never shown.
func (h *Host) loadPrintPage(html string, done func(Ptr, error)) {
	if err := LoadWebKitPrint(); err != nil {
		done(0, errors.New("unsupported"))
		return
	}
	v := ObjectRefSink(WebKitWebViewNew())
	finished := false
	var hs []uint64
	end := func(err error) {
		if finished {
			return
		}
		finished = true
		for _, hd := range hs {
			Disconnect(v, hd)
		}
		if err != nil {
			ObjectUnref(v)
			done(0, err)
			return
		}
		done(v, nil)
	}
	hs = append(hs,
		Connect(v, "load-changed", func(ev uintptr) {
			if ev == WebKitLoadFinished {
				// Printing runs a dialog's loop: not inside the signal.
				IdleAdd(PriorityDefault, func() bool { end(nil); return false })
			}
		}),
		Connect(v, "load-failed", func(ev, uri, gerr uintptr) bool {
			end(errors.New("the page couldn't load: " + GoString(*At[Ptr](gerr + 8))))
			return true
		}),
	)
	WebKitWebViewLoadHTML(v, html, 0)
}

// printView shows the print dialog for a web view's page: it reports
// whether the user printed. after runs once the page is printed (or not).
func (h *Host) printView(v Ptr, landscape bool, after func()) (bool, error) {
	if err := LoadWebKitPrint(); err != nil {
		return false, errors.New("unsupported")
	}
	op := Print.OperationNew(v)
	if landscape {
		setup := Print.PageSetupNew()
		Print.SetOrientation(setup, 1) // GTK_PAGE_ORIENTATION_LANDSCAPE
		Print.OperationSetSetup(op, setup)
		ObjectUnref(setup)
	}
	resp := Print.OperationRunDialog(op, h.dialogParent())
	printed := resp == 0 // WEBKIT_PRINT_OPERATION_RESPONSE_PRINT
	if !printed {
		ObjectUnref(op)
		if after != nil {
			after()
		}
		return false, nil
	}
	// Printing goes on: the operation (and the page) until it's done.
	var hs []uint64
	finish := func() {
		for _, hd := range hs {
			Disconnect(op, hd)
		}
		ObjectUnref(op)
		if after != nil {
			after()
		}
	}
	hs = append(hs,
		Connect(op, "finished", func() { IdleAdd(PriorityDefault, func() bool { finish(); return false }) }),
		Connect(op, "failed", func(uintptr) {}),
	)
	return true, nil
}

// printPDF prints a web view's page to a PDF file, on a page of the
// size and margins (points), without a dialog.
func (h *Host) printPDF(v Ptr, a printArgs, path string, done func(error)) {
	op := Print.OperationNew(v)
	settings := Print.SettingsNew()
	Print.SettingsSetPrinter(settings, "Print to File")
	Print.SettingsSet(settings, "output-file-format", "pdf")
	f := Sys.FileNewForPath(path)
	Print.SettingsSet(settings, "output-uri", GoStringFree(Sys.FileGetURI(f)))
	ObjectUnref(f)
	setup := Print.PageSetupNew()
	size := Print.PaperSizeNewCustom("ryui", "Page", a.Width, a.Height, UnitPoints)
	Print.SetPaperSize(setup, size)
	Print.PaperSizeFree(size)
	Print.SetOrientation(setup, 0) // the size says it
	Print.SetTopMargin(setup, a.Top, UnitPoints)
	Print.SetBottomMargin(setup, a.Bottom, UnitPoints)
	Print.SetLeftMargin(setup, a.Left, UnitPoints)
	Print.SetRightMargin(setup, a.Right, UnitPoints)
	Print.OperationSetSettings(op, settings)
	Print.OperationSetSetup(op, setup)
	ObjectUnref(settings)
	ObjectUnref(setup)
	answered := false
	var hs []uint64
	end := func(err error) {
		if answered {
			return
		}
		answered = true
		for _, hd := range hs {
			Disconnect(op, hd)
		}
		IdleAdd(PriorityDefault, func() bool {
			ObjectUnref(op)
			if err == nil {
				if st, serr := os.Stat(path); serr != nil || st.Size() == 0 {
					err = errors.New("the PDF couldn't be written")
				}
			}
			done(err)
			return false
		})
	}
	hs = append(hs,
		Connect(op, "failed", func(gerr uintptr) {
			msg := "the PDF couldn't be made"
			if gerr != 0 {
				msg += ": " + GoString(*At[Ptr](gerr + 8))
			}
			end(errors.New(msg))
		}),
		Connect(op, "finished", func() { end(nil) }),
	)
	Print.OperationPrint(op)
}

// printFile prints a file: through the print portal, else GTK's print
// dialog. done gets whether it was printed.
func (h *Host) printFile(path, title string, landscape bool, done func(bool, error)) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		done(false, err)
		return
	}
	closeFD := func() {
		if fd >= 0 {
			syscall.Close(fd)
			fd = -1
		}
	}
	h.portalRequest("org.freedesktop.portal.Print", "PreparePrint", func(token string) Ptr {
		setup := map[string]Ptr{}
		if landscape {
			setup["Orientation"] = VStr("landscape")
		}
		return VTuple(VStr(h.parentWindow()), VStr(title), VDict(map[string]Ptr{}), VDict(setup),
			VDict(map[string]Ptr{"handle_token": VStr(token), "modal": VBool(true)}))
	}, nil, func(code uint32, results map[string]any, err error) {
		if errNoPortal(err) {
			closeFD()
			h.printFileDialog(path, title, done)
			return
		}
		if err != nil || code != 0 {
			closeFD()
			if code == 1 {
				done(false, nil)
			} else {
				done(false, err)
			}
			return
		}
		tok, _ := results["token"].(uint32)
		h.portalRequest("org.freedesktop.portal.Print", "Print", func(token string) Ptr {
			return VTuple(VStr(h.parentWindow()), VStr(title), VHandle(0),
				VDict(map[string]Ptr{"handle_token": VStr(token), "token": VU32(tok), "modal": VBool(true)}))
		}, []int32{int32(fd)}, func(code uint32, _ map[string]any, err error) {
			closeFD()
			switch {
			case err != nil:
				done(false, err)
			case code == 0:
				done(true, nil)
			default:
				done(false, nil)
			}
		})
	})
}

// printFileDialog prints a file with GTK's print dialog and a print job
// (no portal).
func (h *Host) printFileDialog(path, title string, done func(bool, error)) {
	d := Print.UnixDialogNew(title, h.dialogParent())
	Print.UnixDialogSetCaps(d, PrintPreviewCaps)
	WindowSetModal(d, true)
	answered := false
	Connect(d, "response", func(resp uintptr) {
		if answered {
			return
		}
		answered = true
		if int32(resp) != ResponseOK {
			WindowDestroy(d)
			done(false, nil)
			return
		}
		printer := Print.UnixDialogPrinter(d)
		settings := Print.UnixDialogSettings(d)
		setup := Print.UnixDialogSetup(d)
		job := Print.JobNew(title, printer, settings, setup)
		ObjectUnref(settings)
		WindowDestroy(d)
		var gerr Ptr
		if !Print.JobSetSourceFile(job, path, &gerr) {
			ObjectUnref(job)
			done(false, GError(gerr))
			return
		}
		PrintJobSend(job, func(err error) {
			ObjectUnref(job)
			done(err == nil, err)
		})
	})
	WindowPresent(d)
}

// Mail.

// composeCan: mail when an app takes mailto: links (or the portal's
// there, which finds one); no composer of Linux's own, no messages.
func (h *Host) composeCan() []byte {
	b := []byte{0, 0, 0, 0}
	if app := Sys.AppInfoGetDefaultForScheme("mailto"); app != 0 {
		ObjectUnref(app)
		b[0] = 1
	} else if os.Getenv("FLATPAK_ID") != "" {
		b[0] = 1 // the portal asks the host's
	}
	return b
}

type mailArgs struct {
	To, CC, BCC []string
	Subject     string
	Text        string
	Mailto      string
	Attachments []modSource
}

// composeMail hands the mail to the user's mail app; answered 3, unknown,
// or 0 if the user cancelled the portal's chooser.
func (h *Host) composeMail(args []byte, reply func([]byte, error)) {
	var a mailArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("compose.mail: bad arguments"))
		return
	}
	h.localFiles(a.Attachments, func(paths []string, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		paths = namedCopies(paths, a.Attachments, h.modCacheDir())
		h.mailPortal(a, paths, func(code uint32, err error) {
			if errNoPortal(err) {
				h.mailFallback(a, paths, reply)
				return
			}
			switch {
			case err != nil:
				reply(nil, err)
			case code == 1:
				reply([]byte{0}, nil)
			case code == 0:
				reply([]byte{3}, nil)
			default:
				h.mailFallback(a, paths, reply)
			}
		})
	})
}

// namedCopies gives attachments their names (a copy named so, when the
// file's isn't).
func namedCopies(paths []string, files []modSource, cache string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p
		name := strings.TrimSpace(files[i].Name)
		if name == "" || name == filepath.Base(p) {
			continue
		}
		dir := filepath.Join(cache, "ryui-mail", randomName())
		to := filepath.Join(dir, filepath.Base(name))
		if os.MkdirAll(dir, 0o755) == nil {
			if _, err := copyFile(p, to); err == nil {
				out[i] = to
			}
		}
	}
	return out
}

// mailPortal composes with the email portal.
func (h *Host) mailPortal(a mailArgs, paths []string, done func(uint32, error)) {
	var fds []int32
	for _, p := range paths {
		fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			for _, f := range fds {
				syscall.Close(int(f))
			}
			done(2, err)
			return
		}
		fds = append(fds, int32(fd))
	}
	closeAll := func() {
		for _, f := range fds {
			syscall.Close(int(f))
		}
	}
	h.portalRequest("org.freedesktop.portal.Email", "ComposeEmail", func(token string) Ptr {
		o := map[string]Ptr{"handle_token": VStr(token)}
		if len(a.To) > 0 {
			o["addresses"] = VStrings(a.To)
			o["address"] = VStr(a.To[0])
		}
		if len(a.CC) > 0 {
			o["cc"] = VStrings(a.CC)
		}
		if len(a.BCC) > 0 {
			o["bcc"] = VStrings(a.BCC)
		}
		if a.Subject != "" {
			o["subject"] = VStr(a.Subject)
		}
		if a.Text != "" {
			o["body"] = VStr(a.Text)
		}
		if len(fds) > 0 {
			hs := make([]Ptr, len(fds))
			for i := range fds {
				hs[i] = VHandle(int32(i))
			}
			o["attachment_fds"] = VArray("h", hs...)
		}
		return VTuple(VStr(h.parentWindow()), VDict(o))
	}, fds, func(code uint32, _ map[string]any, err error) {
		closeAll()
		done(code, err)
	})
}

// mailFallback: xdg-email (xdg-utils) with the attachments, else the
// mailto: link.
func (h *Host) mailFallback(a mailArgs, paths []string, reply func([]byte, error)) {
	if exe, err := exec.LookPath("xdg-email"); err == nil {
		args := []string{"--utf8"}
		for _, c := range a.CC {
			args = append(args, "--cc", c)
		}
		for _, b := range a.BCC {
			args = append(args, "--bcc", b)
		}
		if a.Subject != "" {
			args = append(args, "--subject", a.Subject)
		}
		if a.Text != "" {
			args = append(args, "--body", a.Text)
		}
		for _, p := range paths {
			args = append(args, "--attach", p)
		}
		args = append(args, a.To...)
		go func() {
			err := exec.Command(exe, args...).Run()
			h.Post(func() {
				if err != nil {
					reply(nil, errors.New("unavailable"))
					return
				}
				reply([]byte{3}, nil)
			})
		}()
		return
	}
	if len(paths) > 0 || a.Mailto == "" {
		reply(nil, errors.New("unavailable"))
		return
	}
	if app := Sys.AppInfoGetDefaultForScheme("mailto"); app == 0 {
		reply(nil, errors.New("unavailable"))
		return
	} else {
		ObjectUnref(app)
	}
	if _, err := url.Parse(a.Mailto); err != nil {
		reply(nil, err)
		return
	}
	h.openURL(a.Mailto)
	reply([]byte{3}, nil)
}
