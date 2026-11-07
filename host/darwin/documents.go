//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/internal/localfile"
	"github.com/egoist/ryui/ops"
)

// The modules that hand files to the system: ryui/preview (Quick Look's
// panel), ryui/print (NSPrintOperation) and ryui/compose
// (NSSharingService, mailto:).

// source is a file a module call names: its source and MIME type.
type source struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Name   string `json:"name"`
}

// cacheDir is the app's folder in the user's caches.
func cacheDir() string {
	app := GoString(Send(Send(Class("NSBundle"), "mainBundle"), "bundleIdentifier"))
	if app == "" {
		app = theHost.appName
	}
	return localfile.CacheDir(app)
}

// localFiles finds the files sources name (downloading URLs), off the
// main thread, and calls done with them on it.
func (h *Host) localFiles(sources []source, done func([]string, error)) {
	assets, cache := h.assets, cacheDir()
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

// Quick Look's panel: its controller is RyUITarget, the app's and the
// windows' delegate, which the panel finds up the responder chain.

var previewing struct {
	urls  []ID // retained
	start int
	reply func([]byte, error)
}

var quickLookLoaded bool

// preview is the "preview" module's "show" (JSON: files, start): the
// files in Quick Look's panel, answered once it closes.
func (h *Host) preview(args []byte, reply func([]byte, error)) {
	var a struct {
		Files []source `json:"files"`
		Start int      `json:"start"`
	}
	if err := json.Unmarshal(args, &a); err != nil || len(a.Files) == 0 {
		reply(nil, errors.New("preview.show: bad arguments"))
		return
	}
	h.localFiles(a.Files, func(paths []string, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		if !quickLookLoaded {
			quickLookLoaded = true
			Dlopen("/System/Library/Frameworks/Quartz.framework/Quartz")
		}
		if !HasClass("QLPreviewPanel") {
			reply(nil, errors.New("no viewer"))
			return
		}
		endPreview() // what showed before: closed for these
		for _, p := range paths {
			previewing.urls = append(previewing.urls, Retain(FileURL(p)))
		}
		previewing.start = min(max(a.Start, 0), len(paths)-1)
		previewing.reply = reply
		panel := Send(Class("QLPreviewPanel"), "sharedPreviewPanel")
		if SendBool(panel, "isVisible") && Send(panel, "dataSource") == h.target {
			Send(panel, "reloadData")
			Send(panel, "setCurrentPreviewItemIndex:", uintptr(previewing.start))
		} else {
			Send(panel, "updateController")
			Send(panel, "makeKeyAndOrderFront:", 0)
		}
	})
}

// endPreview answers the preview showing, if one is.
func endPreview() {
	for _, u := range previewing.urls {
		Release(u)
	}
	r := previewing.reply
	previewing.urls, previewing.reply = nil, nil
	if r != nil {
		r(nil, nil)
	}
}

// previewMethods are RyUITarget's: Quick Look's panel controller, data
// source and delegate.
func previewMethods() []objc.MethodDef {
	return []objc.MethodDef{
		Method("acceptsPreviewPanelControl:", func(self ID, _ SEL, panel ID) bool { return previewing.reply != nil }),
		Method("beginPreviewPanelControl:", func(self ID, _ SEL, panel ID) {
			Send(panel, "setDataSource:", uintptr(self))
			Send(panel, "setDelegate:", uintptr(self))
			Send(panel, "reloadData")
			Send(panel, "setCurrentPreviewItemIndex:", uintptr(previewing.start))
		}),
		Method("endPreviewPanelControl:", func(self ID, _ SEL, panel ID) {
			Send(panel, "setDataSource:", 0)
			Send(panel, "setDelegate:", 0)
			endPreview()
		}),
		Method("numberOfPreviewItemsInPreviewPanel:", func(self ID, _ SEL, panel ID) int { return len(previewing.urls) }),
		Method("previewPanel:previewItemAtIndex:", func(self ID, _ SEL, panel ID, i int) ID {
			if i < 0 || i >= len(previewing.urls) {
				return 0
			}
			return previewing.urls[i]
		}),
	}
}

// Printing: an NSPrintOperation, its panel a sheet on the key window,
// answered when it's run (u8: 0 cancelled, 1 printed or saved as a PDF).

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

var (
	printClass bool
	printer    ID                         // RyUIPrintDelegate
	printRuns  = map[ID]func(ok bool){}   // by operation
	printPages = map[ID]func(err error){} // offscreen web views, by view
)

func printDelegate() ID {
	if !printClass {
		printClass = true
		DefineClass("RyUIPrintDelegate", "NSObject", []string{"WKNavigationDelegate"}, []objc.MethodDef{
			// On the operation's thread, if it spawned one: answered on the main one.
			Method("printOperationDidRun:success:contextInfo:", func(self ID, _ SEL, op ID, ok bool, ctx uintptr) {
				theHost.Post(func() {
					if f := printRuns[op]; f != nil {
						delete(printRuns, op)
						f(ok)
						Release(op)
					}
				})
			}),
			Method("webView:didFinishNavigation:", func(self ID, _ SEL, v, nav ID) {
				if f := printPages[v]; f != nil {
					delete(printPages, v)
					f(nil)
				}
			}),
			Method("webView:didFailNavigation:withError:", func(self ID, _ SEL, v, nav, err ID) { pageFailed(v, err) }),
			Method("webView:didFailProvisionalNavigation:withError:", func(self ID, _ SEL, v, nav, err ID) { pageFailed(v, err) }),
		})
		printer = Send(Class("RyUIPrintDelegate"), "new")
	}
	return printer
}

func pageFailed(v, err ID) {
	if f := printPages[v]; f != nil {
		delete(printPages, v)
		f(errors.New(GoString(Send(err, "localizedDescription"))))
	}
}

var pdfKitLoaded bool

func (h *Host) print(method string, args []byte, reply func([]byte, error)) {
	var a printArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("print."+method+": bad arguments"))
		return
	}
	info := Send(Send(Class("NSPrintInfo"), "sharedPrintInfo"), "copy")
	if a.Landscape {
		Send(info, "setOrientation:", 1) // NSPaperOrientationLandscape
	}
	run := func(op ID, job string, after func()) {
		if a.Job != "" {
			job = a.Job
		}
		if job == "" {
			job = h.appName
		}
		Send(op, "setJobTitle:", uintptr(String(job)))
		const panelOptions = 1 | 2 | 4 | 8 | 16 | 1<<17 // copies, pages, paper, orientation, scaling, preview
		Send(Send(op, "printPanel"), "setOptions:", panelOptions)
		var window ID
		if w := h.keyWin(); w != nil {
			window = w.window
		}
		h.runPrint(op, window, func(ok bool) {
			Release(info)
			if after != nil {
				after()
			}
			if ok {
				reply([]byte{1}, nil)
			} else {
				reply([]byte{0}, nil)
			}
		})
	}
	switch method {
	case "file":
		h.localFiles([]source{{Source: a.Source, Type: a.Type}}, func(paths []string, err error) {
			if err != nil {
				Release(info)
				reply(nil, err)
				return
			}
			doc := pdfDocument(paths[0], a.Type)
			if doc == 0 {
				Release(info)
				reply(nil, errors.New("unsupported"))
				return
			}
			// A PDF as it is, smaller pages fit; an image to fill the page.
			scale := uintptr(2) // kPDFPrintPageScaleDownToFit
			if !isPDF(paths[0], a.Type) {
				scale = 1 // kPDFPrintPageScaleToFit
			}
			op := Send(doc, "printOperationForPrintInfo:scalingMode:autoRotate:", uintptr(info), scale, 1)
			if op == 0 {
				Release(doc)
				Release(info)
				reply(nil, errors.New("unsupported"))
				return
			}
			run(Retain(op), filepath.Base(paths[0]), func() { Release(doc) })
		})
	case "webView":
		n := h.nodes[ops.NodeID(a.View)]
		if n == nil || n.web == nil {
			Release(info)
			reply(nil, errors.New("not a WebView"))
			return
		}
		op := webPrintOperation(n.web.view, info)
		run(Retain(op), GoString(Send(n.web.view, "title")), nil)
	case "html":
		h.loadPrintPage(a.HTML, 612, 792, func(v, w ID, err error) {
			if err != nil {
				Release(info)
				reply(nil, err)
				return
			}
			op := webPrintOperation(v, info)
			run(Retain(op), GoString(Send(v, "title")), func() { closePrintPage(v, w) })
		})
	case "pdf":
		if a.Width <= 0 || a.Height <= 0 || a.Left+a.Right >= a.Width || a.Top+a.Bottom >= a.Height {
			Release(info)
			reply(nil, errors.New("the margins leave no room on the page"))
			return
		}
		MsgSetSize(info, Sel("setPaperSize:"), Size{Width: a.Width, Height: a.Height})
		Send(info, "setOrientation:", 0) // the size says it
		MsgSetFloat(info, Sel("setTopMargin:"), a.Top)
		MsgSetFloat(info, Sel("setBottomMargin:"), a.Bottom)
		MsgSetFloat(info, Sel("setLeftMargin:"), a.Left)
		MsgSetFloat(info, Sel("setRightMargin:"), a.Right)
		Send(info, "setHorizontalPagination:", 1) // fit the width
		Send(info, "setVerticalPagination:", 0)   // pages down
		Send(info, "setHorizontallyCentered:", 0)
		Send(info, "setVerticallyCentered:", 0)
		Send(info, "setJobDisposition:", uintptr(Const(AppKit(), "NSPrintSaveJob")))
		h.loadPrintPage(a.HTML, a.Width-a.Left-a.Right, a.Height-a.Top-a.Bottom, func(v, w ID, err error) {
			if err != nil {
				Release(info)
				reply(nil, err)
				return
			}
			path, err := localfile.NewPDF(cacheDir(), a.Title)
			if err != nil {
				Release(info)
				closePrintPage(v, w)
				reply(nil, err)
				return
			}
			Send(Send(info, "dictionary"), "setObject:forKey:", uintptr(FileURL(path)), uintptr(Const(AppKit(), "NSPrintJobSavingURL")))
			op := Retain(webPrintOperation(v, info))
			Send(op, "setShowsPrintPanel:", 0)
			Send(op, "setShowsProgressPanel:", 0)
			h.runPrint(op, w, func(ok bool) {
				Release(info)
				closePrintPage(v, w)
				if st, err := os.Stat(path); !ok || err != nil || st.Size() == 0 {
					os.RemoveAll(filepath.Dir(path))
					reply(nil, errors.New("the PDF couldn't be written"))
					return
				}
				reply([]byte(path), nil)
			})
		})
	default:
		Release(info)
		reply(nil, errors.New("ryui: print."+method+" isn't on macOS"))
	}
}

// runPrint runs op (retained, released after) with its panels on window
// (none: on its own), then done.
func (h *Host) runPrint(op, window ID, done func(ok bool)) {
	printRuns[op] = done
	// WebKit's print view lays its pages out from another thread, while
	// the main one answers its web process (on the main one, it never
	// learns its pages).
	Send(op, "setCanSpawnSeparateThread:", 1)
	Send(op, "runOperationModalForWindow:delegate:didRunSelector:contextInfo:",
		uintptr(window), uintptr(printDelegate()), uintptr(Sel("printOperationDidRun:success:contextInfo:")), 0)
}

// webPrintOperation is a WKWebView's print operation, its view the web
// view's size (else WebKit prints blank pages).
func webPrintOperation(v, info ID) ID {
	op := Send(v, "printOperationWithPrintInfo:", uintptr(info))
	MsgSetRect(Send(op, "view"), Sel("setFrame:"), MsgRect(v, Sel("bounds")))
	return op
}

// loadPrintPage loads html in a WKWebView of width w that's never shown,
// in a window that isn't either (printing wants one), then calls done.
func (h *Host) loadPrintPage(html string, w, ht float64, done func(view, window ID, err error)) {
	if !webKitLoaded {
		webKitLoaded = true
		Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit")
	}
	frame := Rect{Size: Size{Width: w, Height: ht}}
	win := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"), frame, 0, 2, false)
	Send(win, "setReleasedWhenClosed:", 0)
	conf := New("WKWebViewConfiguration")
	v := MsgInitRectID(Send(Class("WKWebView"), "alloc"), Sel("initWithFrame:configuration:"), frame, conf)
	Release(conf)
	Send(Send(win, "contentView"), "addSubview:", uintptr(v))
	Send(v, "setNavigationDelegate:", uintptr(printDelegate()))
	printPages[v] = func(err error) {
		if err != nil {
			closePrintPage(v, win)
			done(0, 0, err)
			return
		}
		done(v, win, nil)
	}
	Send(v, "loadHTMLString:baseURL:", uintptr(String(html)), 0)
}

func closePrintPage(v, win ID) {
	Send(v, "setNavigationDelegate:", 0)
	Send(v, "removeFromSuperview")
	Release(v)
	Send(win, "close")
	Release(win)
}

func isPDF(path, typ string) bool {
	return typ == "application/pdf" || strings.EqualFold(filepath.Ext(path), ".pdf")
}

// pdfDocument is path as a PDFDocument (retained): a PDF, or an image on
// a page of its size; 0 if it's neither.
func pdfDocument(path, typ string) ID {
	if !pdfKitLoaded {
		pdfKitLoaded = true
		Dlopen("/System/Library/Frameworks/PDFKit.framework/PDFKit")
		Dlopen("/System/Library/Frameworks/Quartz.framework/Quartz")
	}
	if isPDF(path, typ) {
		return Send(Send(Class("PDFDocument"), "alloc"), "initWithURL:", uintptr(FileURL(path)))
	}
	img := Send(Send(Class("NSImage"), "alloc"), "initWithContentsOfURL:", uintptr(FileURL(path)))
	if img == 0 {
		return 0
	}
	defer Release(img)
	page := Send(Send(Class("PDFPage"), "alloc"), "initWithImage:", uintptr(img))
	if page == 0 {
		return 0
	}
	defer Release(page)
	doc := New("PDFDocument")
	Send(doc, "insertPage:atIndex:", uintptr(page), 0)
	return doc
}

// Composing: mailto: links, and the share services for mail with
// attachments and for messages.

type composeArgs struct {
	To          []string `json:"to"`
	CC          []string `json:"cc"`
	BCC         []string `json:"bcc"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	HTML        bool     `json:"html"`
	Text        string   `json:"text"`
	Mailto      string   `json:"mailto"`
	Attachments []source `json:"attachments"`
}

var (
	composeClass   bool
	composer       ID                             // RyUIComposeDelegate
	composeReplies = map[ID]func([]byte, error){} // by service
)

func (h *Host) compose(method string, args []byte, reply func([]byte, error)) {
	if method == "can" {
		ws := Send(Class("NSWorkspace"), "sharedWorkspace")
		mail := Send(ws, "URLForApplicationToOpenURL:", uintptr(URL("mailto:a@example.com"))) != 0
		msg := Send(Class("NSSharingService"), "sharingServiceNamed:", uintptr(Const(AppKit(), "NSSharingServiceNameComposeMessage")))
		text := msg != 0 && SendBool(msg, "canPerformWithItems:", uintptr(Array(String("")))) // a text to send
		b := []byte{0, 0, 0, 0}
		if mail {
			b[0] = 1
		}
		if text {
			b[2], b[3] = 1, 1
		}
		reply(b, nil)
		return
	}
	var a composeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("compose."+method+": bad arguments"))
		return
	}
	if method == "mail" && len(a.Attachments) == 0 {
		// The user's mail app, with all the fields the link has.
		u := URL(a.Mailto)
		if u == 0 || !SendBool(Send(Class("NSWorkspace"), "sharedWorkspace"), "openURL:", uintptr(u)) {
			reply(nil, errors.New("unavailable"))
			return
		}
		reply([]byte{3}, nil)
		return
	}
	if method == "mail" && len(a.CC)+len(a.BCC) > 0 {
		reply(nil, errors.New("unavailable")) // the share service has no CC or BCC
		return
	}
	name := "NSSharingServiceNameComposeEmail"
	if method == "message" {
		name = "NSSharingServiceNameComposeMessage"
	} else if method != "mail" {
		reply(nil, errors.New("ryui: compose."+method+" isn't on macOS"))
		return
	}
	h.localFiles(a.Attachments, func(paths []string, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		s := Send(Class("NSSharingService"), "sharingServiceNamed:", uintptr(Const(AppKit(), name)))
		if s == 0 {
			reply(nil, errors.New("unavailable"))
			return
		}
		var items []ID
		body := a.Body
		if method == "message" && a.Subject != "" {
			body = strings.TrimSpace(a.Subject + "\n" + a.Body)
		}
		switch {
		case a.HTML && body != "":
			if as := Send(Send(Class("NSAttributedString"), "alloc"), "initWithHTML:documentAttributes:", uintptr(Data([]byte(body))), 0); as != 0 {
				items = append(items, Autorelease(as))
			} else {
				items = append(items, String(a.Text))
			}
		case body != "":
			items = append(items, String(body))
		}
		for i, p := range paths {
			items = append(items, FileURL(named(p, a.Attachments[i].Name)))
		}
		list := Array(items...)
		if !SendBool(s, "canPerformWithItems:", uintptr(list)) {
			reply(nil, errors.New("unavailable"))
			return
		}
		recipients := make([]ID, len(a.To))
		for i, r := range a.To {
			recipients[i] = String(r)
		}
		Send(s, "setRecipients:", uintptr(Array(recipients...)))
		if method == "mail" {
			Send(s, "setSubject:", uintptr(String(a.Subject)))
		}
		if !composeClass {
			composeClass = true
			DefineClass("RyUIComposeDelegate", "NSObject", []string{"NSSharingServiceDelegate"}, []objc.MethodDef{
				Method("sharingService:didShareItems:", func(self ID, _ SEL, s, items ID) {
					if r := composeReplies[s]; r != nil {
						delete(composeReplies, s)
						Release(s)
						r([]byte{3}, nil)
					}
				}),
				Method("sharingService:didFailToShareItems:error:", func(self ID, _ SEL, s, items, e ID) {
					if r := composeReplies[s]; r != nil {
						delete(composeReplies, s)
						Release(s)
						if SendInt(e, "code") == 3072 { // NSUserCancelledError
							r([]byte{0}, nil)
						} else {
							r(nil, errors.New(GoString(Send(e, "localizedDescription"))))
						}
					}
				}),
			})
			composer = Send(Class("RyUIComposeDelegate"), "new")
		}
		Send(s, "setDelegate:", uintptr(composer))
		composeReplies[Retain(s)] = reply
		Send(s, "performWithItems:", uintptr(list))
	})
}

// named is path under name (a link to it in a folder of its own in the
// caches) if its name isn't name, for the attachment's name in the mail.
func named(path, name string) string {
	name = strings.NewReplacer("/", "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" || name == filepath.Base(path) {
		return path
	}
	base := filepath.Join(cacheDir(), "ryui-files")
	os.MkdirAll(base, 0o755)
	dir, err := os.MkdirTemp(base, "named-")
	if err != nil {
		return path
	}
	dst := filepath.Join(dir, name)
	if os.Link(path, dst) != nil {
		data, err := os.ReadFile(path)
		if err != nil || os.WriteFile(dst, data, 0o644) != nil {
			return path
		}
	}
	return dst
}
