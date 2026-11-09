//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/ryui/internal/boot"
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// The native modules (ui.Call) on Windows, as the other hosts have them:
//
//   - secureStore (ryui/secure): the user's generic credentials in the
//     Credential Manager, named "<app>/<key>" (values up to 2560 bytes).
//   - device: info, the clipboard's text, keeping the display on
//     (SetThreadExecutionState).
//   - browser: the user's browser.
//   - photos and files (device.PickPhotos, PickFiles): the open file
//     dialog over the window in use; files are copied to the app's caches,
//     as iOS and Android give them.
//   - web (EvalJSResult, WebCookies): WebView2's DevTools protocol
//     (Runtime.evaluate, awaiting a promise; Network.getCookies).
//   - js (ryui/js): a script run in a WebView2 that's never shown.

// syncModules are the modules that answer at once.
func (h *Host) syncModules() map[string]func(method string, args []byte) ([]byte, error) {
	return map[string]func(method string, args []byte) ([]byte, error){
		"secureStore": h.secureStore,
		"device":      h.device,
		"browser": func(method string, args []byte) ([]byte, error) {
			if method != "open" {
				return nil, errNoMethod("browser", method)
			}
			OpenURL(string(args))
			return nil, nil
		},
	}
}

func errNoMethod(module, method string) error {
	return fmt.Errorf("ryui: %s.%s isn't on Windows yet", module, method)
}

// credentialTarget is a key's credential's name.
func (h *Host) credentialTarget(key string) string { return h.appName + "/" + key }

func (h *Host) secureStore(method string, args []byte) ([]byte, error) {
	switch method {
	case "get": // args: the key; result: u8 found, then the value
		v, err := ReadCredential(h.credentialTarget(string(args)))
		if errors.Is(err, ErrNoCredential) {
			return []byte{0}, nil
		}
		if err != nil {
			return nil, err
		}
		return append([]byte{1}, v...), nil
	case "set": // args: u32 key length, the key, the value
		if len(args) < 4 || int(binary.LittleEndian.Uint32(args)) > len(args)-4 {
			return nil, errors.New("secureStore.set: bad arguments")
		}
		n := int(binary.LittleEndian.Uint32(args))
		return nil, WriteCredential(h.credentialTarget(string(args[4:4+n])), string(args[4:4+n]), args[4+n:])
	case "delete":
		return nil, DeleteCredential(h.credentialTarget(string(args)))
	}
	return nil, errNoMethod("secureStore", method)
}

func (h *Host) device(method string, args []byte) ([]byte, error) {
	switch method {
	case "info": // JSON: model, system, systemVersion, appVersion, build
		return json.Marshal(map[string]string{
			"model": "PC", "system": "Windows", "systemVersion": fmt.Sprintf("10.0.%d", Build()),
			"appVersion": boot.Version, "build": boot.Build,
		})
	case "clipboard": // u8 has, then the text
		s, ok := ClipboardText()
		if !ok {
			return []byte{0}, nil
		}
		return append([]byte{1}, s...), nil
	case "keepAwake": // args: u8 on
		KeepAwake(len(args) > 0 && args[0] == 1)
		return nil, nil
	}
	return nil, errNoMethod("device", method)
}

// pickFiles shows the open dialog once the core has returned (it runs a
// modal loop) and copies what's chosen to the caches, off the main thread.
func (h *Host) pickFiles(filters []FileFilter, multiple bool, limit int, done func(files []pickedFile, err error)) {
	h.afterCore(func() {
		h.modal++
		paths, err := OpenFiles(h.windowInUse(), "", filters, multiple)
		h.modal--
		if err != nil || len(paths) == 0 {
			done(nil, err)
			return
		}
		if limit > 0 && len(paths) > limit {
			paths = paths[:limit]
		}
		dir := filepath.Join(h.cacheDir(), "ryui-files")
		go func() {
			files, err := copyPicked(dir, paths)
			h.Post(func() { done(files, err) })
		}()
	})
}

// fileFilters are the open dialog's types for MIME types: the
// extensions Windows knows for each ("image/*" its family's); none: any
// file.
func fileFilters(mimes []string) []FileFilter {
	families := map[string]string{
		"image/*": "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp;*.heic;*.heif;*.tif;*.tiff;*.ico;*.svg",
		"video/*": "*.mp4;*.m4v;*.mov;*.webm;*.avi;*.wmv;*.mkv",
		"audio/*": "*.mp3;*.m4a;*.aac;*.wav;*.flac;*.ogg;*.opus;*.wma",
		"text/*":  "*.txt;*.csv;*.md;*.html;*.htm;*.xml;*.json;*.log",
	}
	var pats []string
	for _, m := range mimes {
		switch {
		case m == "" || m == "*/*" || m == "application/*":
			return nil
		case families[m] != "":
			pats = append(pats, families[m])
		default:
			exts, _ := mime.ExtensionsByType(m)
			for _, e := range exts {
				pats = append(pats, "*"+e)
			}
		}
	}
	if len(pats) == 0 {
		return nil
	}
	return []FileFilter{{Name: strings.Join(mimes, ", "), Patterns: strings.Join(pats, ";")}, {Name: "*.*", Patterns: "*.*"}}
}

// pickedFile is device.File.
type pickedFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// copyPicked copies the files into dir, each in a folder of its own (so
// its name stays its own).
func copyPicked(dir string, paths []string) ([]pickedFile, error) {
	out := []pickedFile{}
	for i, src := range paths {
		into, err := os.MkdirTemp(dir, fmt.Sprint(i, "-"))
		if os.IsNotExist(err) {
			if err = os.MkdirAll(dir, 0o755); err == nil {
				into, err = os.MkdirTemp(dir, fmt.Sprint(i, "-"))
			}
		}
		if err != nil {
			return nil, err
		}
		to := filepath.Join(into, filepath.Base(src))
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(to, b, 0o644); err != nil {
			return nil, err
		}
		out = append(out, pickedFile{Path: to, Name: filepath.Base(src), Type: mime.TypeByExtension(filepath.Ext(src)), Size: int64(len(b))})
	}
	return out, nil
}

// pickerModules are photos and files.
func (h *Host) pickerModules(name string) map[string]asyncMethod {
	switch name {
	case "photos":
		// args: u8 limit; result: the picked images' paths, a line each.
		return map[string]asyncMethod{"pick": func(args []byte, reply func([]byte, error)) {
			limit := 1
			if len(args) > 0 {
				limit = int(args[0])
			}
			h.pickFiles(fileFilters([]string{"image/*"}), limit != 1, limit, func(files []pickedFile, err error) {
				var paths []string
				for _, f := range files {
					paths = append(paths, f.Path)
				}
				reply([]byte(strings.Join(paths, "\n")), err)
			})
		}}
	case "files":
		// args: u8 multiple, then MIME types a line each; result: JSON of
		// the copies (none: cancelled).
		return map[string]asyncMethod{"pick": func(args []byte, reply func([]byte, error)) {
			multiple := len(args) > 0 && args[0] == 1
			var mimes []string
			if len(args) > 1 {
				mimes = strings.Split(string(args[1:]), "\n")
			}
			h.pickFiles(fileFilters(mimes), multiple, 0, func(files []pickedFile, err error) {
				if err != nil || len(files) == 0 {
					reply(nil, err)
					return
				}
				b, err := json.Marshal(files)
				reply(b, err)
			})
		}}
	}
	return nil
}

// devTools calls a DevTools protocol method in w's page, once it has
// one, and answers with its result's JSON, on the main thread.
func (w *web) devTools(method string, params any, done func(json.RawMessage, error)) {
	w.do(func() { devTools(w.view, method, params, done) })
}

// devTools calls a DevTools protocol method in view's page
// (ICoreWebView2's CallDevToolsProtocolMethod).
func devTools(view uintptr, method string, params any, done func(json.RawMessage, error)) {
	p, err := json.Marshal(params)
	if err != nil {
		done(nil, err)
		return
	}
	hr := WithHandler(func(hr, result uintptr) {
		// Answered on a later turn: what the answer does (closing the
		// view) mustn't happen inside WebView2's callback.
		var err error
		var r json.RawMessage
		if Failed(hr) {
			err = fmt.Errorf("WebView2: %s failed (0x%x)", method, uint32(hr))
		} else {
			r = json.RawMessage(GoStr(result))
		}
		theHost.Post(func() { done(r, err) })
	}, func(handler uintptr) uintptr {
		return ComCall(view, wvCallDevToolsProtocolMethod, Str(method), Str(string(p)), handler)
	})
	if Failed(hr) {
		done(nil, fmt.Errorf("WebView2: %s failed (0x%x)", method, uint32(hr)))
	}
}

// evaluate runs script in w's page, as evaluate does.
func (w *web) evaluate(script string, done func([]byte, error)) {
	w.do(func() { evaluate(w.view, script, done) })
}

// evaluate runs script in view's page as a script of its own (the last
// statement's value, a promise's awaited) and answers with its value as
// JSON, or what it threw.
func evaluate(view uintptr, script string, done func([]byte, error)) {
	devTools(view, "Runtime.evaluate", map[string]any{"expression": script, "awaitPromise": true, "returnByValue": true},
		func(r json.RawMessage, err error) {
			if err != nil {
				done(nil, err)
				return
			}
			done(evaluated(r))
		})
}

// evaluated is Runtime.evaluate's answer: the value's JSON (null for
// undefined), or what was thrown.
func evaluated(r json.RawMessage) ([]byte, error) {
	var res struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(r, &res); err != nil {
		return nil, err
	}
	if e := res.Exception; e != nil {
		msg := e.Text
		if e.Exception != nil && e.Exception.Description != "" {
			msg = e.Exception.Description
			if i := strings.IndexByte(msg, '\n'); i > 0 {
				msg = msg[:i] // the message, not the stack
			}
		}
		return nil, errors.New(msg)
	}
	if len(res.Result.Value) == 0 {
		return []byte("null"), nil // undefined
	}
	return res.Result.Value, nil
}

// webModules are web and js.
func (h *Host) webModules(name string) map[string]asyncMethod {
	switch name {
	case "web":
		return map[string]asyncMethod{
			// args: u32 a WebView's node id, then the script.
			"eval": func(args []byte, reply func([]byte, error)) {
				if len(args) < 4 {
					reply(nil, errors.New("bad arguments"))
					return
				}
				n := h.nodes[ops.NodeID(binary.LittleEndian.Uint32(args))]
				if n == nil || n.web == nil || n.web.video != nil {
					reply(nil, errors.New("not a WebView"))
					return
				}
				n.web.evaluate(string(args[4:]), reply)
			},
			// args: a URL; result: the WebViews' cookies for it, as a
			// Cookie header (the environment's store, which every WebView
			// shares: one is asked).
			"cookies": func(args []byte, reply func([]byte, error)) {
				var w *web
				for _, n := range h.nodes {
					if n.web != nil && !n.web.closed {
						w = n.web
						break
					}
				}
				if w == nil {
					reply(nil, nil) // no WebView has been: no cookies
					return
				}
				w.devTools("Network.getCookies", map[string]any{"urls": []string{string(args)}}, func(r json.RawMessage, err error) {
					if err != nil {
						reply(nil, err)
						return
					}
					var res struct {
						Cookies []struct{ Name, Value string } `json:"cookies"`
					}
					json.Unmarshal(r, &res)
					parts := make([]string, 0, len(res.Cookies))
					for _, c := range res.Cookies {
						parts = append(parts, c.Name+"="+c.Value)
					}
					reply([]byte(strings.Join(parts, "; ")), nil)
				})
			},
		}
	case "js":
		// args: u32 timeout ms, then the script.
		return map[string]asyncMethod{"eval": h.runScript}
	}
	return nil
}

// runScript runs a ryui/js script in a WebView2 that's never shown (a
// blank page in the host's hidden window), and lets it go after.
func (h *Host) runScript(args []byte, reply func([]byte, error)) {
	if len(args) < 4 {
		reply(nil, errors.New("bad arguments"))
		return
	}
	ms := binary.LittleEndian.Uint32(args)
	src := string(args[4:])
	var page *printPage
	answered := false
	end := func(b []byte, err error) {
		if answered {
			return
		}
		answered = true
		if page != nil {
			page.close()
		}
		reply(b, err)
	}
	time.AfterFunc(time.Duration(ms)*time.Millisecond, func() {
		h.Post(func() { end(nil, errors.New("timeout")) })
	})
	h.loadPrintPage("<!doctype html><html></html>", func(p *printPage, err error) {
		if err != nil {
			end(nil, err)
			return
		}
		if answered {
			p.close() // timed out meanwhile
			return
		}
		page = p
		evaluate(p.view, src, end)
	})
}
