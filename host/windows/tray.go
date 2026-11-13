//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// asuka/tray on Windows: icons in the notification area
// (Shell_NotifyIcon, NOTIFYICON_VERSION_4), owned by a hidden top-level
// window of the modules' class (a message-only window doesn't get
// TaskbarCreated, the message that says Explorer started again and the
// icons are to be added again). A click (NIN_SELECT, or Enter or Space
// on the focused icon) is the icon's OnClick, or opens its menu if it
// has none; a right click (WM_CONTEXTMENU) opens the menu, a popup menu
// built as the menu bar's (submenus, checks, destructive items drawn
// red). The icon is the image, fitted to a small icon's size at the
// system's DPI, or the symbol's Segoe Fluent Icons glyph; a symbol, and
// a template image, are drawn white on a dark taskbar and black on a
// light one, and drawn again when it turns. Without either, the app's
// icon. Windows shows no text beside an icon (Title).
//
// The Dock menu is the Tasks of the app's jump list (ICustomDestinationList):
// a link per item, starting the app with --asuka-dock-item=<number>. A
// running app that runs once (asuka.json's id) is handed the number by
// the launch (deeplink_windows.go); one that isn't running gets it as it
// starts. The jump list has separators, but no submenus, checks or
// disabled items: those items are left out, as is a quit item.

const (
	trayMessage = WM_APP + 0x54 // an icon's events
	smCxSmIcon  = 49            // SM_CXSMICON
	trayQueue   = 64            // what happens, kept while nothing asks
)

// launchDockItem is the Dock menu item the app was started for (a jump
// list task), -1 if none: taken out of os.Args before the app sees them.
var launchDockItem = -1

func init() {
	os.Args, launchDockItem = takeDockItem(os.Args)
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["tray"] = h.trayModule() })
}

// trayState is the tray module's: its icons, the Dock menu, and what
// happened that Go hasn't asked for yet.
type trayState struct {
	hwnd    uintptr
	created uint32 // TaskbarCreated
	icons   map[uint32]*trayIcon
	order   []uint32 // the icons' IDs, in the order they came
	dock    []ops.MenuItem
	queue   [][]byte
	waiting func([]byte, error)
	light   bool // the taskbar is light
}

// trayIcon is an icon in the notification area.
type trayIcon struct {
	id                     uint32
	flags                  byte // 1 template, 2 clickable
	image, symbol, tooltip string
	menu                   []ops.MenuItem
	ni                     NotifyIcon
	ownIcon                bool // ni.Icon is the host's, to destroy
	added                  bool
	ready                  bool // drawn: its image loaded, or none
}

// trays is the tray module's state, made with its window the first time.
func (h *Host) trays() *trayState {
	if h.tray != nil {
		return h.tray
	}
	s := &trayState{icons: map[uint32]*trayIcon{}, created: RegisterWindowMessage("TaskbarCreated"), light: SystemUsesLightTheme()}
	h.tray = s
	s.hwnd = ownWindow(0, "", WS_POPUP, 0, 0, 0, 0, 0, func(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
		return h.trayWindowMessage(m, wp, lp)
	})
	return s
}

func (h *Host) trayModule() map[string]asyncMethod {
	le := binary.LittleEndian
	return map[string]asyncMethod{
		"supported": func(_ []byte, reply func([]byte, error)) { reply([]byte{1}, nil) },
		"set": func(args []byte, reply func([]byte, error)) {
			if len(args) < 5 {
				reply(nil, errors.New("tray.set: bad arguments"))
				return
			}
			s := h.trays()
			id, flags, b := le.Uint32(args), args[4], args[5:]
			str := func() string {
				if len(b) < 4 || int(le.Uint32(b)) > len(b)-4 {
					b = nil
					return ""
				}
				n := int(le.Uint32(b))
				v := string(b[4 : 4+n])
				b = b[4+n:]
				return v
			}
			image, symbol, _, tooltip, _ := str(), str(), str(), str(), str() // no title on Windows; the ID is its place
			t := s.icons[id]
			if t == nil {
				t = &trayIcon{id: id, ni: NotifyIcon{Wnd: s.hwnd, ID: id, Message: trayMessage}}
				s.icons[id] = t
				s.order = append(s.order, id)
			}
			redraw := t.image != image || t.symbol != symbol || t.flags&1 != flags&1 || !t.ready
			t.flags, t.image, t.symbol, t.tooltip = flags, image, symbol, tooltip
			t.menu = ops.DecodeMenu(b)
			t.ni.Tip = tooltip
			if redraw {
				h.trayDraw(t)
			} else {
				h.trayShow(t)
			}
			reply(nil, nil)
		},
		"remove": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 && h.tray != nil {
				h.trayRemove(h.tray.icons[le.Uint32(args)])
			}
			reply(nil, nil)
		},
		"dock": func(args []byte, reply func([]byte, error)) {
			s := h.trays()
			s.dock = ops.DecodeMenu(args)
			reply(nil, h.setJumpList(s.dock))
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			s := h.trays()
			if len(s.queue) > 0 {
				e := s.queue[0]
				s.queue = s.queue[1:]
				reply(e, nil)
				return
			}
			if w := s.waiting; w != nil {
				w(nil, errors.New("stopped")) // a newer one waits instead
			}
			s.waiting = reply
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if s := h.tray; s != nil && s.waiting != nil {
				w := s.waiting
				s.waiting = nil
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// trayEvent reports what happened to icon id (0: the Dock menu): kind 1
// a click, 2 the menu's item numbered index chosen.
func (h *Host) trayEvent(id uint32, kind byte, index int) {
	s := h.trays()
	b := binary.LittleEndian.AppendUint32(nil, id)
	b = append(b, kind)
	b = binary.LittleEndian.AppendUint32(b, uint32(index))
	if w := s.waiting; w != nil {
		s.waiting = nil
		w(b, nil)
		return
	}
	if len(s.queue) < trayQueue {
		s.queue = append(s.queue, b)
	}
}

// trayDraw makes the icon's picture again: its image once it's loaded,
// or its symbol.
func (h *Host) trayDraw(t *trayIcon) {
	if t.image == "" {
		h.trayPicture(t, nil)
		return
	}
	src := t.image
	h.fetchImage(src, func(d *decoded) {
		s := h.tray
		if s == nil || s.icons[t.id] != t || t.image != src {
			return
		}
		if d.err != nil {
			fmt.Fprintf(os.Stderr, "asuka: tray icon %s: %v\n", src, d.err)
			d = nil
		}
		h.trayPicture(t, d)
	})
}

// trayPicture sets the icon's picture from its image (d), else its
// symbol, else the app's icon, and shows it.
func (h *Host) trayPicture(t *trayIcon, d *decoded) {
	s := h.tray
	size := int32(SystemMetric(smCxSmIcon, DPI(s.hwnd)))
	if size <= 0 {
		size = 16
	}
	var ink [3]byte // BGR: black on a light taskbar
	if !s.light {
		ink = [3]byte{255, 255, 255}
	}
	var px []byte
	switch {
	case d != nil && d.w > 0 && d.h > 0:
		px = fitIcon(d.pixels, d.w, d.h, size)
		if t.flags&1 != 0 {
			for i := 0; i < len(px); i += 4 {
				px[i], px[i+1], px[i+2] = ink[0], ink[1], ink[2]
			}
		}
	case t.symbol != "":
		if g := symbolGlyph(t.symbol); g != 0 {
			cov := GlyphCoverage(g, h.iconFace(), size)
			px = make([]byte, len(cov)*4)
			for i, a := range cov {
				px[i*4], px[i*4+1], px[i*4+2], px[i*4+3] = ink[0], ink[1], ink[2], a
			}
		} else {
			warnSymbol(t.symbol)
		}
	}
	old, own := t.ni.Icon, t.ownIcon
	t.ni.Icon, t.ownIcon = AppIcon(), false
	if px != nil {
		if icon := IconFromPixels(px, size, size); icon != 0 {
			t.ni.Icon, t.ownIcon = icon, true
		}
	}
	t.ready = true
	h.trayShow(t)
	if own {
		DestroyIcon(old)
	}
}

// trayShow adds the icon to the notification area, or sets it there.
func (h *Host) trayShow(t *trayIcon) {
	if !t.ready {
		return
	}
	if t.added {
		t.ni.Modify()
		return
	}
	t.added = t.ni.Add()
}

// trayRemove takes an icon away.
func (h *Host) trayRemove(t *trayIcon) {
	s := h.tray
	if t == nil || s == nil {
		return
	}
	if t.added {
		t.ni.Delete()
		t.added = false
	}
	if t.ownIcon {
		DestroyIcon(t.ni.Icon)
		t.ni.Icon, t.ownIcon = 0, false
	}
	delete(s.icons, t.id)
	for i, id := range s.order {
		if id == t.id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// removeTrayIcons takes every icon away (the app quits).
func (h *Host) removeTrayIcons() {
	if h.tray == nil {
		return
	}
	for _, id := range append([]uint32(nil), h.tray.order...) {
		h.trayRemove(h.tray.icons[id])
	}
}

// trayWindowMessage handles the icons' window's messages: their events,
// Explorer starting again, the taskbar turning light or dark, and the
// owner-drawn items of their menus.
func (h *Host) trayWindowMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	s := h.tray
	if s == nil {
		return 0, false
	}
	switch {
	case m == trayMessage:
		t := s.icons[uint32(HiWord(lp))]
		if t == nil {
			return 0, true
		}
		x, y := int32(int16(LoWord(wp))), int32(int16(HiWord(wp)))
		switch uint32(LoWord(lp)) {
		case NIN_SELECT, NIN_KEYSELECT:
			if t.flags&2 != 0 {
				h.trayEvent(t.id, 1, 0)
			} else {
				h.trayMenu(t, x, y)
			}
		case WM_CONTEXTMENU:
			h.trayMenu(t, x, y)
		}
		return 0, true
	case s.created != 0 && m == s.created:
		// Explorer started again: the icons are gone from it.
		for _, id := range s.order {
			if t := s.icons[id]; t != nil {
				t.added = false
				h.trayShow(t)
			}
		}
		return 0, true
	case m == WM_SETTINGCHANGE:
		if GoStr(lp) == "ImmersiveColorSet" {
			if light := SystemUsesLightTheme(); light != s.light {
				s.light = light
				for _, id := range s.order {
					if t := s.icons[id]; t != nil && (t.symbol != "" || t.flags&1 != 0) {
						h.trayDraw(t)
					}
				}
			}
		}
		return 0, false
	case m == WM_MEASUREITEM || m == WM_DRAWITEM || m == WM_MENUCHAR || m == WM_INITMENUPOPUP || m == WM_INITMENU || m == WM_ENTERMENULOOP || m == WM_EXITMENULOOP:
		return h.menuItemMessage(s.hwnd, m, wp, lp)
	}
	return 0, false
}

// trayMenu opens an icon's menu at the point its event gave (on the
// taskbar), as the system's tray menus open: above and from the point
// (leftward, right to left). The menu runs a modal loop, so it shows
// once the core has returned.
func (h *Host) trayMenu(t *trayIcon, x, y int32) {
	if len(t.menu) == 0 {
		return
	}
	id := t.id
	h.afterCore(func() {
		s := h.tray
		t := s.icons[id]
		if t == nil {
			return
		}
		order, flags := uint32(0), uint32(TPM_RIGHTBUTTON|TPM_BOTTOMALIGN)
		if h.rtl {
			order = MF_RIGHTORDER
			flags |= TPM_RIGHTALIGN | TPM_LAYOUTRTL
		}
		b := &menuBuild{h: h, base: 1, order: order}
		m := b.popup(t.menu)
		// The menu closes when the user clicks away only if its owner is
		// the foreground window.
		SetForegroundWindow(s.hwnd)
		h.modal++
		if len(b.owned) > 0 {
			h.menuLoop(true)
		}
		got := TrackPopupMenu(m, flags, x, y, s.hwnd)
		h.menuLoop(false)
		h.modal--
		PostMessage(s.hwnd, WM_NULL, 0, 0)
		DestroyMenu(m)
		for _, om := range b.owned {
			h.forget(om)
		}
		if got > 0 {
			h.trayChose(t, got-1)
		}
	})
}

// trayChose is an icon's menu item chosen (its number): reported, or a
// quit item quits.
func (h *Host) trayChose(t *trayIcon, index int) {
	flat := ops.FlattenMenu(t.menu)
	if index < 0 || index >= len(flat) {
		return
	}
	switch it := flat[index]; {
	case it.Flags&(ops.MenuSeparator|ops.MenuDisabled|ops.MenuSubmenu) != 0:
	case it.Role == ops.MenuRoleQuit:
		h.quit()
	default:
		h.trayEvent(t.id, 2, it.Index)
	}
}

// setJumpList makes the Dock menu's items the Tasks of the app's jump
// list (not for tests and automation runs, which would change the
// user's).
func (h *Host) setJumpList(items []ops.MenuItem) error {
	if testing.Testing() || notItself(os.Getenv) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return SetJumpListTasks(jumpTasks(items, exe))
}

// jumpTasks are the jump list's tasks for the Dock menu's items: each
// starts exe for its item; separators between them, never first, last or
// two together; what the jump list can't show is left out.
func jumpTasks(items []ops.MenuItem, exe string) []JumpTask {
	var tasks []JumpTask
	sep := false
	for _, it := range items {
		switch {
		case it.Is(ops.MenuSeparator):
			sep = len(tasks) > 0
			continue
		case it.Flags&(ops.MenuDisabled|ops.MenuSubmenu) != 0, it.Role != ops.MenuRoleNone, it.Title == "":
			continue
		}
		if sep {
			tasks = append(tasks, JumpTask{Separator: true})
			sep = false
		}
		tasks = append(tasks, JumpTask{Title: it.Title, Path: exe, Args: dockItemArg + itoa(it.Index)})
	}
	return tasks
}
