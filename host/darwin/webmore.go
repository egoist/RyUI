//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// More of a WebView, as on iOS: the requests Block names are blocked (a
// content rule list), a response the page can't show is a Download, the
// "web" module runs script for its value (EvalJSResult) and reads the
// cookies (WebCookies), and the "js" module (asuka/sdk/js) runs script in a
// web view that's never shown.

// setBlock compiles Block's patterns (u32 count, then u32-length
// strings) into a content rule list: a host blocks it and its
// subdomains, a part of a URL any URL with it.
func (w *web) setBlock(h *Host, n *node, b []byte) {
	Send(w.ucc, "removeAllContentRuleLists")
	w.block++
	gen := w.block
	var rules []map[string]any
	if len(b) >= 4 {
		count := int(binary.LittleEndian.Uint32(b))
		b = b[4:]
		for range count {
			if len(b) < 4 {
				break
			}
			l := int(binary.LittleEndian.Uint32(b))
			if l > len(b)-4 {
				break
			}
			p := string(b[4 : 4+l])
			b = b[4+l:]
			esc := regexp.QuoteMeta(p)
			filter := esc
			if !strings.Contains(p, "/") {
				filter = `^[a-z]+://([^/:]+\.)?` + esc + `[:/]`
			}
			rules = append(rules, map[string]any{"trigger": map[string]any{"url-filter": filter}, "action": map[string]any{"type": "block"}})
		}
	}
	js, err := json.Marshal(rules)
	if len(rules) == 0 || err != nil {
		w.released()
		return
	}
	w.pending = gen
	var done objc.Block
	done = NewBlock(func(_ objc.Block, list, err ID) {
		Retain(list)
		h.Post(func() {
			done.Release()
			defer Release(list)
			if w.block != gen {
				return // another list, or the view went
			}
			if list != 0 {
				Send(w.ucc, "addContentRuleList:", uintptr(list))
			}
			w.released()
		})
	})
	store := Send(Class("WKContentRuleListStore"), "defaultStore")
	Send(store, "compileContentRuleListForIdentifier:encodedContentRuleList:completionHandler:",
		uintptr(String(fmt.Sprintf("asuka-block-%d", n.id))), uintptr(String(string(js))), uintptr(done))
}

// compiling reports whether Block's rule list is being compiled.
func (w *web) compiling() bool { return w.pending != 0 && w.pending == w.block }

// released runs the load held while the rule list compiled.
func (w *web) released() {
	w.pending = 0
	if f := w.held; f != nil {
		w.held = nil
		f()
	}
}

// decideResponse is the navigation delegate's answer for a response:
// the main frame's that the view can't show, or an attachment, is
// cancelled and reported as a Download; the rest load.
func (h *Host) decideResponse(v, resp, handler ID) {
	const cancel, allow = 0, 1
	r := Send(resp, "response")
	disposition := ""
	if SendBool(r, "isKindOfClass:", uintptr(Class("NSHTTPURLResponse"))) {
		disposition = GoString(Send(r, "valueForHTTPHeaderField:", uintptr(String("Content-Disposition"))))
	}
	if !SendBool(resp, "isForMainFrame") || SendBool(resp, "canShowMIMEType") && !strings.HasPrefix(strings.ToLower(disposition), "attachment") {
		CallBlock(uintptr(handler), allow)
		return
	}
	CallBlock(uintptr(handler), cancel)
	n := h.byView[v]
	if n == nil {
		return
	}
	var b []byte
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	str(GoString(Send(Send(r, "URL"), "absoluteString")))
	str(GoString(Send(r, "MIMEType")))
	str(GoString(Send(r, "suggestedFilename")))
	b = binary.LittleEndian.AppendUint64(b, uint64(int64(SendInt(r, "expectedContentLength"))))
	h.emit(n, ops.Event{Kind: ops.EventDownload, Bytes: b})
}

// evalBody runs s in the page's world and answers with its value as
// JSON: the last statement's, or what its promise resolves to.
const evalBody = `const v = await (0, eval)(s); return JSON.stringify(v === undefined ? null : v)`

// evalIn runs script in a web view (callAsyncJavaScript) and answers
// with its value as JSON, or what it threw.
func (h *Host) evalIn(view ID, script string, reply func([]byte, error)) {
	var done objc.Block
	done = NewBlock(func(_ objc.Block, result, err ID) {
		var out []byte
		var msg string
		if err != 0 {
			msg = GoString(Send(Send(err, "userInfo"), "objectForKey:", uintptr(String("WKJavaScriptExceptionMessage"))))
			if msg == "" {
				msg = GoString(Send(err, "localizedDescription"))
			}
		} else {
			out = []byte("null")
			if result != 0 && SendBool(result, "isKindOfClass:", uintptr(Class("NSString"))) {
				out = []byte(GoString(result))
			}
		}
		h.Post(func() {
			done.Release()
			if msg != "" {
				reply(nil, errors.New(msg))
				return
			}
			reply(out, nil)
		})
	})
	args := Dict(String("s"), String(script))
	world := Send(Class("WKContentWorld"), "pageWorld")
	Send(view, "callAsyncJavaScript:arguments:inFrame:inContentWorld:completionHandler:",
		uintptr(String(evalBody)), uintptr(args), 0, uintptr(world), uintptr(done))
}

// webModule is the "web" module: "eval" (args: u32 a WebView's node id,
// then the script) answers with the script's value as JSON; "cookies"
// (args: a URL) with the cookies the WebViews have for it, as a Cookie
// header.
func (h *Host) webModule() map[string]method {
	return map[string]method{
		"eval": func(args []byte, reply func([]byte, error)) {
			if len(args) < 4 {
				reply(nil, errors.New("bad arguments"))
				return
			}
			n := h.nodes[ops.NodeID(binary.LittleEndian.Uint32(args))]
			if n == nil || n.web == nil {
				reply(nil, errors.New("not a WebView"))
				return
			}
			h.evalIn(n.web.view, string(args[4:]), reply)
		},
		"cookies": func(args []byte, reply func([]byte, error)) {
			u, err := url.Parse(string(args))
			if err != nil || u.Hostname() == "" {
				reply(nil, nil)
				return
			}
			if !webKitLoaded {
				webKitLoaded = true
				Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit")
			}
			store := Send(Send(Class("WKWebsiteDataStore"), "defaultDataStore"), "httpCookieStore")
			var done objc.Block
			done = NewBlock(func(_ objc.Block, arr ID) {
				header := cookieHeader(u, arr)
				h.Post(func() {
					done.Release()
					reply([]byte(header), nil)
				})
			})
			Send(store, "getAllCookies:", uintptr(done))
		},
	}
}

// cookieHeader is the cookies of arr (NSHTTPCookies) that go with a
// request to u, as a Cookie header.
func cookieHeader(u *url.URL, arr ID) string {
	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	now := float64(time.Now().UnixNano()) / 1e9
	var parts []string
	for _, c := range ArrayItems(arr) {
		d := strings.ToLower(GoString(Send(c, "domain")))
		ok := host == d
		if strings.HasPrefix(d, ".") {
			ok = host == d[1:] || strings.HasSuffix(host, d)
		}
		if !ok || !strings.HasPrefix(path, GoString(Send(c, "path"))) {
			continue
		}
		if SendBool(c, "isSecure") && u.Scheme != "https" {
			continue
		}
		if exp := Send(c, "expiresDate"); exp != 0 && MsgFloat(exp, Sel("timeIntervalSince1970")) <= now {
			continue
		}
		parts = append(parts, GoString(Send(c, "name"))+"="+GoString(Send(c, "value")))
	}
	return strings.Join(parts, "; ")
}

// script is a asuka/sdk/js run: a web view that's never shown, its script,
// and who waits for it.
type script struct {
	view  ID
	src   string
	reply func([]byte, error)
}

// jsModule is the "js" module: "eval" (args: u32 timeout ms, then the
// script) runs the script in a web view made for it and let go after,
// and answers with its value as JSON, or what it threw (or "timeout").
func (h *Host) jsModule() map[string]method {
	return map[string]method{
		"eval": func(args []byte, reply func([]byte, error)) {
			if len(args) < 4 {
				reply(nil, errors.New("bad arguments"))
				return
			}
			ms := binary.LittleEndian.Uint32(args)
			if !webKitLoaded {
				webKitLoaded = true
				Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit")
			}
			conf := New("WKWebViewConfiguration")
			v := MsgInitRectID(Send(Class("WKWebView"), "alloc"), Sel("initWithFrame:configuration:"), Rect{}, conf)
			Release(conf)
			s := &script{view: v, src: string(args[4:]), reply: reply}
			if h.scripts == nil {
				h.scripts = map[ID]*script{}
			}
			h.scripts[v] = s
			Send(v, "setNavigationDelegate:", uintptr(h.target))
			Send(v, "loadHTMLString:baseURL:", uintptr(String("<!doctype html><html></html>")), 0)
			time.AfterFunc(time.Duration(ms)*time.Millisecond, func() {
				h.Post(func() { h.endScript(s, nil, errors.New("timeout")) })
			})
		},
	}
}

// scriptLoaded runs a asuka/sdk/js script once its page has loaded: true if
// view is one of theirs.
func (h *Host) scriptLoaded(view ID) bool {
	s := h.scripts[view]
	if s == nil {
		return false
	}
	h.evalIn(view, s.src, func(b []byte, err error) { h.endScript(s, b, err) })
	return true
}

// endScript answers a script once, and lets its web view go.
func (h *Host) endScript(s *script, b []byte, err error) {
	if s.reply == nil {
		return
	}
	reply := s.reply
	s.reply = nil
	delete(h.scripts, s.view)
	Send(s.view, "setNavigationDelegate:", 0)
	Send(s.view, "stopLoading")
	Release(s.view)
	reply(b, err)
}
