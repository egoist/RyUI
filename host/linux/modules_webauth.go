//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/l10n"
)

// asuka/sdk/webauth on Linux, as ASWebAuthenticationSession on a Mac: a
// dialog over the window in use with a WebKitGTK web view (opened as the
// app's WebViews open it) that loads the URL; the session finishes when
// a page navigates to the callback scheme (the navigation is stopped and
// its URL is the answer), and is canceled as the dialog closes (its
// Cancel button, Escape) or Cancel is called. Its cookies are WebKit's
// default session's (the app's web views'), or, with Ephemeral, a
// session of its own, gone with it.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["webAuth"] = h.webAuthModule()
	})
}

type webAuthSession struct {
	id     uint32
	reply  func([]byte, error)
	dialog Ptr
	view   Ptr
	title  Ptr // the header's AdwWindowTitle
	scheme string
	done   bool
}

var webAuthSessions = map[uint32]*webAuthSession{}

func decodeWebAuth(args []byte) (id uint32, flags byte, link, scheme string, ok bool) {
	if len(args) < 13 {
		return
	}
	id, flags = binary.LittleEndian.Uint32(args), args[4]
	link, rest := readString(args[5:])
	if rest == nil {
		return
	}
	scheme, rest = readString(rest)
	return id, flags, link, scheme, rest != nil
}

func (h *Host) webAuthModule() map[string]method {
	return map[string]method{
		"start": func(args []byte, reply func([]byte, error)) {
			id, flags, link, scheme, good := decodeWebAuth(args)
			if !good {
				reply(nil, errors.New("bad args"))
				return
			}
			if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
				reply(nil, errors.New("not a web URL"))
				return
			}
			if err := webKit(); err != nil {
				reply(nil, errors.New("signing in on the web needs WebKitGTK: "+err.Error()))
				return
			}
			if old := webAuthSessions[id]; old != nil {
				old.finish(nil, errors.New("canceled"))
			}
			s := &webAuthSession{id: id, reply: reply, scheme: strings.ToLower(strings.TrimSuffix(scheme, ":"))}
			webAuthSessions[id] = s
			h.showWebAuth(s, link, flags&1 != 0)
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				if s := webAuthSessions[binary.LittleEndian.Uint32(args)]; s != nil {
					s.finish(nil, errors.New("canceled"))
				}
			}
			reply(nil, nil)
		},
	}
}

func (h *Host) showWebAuth(s *webAuthSession, link string, ephemeral bool) {
	if ephemeral {
		s.view = WebKitWebViewNewEphemeral()
	} else {
		s.view = WebKitWebViewNew()
	}
	ObjectRefSink(s.view)
	WidgetSetVExpand(s.view, true)
	s.dialog = ObjectRefSink(AdwDialogNew())
	AdwDialogSetContentWidth(s.dialog, 560)
	AdwDialogSetContentHeight(s.dialog, 680)
	tv := AdwToolbarViewNew()
	bar := AdwHeaderBarNew()
	AdwHeaderBarSetShowStartTitleButtons(bar, false)
	AdwHeaderBarSetShowEndTitleButtons(bar, false)
	cancel := ButtonNew()
	ButtonSetLabel(cancel, l10n.Cancel(h.locale))
	AdwHeaderBarPackStart(bar, cancel)
	s.title = AdwWindowTitleNew("", hostOf(link))
	AdwHeaderBarSetTitleWidget(bar, s.title)
	AdwToolbarViewAddTopBar(tv, bar)
	AdwToolbarViewSetContent(tv, s.view)
	AdwDialogSetChild(s.dialog, tv)

	Connect(cancel, "clicked", func() { s.finish(nil, errors.New("canceled")) })
	Connect(s.dialog, "closed", func() { s.finish(nil, errors.New("canceled")) })
	Connect(s.view, "decide-policy", func(d, kind uintptr) bool {
		if kind != WebKitPolicyNavigation && kind != WebKitPolicyNewWindow {
			return false
		}
		u := WebKitNavigationURI(d)
		if !s.isCallback(u) {
			return false
		}
		WebKitPolicyDecisionIgnore(d)
		h.Post(func() { s.finish([]byte(u), nil) })
		return true
	})
	Connect(s.view, "notify::title", func(uintptr) {
		AdwWindowTitleSetTitle(s.title, GoString(WebKitWebViewGetTitle(s.view)))
	})
	Connect(s.view, "notify::uri", func(uintptr) {
		Dev.AdwWindowTitleSetSubtitle(s.title, hostOf(GoString(WebKitWebViewGetURI(s.view))))
	})
	AdwDialogPresent(s.dialog, h.dialogParent())
	WebKitWebViewLoadURI(s.view, link)
}

// isCallback reports whether a URL is the callback: its scheme's the
// session's.
func (s *webAuthSession) isCallback(u string) bool {
	scheme, _, ok := strings.Cut(u, ":")
	return ok && s.scheme != "" && strings.ToLower(scheme) == s.scheme
}

// finish closes the dialog and answers, once.
func (s *webAuthSession) finish(b []byte, err error) {
	if s.done {
		return
	}
	s.done = true
	if webAuthSessions[s.id] == s {
		delete(webAuthSessions, s.id)
	}
	WebKitWebViewStopLoading(s.view)
	AdwDialogForceClose(s.dialog)
	ObjectUnref(s.dialog)
	ObjectUnref(s.view)
	s.reply(b, err)
}

// hostOf is a URL's host, as the dialog's subtitle shows it.
func hostOf(u string) string {
	_, rest, ok := strings.Cut(u, "://")
	if !ok {
		return ""
	}
	host, _, _ := strings.Cut(rest, "/")
	if i := strings.LastIndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	return host
}
