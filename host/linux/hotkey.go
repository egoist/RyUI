//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"
	"os"
	"sort"
	"strings"
	"unsafe"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/hotkey on Linux, two ways:
//
//   - Wayland: apps can't see keys pressed in other apps; the desktop
//     binds shortcuts for an app that asks the XDG desktop portal's
//     GlobalShortcuts (GNOME 48, KDE Plasma 6, Hyprland's portal). A
//     session binds its shortcuts once (BindShortcuts), so each change
//     closes it and binds them all in a new one; the desktop remembers
//     the keys the user picks by the shortcut's id (its keys, as the
//     portal writes them), so binding the same ones again asks nothing.
//     The desktop may ask the user to confirm (declined: "not allowed")
//     and knows apps by their desktop entry (an app it can't place:
//     "not allowed"). Presses are its Activated signal; the window
//     OnPress presents or focuses takes the focus with the press's
//     activation token, as Wayland asks. Without the portal (GNOME
//     before 48, xdg-desktop-portal-gtk alone): "not available"; so
//     under XWayland, whose grabs see only X11 windows' keys.
//   - X11: the app grabs the keys on the root window (XGrabKey, with
//     Caps Lock's and Num Lock's states too), which fails while another
//     client has them ("taken"); the presses come through GDK's xevent
//     signal. A key held down repeats as presses without releases (GDK
//     asks for detectable autorepeat) or as releases and presses at the
//     same time: neither is a press again.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["hotkey"] = h.hotkeyModule() })
}

// hotkeyPortalOnly binds through the portal under X11 too (tests).
var hotkeyPortalOnly bool

// hotkeyReg is a registration: its keys, the X11 grab's, or the portal's
// shortcut and the answer waiting for its binding.
type hotkeyReg struct {
	id         uint32
	mods       byte
	key, desc  string
	keycode    uint8
	xmods      uint32
	down       bool   // X11: pressed, not released yet
	releasedAt uint32 // X11: the last release's time
	trigger    string // the portal's: CTRL+SHIFT+space
	reply      func([]byte, error)
}

var hotkeys struct {
	host    *Host
	way     int // 0 not chosen, hotkeysX11, hotkeysPortal, hotkeysNone
	regs    map[uint32]*hotkeyReg
	pressed []byte
	token   string // the portal's: the last press's activation token
	waiting func([]byte, error)
	xevent  uint64 // X11: the xevent handler
	portal  hotkeyPortal
}

const (
	hotkeysX11 = 1 + iota
	hotkeysPortal
	hotkeysNone
)

// hotkeyModule: "register" (u32 id, u8 mods, u32 key length, the key,
// the description the portal shows), "unregister" (u32 id), "next"
// (answered with the ids pressed since, u32 each, once there are some)
// and "stop" (fails the waiting "next" with "stopped").
func (h *Host) hotkeyModule() map[string]method {
	hotkeys.host = h
	return map[string]method{
		"register": func(args []byte, reply func([]byte, error)) {
			if len(args) < 9 || int(le.Uint32(args[5:])) > len(args)-9 {
				reply(nil, errors.New("hotkey.register: bad arguments"))
				return
			}
			n := le.Uint32(args[5:])
			r := &hotkeyReg{id: le.Uint32(args), mods: args[4], key: string(args[9 : 9+n]), desc: string(args[9+n:])}
			h.hotkeyWay(func(way int) {
				switch way {
				case hotkeysX11:
					reply(nil, grabHotkey(r))
				case hotkeysPortal:
					h.bindHotkey(r, reply)
				default:
					reply(nil, errors.New("not available"))
				}
			})
		},
		"unregister": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				if r := hotkeys.regs[le.Uint32(args)]; r != nil {
					delete(hotkeys.regs, r.id)
					switch hotkeys.way {
					case hotkeysX11:
						ungrabHotkey(r)
					case hotkeysPortal:
						if r.reply != nil {
							r.reply(nil, errors.New("unregistered"))
							r.reply = nil
						}
						h.syncHotkeys()
					}
				}
			}
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if w := hotkeys.waiting; w != nil {
				w(nil, errors.New("stopped")) // one poll at a time
			}
			hotkeys.waiting = reply
			flushHotkeys()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			w := hotkeys.waiting
			hotkeys.waiting, hotkeys.pressed, hotkeys.token = nil, nil, ""
			if w != nil {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// hotkeyWay chooses, once, how shortcuts are registered: on a Wayland
// session the portal (none: not available), on X11 the app's grabs.
func (h *Host) hotkeyWay(then func(way int)) {
	if hotkeys.regs == nil {
		hotkeys.regs = map[uint32]*hotkeyReg{}
	}
	if hotkeys.way != 0 {
		then(hotkeys.way)
		return
	}
	display := DisplayGetDefault()
	wayland := IsWayland(display) || os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland"
	if IsX11(display) && !wayland && !hotkeyPortalOnly {
		hotkeys.way = hotkeysX11
		then(hotkeys.way)
		return
	}
	h.hotkeyPortalVersion(func(version uint32) {
		if hotkeys.way == 0 {
			hotkeys.way = hotkeysNone
			if version > 0 {
				hotkeys.way = hotkeysPortal
			}
		}
		then(hotkeys.way)
	})
}

func hotkeyPress(r *hotkeyReg) {
	hotkeys.pressed = binary.LittleEndian.AppendUint32(hotkeys.pressed, r.id)
	flushHotkeys()
}

// flushHotkeys answers the waiting "next" with the presses. With the
// portal's activation token, the windows OnPress presents or focuses
// take the focus with it: it's the display's while the answer is
// dispatched (posted, as the core's flush with it), and cleared after.
func flushHotkeys() {
	if hotkeys.waiting == nil || len(hotkeys.pressed) == 0 {
		return
	}
	w, b, token := hotkeys.waiting, hotkeys.pressed, hotkeys.token
	hotkeys.waiting, hotkeys.pressed, hotkeys.token = nil, nil, ""
	h := hotkeys.host
	if token != "" && h != nil {
		WaylandSetStartupID(DisplayGetDefault(), token)
	}
	w(b, nil)
	if token != "" && h != nil {
		h.Post(func() { WaylandSetStartupID(DisplayGetDefault(), "") })
	}
}

// X11.

// X's modifier masks, and its event types (X.h).
const (
	xShiftMask   = 1 << 0
	xLockMask    = 1 << 1 // Caps Lock
	xControlMask = 1 << 2
	xMod1Mask    = 1 << 3 // Alt
	xMod2Mask    = 1 << 4 // Num Lock
	xKeyPress    = 2
	xKeyRelease  = 3
	xBadAccess   = 10
)

// xLocks are the lock states a grab is made in too, so it works with
// Caps Lock or Num Lock on.
var xLocks = [...]uint32{0, xLockMask, xMod2Mask, xLockMask | xMod2Mask}

// xKeysym is a shortcut key's keysym: GDK's name for it (X's), or the
// character's (Latin-1 as itself, the rest Unicode's).
func xKeysym(key string) uint64 {
	if name := keyName(key); name != "" {
		if ks := X11.StringToKeysym(name); ks != 0 {
			return ks
		}
	}
	rs := []rune(strings.ToLower(key))
	if len(rs) != 1 {
		return 0
	}
	if r := rs[0]; r >= 0x20 && r <= 0x7e || r >= 0xa0 && r <= 0xff {
		return uint64(r)
	} else {
		return 0x01000000 | uint64(r)
	}
}

// grabHotkey grabs r's keys on the root window: "taken" if another
// client has them, "no key" if the keyboard has none.
func grabHotkey(r *hotkeyReg) error {
	display := DisplayGetDefault()
	dpy := X11.XDisplay(display)
	ks := xKeysym(r.key)
	if ks == 0 {
		return errors.New("no key")
	}
	kc := X11.KeysymToKeycode(dpy, ks)
	if kc == 0 {
		return errors.New("no key")
	}
	var mods uint32
	if r.mods&(1|8) != 0 { // Primary, Control
		mods |= xControlMask
	}
	if r.mods&2 != 0 {
		mods |= xShiftMask
	}
	if r.mods&4 != 0 {
		mods |= xMod1Mask
	}
	// A character the key types only with Shift ("+" on a US keyboard).
	if X11.KeycodeToKeysym(dpy, uint32(kc), 0, 0) != ks && X11.KeycodeToKeysym(dpy, uint32(kc), 0, 1) == ks {
		mods |= xShiftMask
	}
	r.keycode, r.xmods = kc, mods
	for _, o := range hotkeys.regs {
		if o.keycode == kc && o.xmods == mods {
			return errors.New("taken")
		}
	}
	root := X11.DefaultRootWindow(dpy)
	trap := func(grab bool) int32 {
		X11.ErrorTrapPush(display)
		for _, lock := range xLocks {
			if grab {
				X11.GrabKey(dpy, int32(kc), mods|lock, root, false, 1, 1) // GrabModeAsync
			} else {
				X11.UngrabKey(dpy, int32(kc), mods|lock, root)
			}
		}
		return X11.ErrorTrapPop(display) // syncs
	}
	if code := trap(true); code != 0 {
		trap(false)
		if code == xBadAccess {
			return errors.New("taken")
		}
		return errors.New("hotkey: XGrabKey failed")
	}
	if hotkeys.xevent == 0 {
		hotkeys.xevent = Connect(display, "xevent", hotkeyXEvent)
	}
	hotkeys.regs[r.id] = r
	return nil
}

func ungrabHotkey(r *hotkeyReg) {
	display := DisplayGetDefault()
	dpy := X11.XDisplay(display)
	root := X11.DefaultRootWindow(dpy)
	X11.ErrorTrapPush(display)
	for _, lock := range xLocks {
		X11.UngrabKey(dpy, int32(r.keycode), r.xmods|lock, root)
	}
	X11.Flush(dpy)
	X11.ErrorTrapPop(display)
}

// hotkeyXEvent is GdkX11Display's xevent: a grabbed key's press is a
// press, its repeats aren't; true takes the event from GDK.
func hotkeyXEvent(xev uintptr) bool {
	ev := unsafe.Pointer(At[int32](xev))
	typ := *(*int32)(ev)
	if typ != xKeyPress && typ != xKeyRelease {
		return false
	}
	// XKeyEvent (LP64): time at 56, state at 80, keycode at 84.
	at := uint32(*(*uint64)(unsafe.Add(ev, 56)))
	state := *(*uint32)(unsafe.Add(ev, 80)) &^ (xLockMask | xMod2Mask)
	kc := uint8(*(*uint32)(unsafe.Add(ev, 84)))
	for _, r := range hotkeys.regs {
		if r.keycode != kc {
			continue
		}
		// The release comes to the grab whichever modifiers are still
		// down (they may be let go first).
		if typ == xKeyRelease {
			if !r.down {
				continue
			}
			r.down, r.releasedAt = false, at
			return true
		}
		if r.xmods != state&(xShiftMask|xControlMask|xMod1Mask|1<<6) {
			continue
		}
		if r.down || at == r.releasedAt && at != 0 {
			r.down = true
			return true // a repeat
		}
		r.down = true
		hotkeyPress(r)
		return true
	}
	return false
}

// The portal.

const shortcutsPortal = "org.freedesktop.portal.GlobalShortcuts"

type hotkeyPortal struct {
	session   string // the session the shortcuts are bound in
	busy      bool   // a session is being made and bound
	stale     bool   // the shortcuts changed meanwhile
	activated uint32 // the Activated signal's subscription
}

// hotkeyPortalVersion is the GlobalShortcuts portal's version, 0 if
// there's none.
func (h *Host) hotkeyPortalVersion(then func(uint32)) {
	conn, err := Bus(BusSession)
	if err != nil {
		then(0)
		return
	}
	DBusCall(conn, portalDest, portalPath, "org.freedesktop.DBus.Properties", "Get", VTuple(VStr(shortcutsPortal), VStr("version")), 5000, func(r Ptr, err error) {
		var v uint32
		if err == nil {
			if args, _ := VariantGo(r).([]any); len(args) > 0 {
				v, _ = args[0].(uint32)
			}
		}
		then(v)
	})
}

// bindHotkey adds r to the shortcuts the portal binds; reply answers
// once they're bound.
func (h *Host) bindHotkey(r *hotkeyReg, reply func([]byte, error)) {
	r.trigger = xdgTrigger(r.key, r.mods)
	if r.trigger == "" {
		reply(nil, errors.New("no key"))
		return
	}
	for _, o := range hotkeys.regs {
		if o.trigger == r.trigger {
			reply(nil, errors.New("taken"))
			return
		}
	}
	p := &hotkeys.portal
	if p.activated == 0 {
		conn, err := Bus(BusSession)
		if err != nil {
			reply(nil, err)
			return
		}
		p.activated = DBusSubscribe(conn, portalDest, shortcutsPortal, "Activated", portalPath, hotkeyActivated)
	}
	r.reply = reply
	hotkeys.regs[r.id] = r
	h.syncHotkeys()
}

// xdgTrigger is a shortcut as the XDG shortcuts specification writes
// it: CTRL+SHIFT+space (the key X's keysym name).
func xdgTrigger(key string, mods byte) string {
	name := keyName(key)
	if name == "" {
		return ""
	}
	var b strings.Builder
	if mods&(1|8) != 0 {
		b.WriteString("CTRL+")
	}
	if mods&4 != 0 {
		b.WriteString("ALT+")
	}
	if mods&2 != 0 {
		b.WriteString("SHIFT+")
	}
	b.WriteString(name)
	return b.String()
}

// syncHotkeys binds the shortcuts in a new session, closing the old: a
// session binds once. While one is on its way, it does so after.
func (h *Host) syncHotkeys() {
	p := &hotkeys.portal
	if p.busy {
		p.stale = true
		return
	}
	conn, err := Bus(BusSession)
	if err != nil {
		h.answerHotkeys(err)
		return
	}
	if p.session != "" {
		DBusCall(conn, portalDest, p.session, "org.freedesktop.portal.Session", "Close", 0, -1, func(Ptr, error) {})
		p.session = ""
	}
	if len(hotkeys.regs) == 0 {
		return
	}
	p.busy, p.stale = true, false
	h.portalRequest(shortcutsPortal, "CreateSession", func(token string) Ptr {
		return VTuple(VDict(map[string]Ptr{
			"handle_token":         VStr(token),
			"session_handle_token": VStr(h.hotkeySessionToken()),
			// KDE Plasma 5 binds the shortcuts given here, as the
			// portal's drafts had it (portals before 1.17 pass them on).
			"shortcuts": hotkeyShortcuts(),
		}))
	}, nil, func(code uint32, results map[string]any, err error) {
		session, _ := results["session_handle"].(string)
		if err != nil || code != 0 || session == "" {
			p.busy = false
			switch {
			case err != nil && strings.Contains(err.Error(), "NotAllowed"):
				err = errors.New("not allowed") // the app has no desktop entry the desktop knows
			case err == nil && code == 1:
				err = errors.New("not allowed")
			case err == nil:
				err = errors.New("hotkey: the desktop portal made no session")
			}
			h.answerHotkeys(err)
			h.resyncHotkeys()
			return
		}
		if len(hotkeys.regs) == 0 || p.stale {
			p.busy = false
			DBusCall(conn, portalDest, session, "org.freedesktop.portal.Session", "Close", 0, -1, func(Ptr, error) {})
			h.resyncHotkeys()
			return
		}
		p.session = session
		h.bindHotkeys(session)
	})
}

// bindHotkeys binds the shortcuts in the new session.
func (h *Host) bindHotkeys(session string) {
	p := &hotkeys.portal
	h.portalRequest(shortcutsPortal, "BindShortcuts", func(token string) Ptr {
		return VTuple(VPath(session), hotkeyShortcuts(), VStr(h.parentWindow()), VDict(map[string]Ptr{"handle_token": VStr(token)}))
	}, nil, func(code uint32, _ map[string]any, err error) {
		p.busy = false
		switch {
		case err != nil:
		case code == 1:
			err = errors.New("not allowed") // the user declined them
		case code != 0:
			err = errors.New("hotkey: the desktop didn't bind the shortcuts")
		}
		if err != nil && p.session == session {
			p.session = ""
			if conn, cerr := Bus(BusSession); cerr == nil {
				DBusCall(conn, portalDest, session, "org.freedesktop.portal.Session", "Close", 0, -1, func(Ptr, error) {})
			}
		}
		// Failed, the new ones are dropped and the ones bound before are
		// bound again, in a session of their own (the desktop asks
		// nothing for keys it has bound for the app).
		if h.answerHotkeys(err) > 0 && err != nil && len(hotkeys.regs) > 0 {
			p.stale = true
		}
		h.resyncHotkeys()
	})
}

// hotkeySessionToken names the portal's session after the app: KDE
// names the shortcuts of an app it has no desktop entry for after it,
// and keeps the keys the user picks for them by it.
func (h *Host) hotkeySessionToken() string {
	return "asuka_" + strings.Map(func(r rune) rune {
		if r < 0x80 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, h.appID)
}

// hotkeyShortcuts are the shortcuts to bind, as the portal takes them
// (a(sa{sv})): each one's id its keys (the desktop remembers the keys the
// user picks by it), with its description and preferred trigger.
func hotkeyShortcuts() Ptr {
	ids := make([]uint32, 0, len(hotkeys.regs))
	for id := range hotkeys.regs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	items := make([]Ptr, 0, len(ids))
	for _, id := range ids {
		r := hotkeys.regs[id]
		desc := r.desc
		if desc == "" {
			desc = r.trigger
		}
		items = append(items, VTuple(VStr(r.trigger), VDict(map[string]Ptr{
			"description":       VStr(desc),
			"preferred_trigger": VStr(r.trigger),
		})))
	}
	return VArray("(sa{sv})", items...)
}

// answerHotkeys answers the registrations waiting for a binding; failed,
// they're dropped. It returns how many it answered.
func (h *Host) answerHotkeys(err error) int {
	n := 0
	for id, r := range hotkeys.regs {
		if r.reply == nil {
			continue
		}
		reply := r.reply
		r.reply = nil
		if err != nil {
			delete(hotkeys.regs, id)
		}
		reply(nil, err)
		n++
	}
	return n
}

// resyncHotkeys binds again if the shortcuts changed while a session was
// on its way.
func (h *Host) resyncHotkeys() {
	if hotkeys.portal.stale {
		hotkeys.portal.stale = false
		h.syncHotkeys()
	}
}

// hotkeyActivated is the portal's Activated: (session, shortcut id,
// timestamp, options).
func hotkeyActivated(s DBusSignal) {
	args, _ := VariantGo(s.Params).([]any)
	if len(args) < 2 {
		return
	}
	session, _ := args[0].(string)
	id, _ := args[1].(string)
	if session == "" || session != hotkeys.portal.session {
		return
	}
	for _, r := range hotkeys.regs {
		if r.trigger == id && r.reply == nil {
			if len(args) >= 4 {
				opts, _ := args[3].(map[string]any)
				hotkeys.token, _ = opts["activation_token"].(string)
			}
			hotkeyPress(r)
			return
		}
	}
}
