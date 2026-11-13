//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/webauth on Windows: the sign-in page in a window of its own over
// the window in use (which waits, as for a sheet), a WebView2 in it, as
// ASWebAuthenticationSession's sheet is on a Mac. It shares the app's
// WebView2 profile (the user's sessions there), or, Ephemeral, has a
// profile of its own, thrown away after. It ends when the page goes to a
// URL of the callback scheme (that navigation is stopped and its URL is
// the answer), when the app cancels it, or when the user closes the
// window ("canceled").
//
// "start": u32 the session's id, u8 flags (1 ephemeral), string the URL,
// string the callback scheme; "cancel": u32 the id.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["webAuth"] = h.webAuthModule() })
}

type webAuthSession struct {
	id         uint32
	d          *dialog
	controller uintptr
	view       uintptr
	env        uintptr // its own environment (ephemeral), or 0: the app's
	dir        string  // the ephemeral profile's folder
	scheme     string
	reply      func([]byte, error)
	done       bool
}

var webAuths = map[uint32]*webAuthSession{}

const navStartingPutCancel = 8

func (h *Host) webAuthModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"start": func(args []byte, reply func([]byte, error)) {
			if len(args) < 13 {
				reply(nil, errors.New("bad args"))
				return
			}
			id, flags := binary.LittleEndian.Uint32(args), args[4]
			link, rest := readString(args[5:])
			if rest == nil {
				reply(nil, errors.New("bad args"))
				return
			}
			scheme, rest := readString(rest)
			if rest == nil {
				reply(nil, errors.New("bad args"))
				return
			}
			u, err := url.Parse(link)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				reply(nil, errors.New("not a web URL"))
				return
			}
			if old := webAuths[id]; old != nil {
				old.finish(nil, errors.New("canceled"))
			}
			s := &webAuthSession{id: id, scheme: strings.ToLower(scheme), reply: reply}
			webAuths[id] = s
			h.startWebAuth(s, link, u.Host, flags&1 != 0)
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				if s := webAuths[binary.LittleEndian.Uint32(args)]; s != nil {
					s.finish(nil, errors.New("canceled"))
				}
			}
			reply(nil, nil)
		},
	}
}

func (h *Host) startWebAuth(s *webAuthSession, link, title string, ephemeral bool) {
	s.d = h.newDialog(title, 480, 640, func(d *dialog, m uint32, _, _ uintptr) (uintptr, bool) {
		if m == WM_SIZE && s.controller != 0 {
			ComCallRect(s.controller, ctlPutBounds, ClientRect(d.hwnd))
		}
		return 0, false
	})
	s.d.onClose = func() { s.finish(nil, errors.New("canceled")) }
	if !snapshotting() {
		s.d.show()
	} else {
		ShowWindow(s.d.hwnd, SW_SHOWNOACTIVATE)
	}
	withEnv := func(env uintptr) {
		if s.done {
			return
		}
		if env == 0 {
			s.finish(nil, errors.New("couldn't start the sign-in: WebView2 isn't there"))
			return
		}
		hr := WithHandler(func(hr, controller uintptr) {
			if Failed(hr) || controller == 0 || s.done {
				if !s.done {
					s.finish(nil, errors.New("couldn't start the sign-in"))
				}
				return
			}
			AddRef(controller)
			s.controller = controller
			ComCall(controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&s.view)))
			ComCallRect(controller, ctlPutBounds, ClientRect(s.d.hwnd))
			var tok int64
			add := func(index int, fn func(a, b uintptr)) {
				WithHandler(fn, func(handler uintptr) uintptr {
					return ComCall(s.view, index, handler, uintptr(unsafe.Pointer(&tok)))
				})
			}
			add(wvAddNavigationStarting, func(_, args uintptr) {
				var p uintptr
				ComCall(args, navStartingGetURI, uintptr(unsafe.Pointer(&p)))
				to := TakeString(p)
				if s.scheme == "" || s.done {
					return
				}
				if i := strings.IndexByte(to, ':'); i > 0 && strings.ToLower(to[:i]) == s.scheme {
					ComCall(args, navStartingPutCancel, 1)
					h.Post(func() { s.finish([]byte(to), nil) })
				}
			})
			add(wvAddNewWindowRequested, func(_, args uintptr) {
				// A popup (a provider's "sign in with...") opens here.
				var p uintptr
				ComCall(args, newWinGetURI, uintptr(unsafe.Pointer(&p)))
				to := TakeString(p)
				ComCall(args, newWinPutHandled, 1)
				ComCall(s.view, wvNavigate, Str(to))
			})
			add(wvAddDocumentTitleChanged, func(_, _ uintptr) {
				var p uintptr
				ComCall(s.view, wvGetDocumentTitle, uintptr(unsafe.Pointer(&p)))
				if t := TakeString(p); t != "" && !s.done {
					SetWindowText(s.d.hwnd, t)
				}
			})
			ComCall(controller, ctlPutIsVisible, 1)
			ComCall(s.view, wvNavigate, Str(link))
		}, func(handler uintptr) uintptr {
			return ComCall(env, envCreateController, s.d.hwnd, handler)
		})
		if Failed(hr) {
			s.finish(nil, errors.New("couldn't start the sign-in"))
		}
	}
	if !ephemeral {
		h.whenWebEnvironment(func() { withEnv(webEnv) })
		return
	}
	// A profile of its own, in a new folder, thrown away after.
	base := filepath.Join(os.Getenv("LOCALAPPDATA"), h.appName, "WebView2-signin")
	os.MkdirAll(base, 0o755)
	dir, err := os.MkdirTemp(base, "ephemeral-")
	if err != nil {
		s.finish(nil, err)
		return
	}
	s.dir = dir
	if err := CreateWebView2Environment(dir, func(env uintptr, err error) {
		if err != nil || env == 0 {
			withEnv(0)
			return
		}
		s.env = env
		withEnv(env)
	}); err != nil {
		withEnv(0)
	}
}

// finish answers the session, once, and closes its window (on a later
// turn: it's often called from WebView2's callbacks).
func (s *webAuthSession) finish(b []byte, err error) {
	if s.done {
		return
	}
	s.done = true
	if webAuths[s.id] == s {
		delete(webAuths, s.id)
	}
	s.reply(b, err)
	controller, view, env, dir, d := s.controller, s.view, s.env, s.dir, s.d
	s.controller, s.view, s.env = 0, 0, 0
	theHost.Post(func() {
		if controller != 0 {
			ComCall(controller, ctlClose)
			Release(view)
			Release(controller)
		}
		if d != nil {
			d.close()
		}
		Release(env)
		if dir != "" {
			// The browser's processes let the folder go once they've
			// exited.
			go func() {
				for i := 0; i < 20; i++ {
					if os.RemoveAll(dir) == nil {
						return
					}
					time.Sleep(500 * time.Millisecond)
				}
			}()
		}
	})
}
