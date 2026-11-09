//go:build windows && amd64

package windows

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/egoist/ryui/internal/localfile"
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/native"
	"github.com/egoist/ryui/ops"
)

// The modules that hand files to the system: ryui/preview (the file's
// app), ryui/print (WebView2's printing, the file's app) and
// ryui/compose (mailto:, Simple MAPI). Their methods answer later (a
// download, a page loading, a composer the user closes).

type asyncMethod func(args []byte, reply func([]byte, error))

func (h *Host) asyncModule(name string) map[string]asyncMethod {
	switch name {
	case "photos", "files":
		return h.pickerModules(name)
	case "web", "js":
		return h.webModules(name)
	default:
		// The app's own (package native), unless the host has its own.
		if h.module(name) != nil {
			return nil
		}
		if am := native.ModuleNamed(name); am != nil {
			m := make(map[string]asyncMethod, len(am))
			for k, f := range am {
				m[k] = f
			}
			return m
		}
	case "preview":
		return map[string]asyncMethod{"show": h.preview}
	case "print":
		return map[string]asyncMethod{
			"file":    h.printFile,
			"html":    h.printHTML,
			"webView": h.printWebView,
			"pdf":     h.printPDF,
		}
	case "compose":
		return map[string]asyncMethod{
			"can":     func(_ []byte, reply func([]byte, error)) { reply(h.composeCan(), nil) },
			"mail":    h.composeMail,
			"message": func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("unavailable")) },
		}
	}
	return nil
}

// source is a file a module call names.
type source struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Name   string `json:"name"`
}

func (h *Host) cacheDir() string { return localfile.CacheDir(h.appName) }

// localFiles finds the files sources name (downloading URLs) off the
// main thread, and calls done with them on it.
func (h *Host) localFiles(sources []source, done func([]string, error)) {
	assets, cache := absAssets(h), h.cacheDir()
	go func() {
		paths := make([]string, len(sources))
		var err error
		for i, s := range sources {
			if paths[i], err = localfile.Resolve(s.Source, s.Type, assets, cache); err != nil {
				break
			}
		}
		h.Post(func() { done(paths, err) })
	}()
}

// owner is the window dialogs belong to.
func (h *Host) owner() uintptr { return h.window }

// preview opens the start file in its app, as Explorer does; answered
// once it's handed over (Windows doesn't say when it's closed).
func (h *Host) preview(args []byte, reply func([]byte, error)) {
	var a struct {
		Files []source `json:"files"`
		Start int      `json:"start"`
	}
	if err := json.Unmarshal(args, &a); err != nil || len(a.Files) == 0 {
		reply(nil, errors.New("preview.show: bad arguments"))
		return
	}
	f := a.Files[min(max(a.Start, 0), len(a.Files)-1)]
	h.localFiles([]source{f}, func(paths []string, err error) {
		if err == nil {
			err = ShellExecute(h.owner(), "", paths[0])
		}
		if errors.Is(err, ErrNoAssociation) {
			err = errors.New("no viewer")
		}
		reply(nil, err)
	})
}

type printArgs struct {
	Source    string  `json:"source"`
	Type      string  `json:"type"`
	HTML      string  `json:"html"`
	Title     string  `json:"title"`
	View      uint32  `json:"view"`
	Landscape bool    `json:"landscape"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	Top       float64 `json:"top"`
	Right     float64 `json:"right"`
	Bottom    float64 `json:"bottom"`
	Left      float64 `json:"left"`
}

// printFile has the file's app print it (the shell's "print" verb):
// answered 2, unknown, once it's handed over.
func (h *Host) printFile(args []byte, reply func([]byte, error)) {
	var a printArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("print.file: bad arguments"))
		return
	}
	h.localFiles([]source{{Source: a.Source, Type: a.Type}}, func(paths []string, err error) {
		if err == nil {
			err = ShellExecute(h.owner(), "print", paths[0])
		}
		if errors.Is(err, ErrNoAssociation) {
			reply(nil, errors.New("unsupported"))
			return
		}
		if err != nil {
			reply(nil, err)
			return
		}
		reply([]byte{2}, nil)
	})
}

// The WebView2 interfaces printing uses, from WebView2.h.
const (
	wvCallDevToolsProtocolMethod = 36
	wv16ShowPrintUI              = 114
)

var iidWebView2_16 = NewGUID("0eb34dc9-9f91-41e1-8639-95cd5943906b")

// showPrintUI shows WebView2's print dialog for the page view shows:
// kind 0 the browser's (its preview, in the view), 1 the system's.
// Without ICoreWebView2_16 (a runtime from before 2023), the page's own
// window.print(), the browser's.
func showPrintUI(view uintptr, kind uintptr) {
	if wv16 := QueryInterface(view, &iidWebView2_16); wv16 != 0 {
		ComCall(wv16, wv16ShowPrintUI, kind)
		Release(wv16)
		return
	}
	ComCall(view, wvExecuteScript, Str("window.print()"), 0)
}

// printWebView shows the browser's print preview for a WebView's page,
// as Edge prints; answered 2, unknown (WebView2 doesn't say).
func (h *Host) printWebView(args []byte, reply func([]byte, error)) {
	var a printArgs
	json.Unmarshal(args, &a)
	n := h.nodes[ops.NodeID(a.View)]
	if n == nil || n.web == nil || n.web.video != nil {
		reply(nil, errors.New("not a WebView"))
		return
	}
	n.web.do(func() {
		showPrintUI(n.web.view, 0)
		reply([]byte{2}, nil)
	})
}

// A printPage is markup in a WebView2 that's never shown.
type printPage struct {
	hwnd, controller, view uintptr
}

// lastPage is the page HTML printed last, kept while its dialog may
// still print it (WebView2 doesn't say when it's done).
var lastPage *printPage

// loadPrintPage loads html in a WebView2 in the host's hidden window,
// then calls done on the main thread.
func (h *Host) loadPrintPage(html string, done func(*printPage, error)) {
	p := &printPage{hwnd: h.makeView(0)}
	SetWindowPos(p.hwnd, 0, 0, 0, 816, 1056, SWP_NOZORDER|SWP_NOACTIVATE) // a Letter page at 96 dpi
	h.whenWebEnvironment(func() {
		if webEnv == 0 {
			p.close()
			done(nil, errors.New("unavailable"))
			return
		}
		hr := WithHandler(func(hr, controller uintptr) {
			if Failed(hr) || controller == 0 {
				p.close()
				done(nil, errors.New("WebView2 couldn't make a view"))
				return
			}
			AddRef(controller)
			p.controller = controller
			ComCall(controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&p.view)))
			r := ClientRect(p.hwnd)
			ComCall(controller, ctlPutBounds, uintptr(unsafe.Pointer(&r)))
			var tok int64
			finished := false
			WithHandler(func(_, args uintptr) {
				if finished {
					return
				}
				finished = true
				var ok int32
				ComCall(args, navCompletedGetIsSuccess, uintptr(unsafe.Pointer(&ok)))
				if ok == 0 {
					p.close()
					done(nil, errors.New("the page couldn't load"))
					return
				}
				done(p, nil)
			}, func(handler uintptr) uintptr {
				return ComCall(p.view, wvAddNavigationCompleted, handler, uintptr(unsafe.Pointer(&tok)))
			})
			ComCall(p.view, wvNavigateToString, Str(html))
		}, func(handler uintptr) uintptr {
			return ComCall(webEnv, envCreateController, p.hwnd, handler)
		})
		if Failed(hr) {
			p.close()
			done(nil, errors.New("WebView2 couldn't make a view"))
		}
	})
}

func (p *printPage) close() {
	if p.controller != 0 {
		ComCall(p.controller, ctlClose)
		Release(p.view)
		Release(p.controller)
		p.controller, p.view = 0, 0
	}
	if p.hwnd != 0 {
		DestroyWindow(p.hwnd)
		p.hwnd = 0
	}
}

// printHTML shows the system's print dialog for the markup, printed by
// WebView2; answered 2, unknown (WebView2 doesn't say).
func (h *Host) printHTML(args []byte, reply func([]byte, error)) {
	var a printArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("print.html: bad arguments"))
		return
	}
	h.loadPrintPage(a.HTML, func(p *printPage, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		if lastPage != nil {
			lastPage.close()
		}
		lastPage = p
		showPrintUI(p.view, 1)
		reply([]byte{2}, nil)
	})
}

// printPDF lays the markup out with Chromium's printing (DevTools'
// Page.printToPDF: CSS's page breaks, @media print) and writes the PDF.
func (h *Host) printPDF(args []byte, reply func([]byte, error)) {
	var a printArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("print.pdf: bad arguments"))
		return
	}
	if a.Width <= 0 || a.Height <= 0 || a.Left+a.Right >= a.Width || a.Top+a.Bottom >= a.Height {
		reply(nil, errors.New("the margins leave no room on the page"))
		return
	}
	in := func(pt float64) float64 { return pt / 72 } // DevTools takes inches
	params, _ := json.Marshal(map[string]any{
		"paperWidth": in(a.Width), "paperHeight": in(a.Height),
		"marginTop": in(a.Top), "marginRight": in(a.Right), "marginBottom": in(a.Bottom), "marginLeft": in(a.Left),
		"printBackground": true, "preferCSSPageSize": false, "transferMode": "ReturnAsBase64",
	})
	h.loadPrintPage(a.HTML, func(p *printPage, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		hr := WithHandler(func(hr, result uintptr) {
			defer p.close()
			if Failed(hr) {
				reply(nil, errors.New("the PDF couldn't be made"))
				return
			}
			var r struct {
				Data string `json:"data"`
			}
			if err := json.Unmarshal([]byte(GoStr(result)), &r); err != nil {
				reply(nil, err)
				return
			}
			pdf, err := base64.StdEncoding.DecodeString(r.Data)
			if err != nil {
				reply(nil, err)
				return
			}
			path, err := localfile.NewPDF(h.cacheDir(), a.Title)
			if err == nil {
				err = os.WriteFile(path, pdf, 0o644)
			}
			if err != nil {
				reply(nil, err)
				return
			}
			reply([]byte(path), nil)
		}, func(handler uintptr) uintptr {
			return ComCall(p.view, wvCallDevToolsProtocolMethod, Str("Page.printToPDF"), Str(string(params)), handler)
		})
		if Failed(hr) {
			p.close()
			reply(nil, errors.New("the PDF couldn't be made"))
		}
	})
}

// composeCan: mail if an app takes mailto: links; no composer of
// Windows's own, no messages.
func (h *Host) composeCan() []byte {
	b := []byte{0, 0, 0, 0}
	if SchemeHasApp("mailto") {
		b[0] = 1
	}
	return b
}

// composeMail hands the mail to the user's mail app: its mailto: link;
// with attachments, Simple MAPI, on a thread of its own while the
// composer shows. Answered 3, unknown, or 0 if the user cancelled MAPI's.
func (h *Host) composeMail(args []byte, reply func([]byte, error)) {
	var a struct {
		To, CC, BCC []string
		Subject     string
		Text        string
		Mailto      string
		Attachments []source
	}
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("compose.mail: bad arguments"))
		return
	}
	if len(a.Attachments) == 0 {
		if !SchemeHasApp("mailto") {
			reply(nil, errors.New("unavailable"))
			return
		}
		if err := ShellExecute(h.owner(), "open", a.Mailto); err != nil {
			reply(nil, err)
			return
		}
		reply([]byte{3}, nil)
		return
	}
	h.localFiles(a.Attachments, func(paths []string, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		files := make([]MailFile, len(paths))
		for i, p := range paths {
			name := strings.TrimSpace(a.Attachments[i].Name)
			if name == "" {
				name = filepath.Base(p)
			}
			files[i] = MailFile{Path: p, Name: name}
		}
		go func() {
			err := SendMail(a.To, a.CC, a.BCC, a.Subject, a.Text, files)
			h.Post(func() {
				switch {
				case err == nil:
					reply([]byte{3}, nil)
				case errors.Is(err, ErrMAPICancelled):
					reply([]byte{0}, nil)
				case errors.Is(err, ErrMAPIUnavailable):
					reply(nil, errors.New("unavailable"))
				default:
					reply(nil, err)
				}
			})
		}()
	})
}
