//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// asuka/tray on a Mac (the "tray" module; the protocol is in package
// tray): each icon is an NSStatusItem in the menu bar, its button showing
// the image (a template: AppKit colors it as the menu bar is) and the
// title, its menu an NSMenu. Without OnClick the menu is the item's own,
// which AppKit opens on any click; with it, a click is OnClick and a
// secondary click (a right click, a Control click) opens the menu. The
// Dock menu's items are the application delegate's
// (applicationDockMenu:), above the Dock's own.

type trayIcon struct {
	id                             uint32
	item                           ID // the NSStatusItem (retained)
	menu                           ID // its NSMenu (retained), or 0
	flags                          byte
	image, symbol, title, tip, key string
	items                          []ops.MenuItem
	loading                        string // the image being decoded
}

const (
	trayTemplate  = 1
	trayClickable = 2
)

var trayState struct {
	icons   map[uint32]*trayIcon
	dock    []ops.MenuItem
	queue   [][]byte
	waiting func([]byte, error)
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["tray"] = h.trayModule() })
	targetMethods = append(targetMethods,
		Method("trayClicked:", func(self ID, _ SEL, button ID) { theHost.trayClicked(button) }),
		Method("trayMenuItem:", func(self ID, _ SEL, mi ID) {
			tag := uint64(SendInt(mi, "tag"))
			trayHappened(uint32(tag>>32), 2, int(uint32(tag)))
		}),
		Method("dockMenuItem:", func(self ID, _ SEL, mi ID) { trayHappened(0, 2, SendInt(mi, "tag")) }),
		Method("applicationDockMenu:", func(self ID, _ SEL, app ID) ID { return theHost.dockMenu() }),
	)
}

func (h *Host) trayModule() map[string]method {
	s := &trayState
	if s.icons == nil {
		s.icons = map[uint32]*trayIcon{}
	}
	return map[string]method{
		"supported": func(_ []byte, reply func([]byte, error)) { reply([]byte{1}, nil) },
		"set": func(args []byte, reply func([]byte, error)) {
			if len(args) < 5 {
				reply(nil, errors.New("tray.set: bad arguments"))
				return
			}
			id, flags := binary.LittleEndian.Uint32(args), args[4]
			rest := args[5:]
			var strs [5]string
			for i := range strs {
				strs[i], rest = readString(rest)
			}
			WithPool(func() { h.setTrayIcon(id, flags, strs, ops.DecodeMenu(rest)) })
			reply(nil, nil)
		},
		"remove": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				h.removeTrayIcon(binary.LittleEndian.Uint32(args))
			}
			reply(nil, nil)
		},
		"dock": func(args []byte, reply func([]byte, error)) {
			s.dock = ops.DecodeMenu(args)
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if len(s.queue) > 0 {
				e := s.queue[0]
				s.queue = s.queue[1:]
				reply(e, nil)
				return
			}
			s.waiting = reply
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if w := s.waiting; w != nil {
				s.waiting = nil
				w(nil, errors.New("stopped"))
			}
			s.queue = nil
			reply(nil, nil)
		},
	}
}

// trayHappened reports what happened to an icon (id 0: the Dock menu) to
// the "next" waiting, or queues it.
func trayHappened(id uint32, kind byte, index int) {
	s := &trayState
	b := binary.LittleEndian.AppendUint32(nil, id)
	b = append(b, kind)
	b = binary.LittleEndian.AppendUint32(b, uint32(index))
	if w := s.waiting; w != nil {
		s.waiting = nil
		w(b, nil)
		return
	}
	s.queue = append(s.queue, b)
}

// setTrayIcon makes or changes an icon: its image, title, tooltip and
// menu.
func (h *Host) setTrayIcon(id uint32, flags byte, strs [5]string, items []ops.MenuItem) {
	s := &trayState
	t := s.icons[id]
	if t == nil {
		bar := Send(Class("NSStatusBar"), "systemStatusBar")
		item := Retain(MsgFloatID(bar, Sel("statusItemWithLength:"), -1)) // NSVariableStatusItemLength
		t = &trayIcon{id: id, item: item}
		s.icons[id] = t
		if key := strs[4]; key != "" {
			Send(item, "setAutosaveName:", uintptr(String("asuka "+key)))
		}
		button := Send(item, "button")
		Send(button, "setTarget:", uintptr(h.target))
		Send(button, "setAction:", uintptr(Sel("trayClicked:")))
		// The left and right button's up, so the right one is ours too.
		Send(button, "sendActionOn:", 1<<2|1<<4)
		// A changing number's digits keep their width.
		size := MsgFloat(MsgFloatID(Class("NSFont"), Sel("menuBarFontOfSize:"), 0), Sel("pointSize"))
		Send(button, "setFont:", uintptr(MsgFloat2ID(Class("NSFont"), Sel("monospacedDigitSystemFontOfSize:weight:"), size, 0)))
	}
	button := Send(t.item, "button")
	imageChanged := t.image != strs[0] || t.symbol != strs[1] || (t.flags^flags)&trayTemplate != 0
	t.flags = flags
	t.image, t.symbol = strs[0], strs[1]
	if t.title != strs[2] {
		t.title = strs[2]
		Send(button, "setTitle:", uintptr(String(t.title)))
		h.placeTrayImage(t)
	}
	if t.tip != strs[3] {
		t.tip = strs[3]
		Send(button, "setToolTip:", uintptr(String(t.tip)))
	}
	t.key = strs[4]
	if imageChanged {
		h.loadTrayImage(t)
	}
	t.items = items
	if t.menu != 0 {
		Release(t.menu)
		t.menu = 0
	}
	if len(items) > 0 {
		t.menu = Retain(h.statusMenu(items, Sel("trayMenuItem:"), int(id)<<32))
	}
	// Without OnClick, AppKit opens the item's menu on any click.
	if flags&trayClickable == 0 {
		Send(t.item, "setMenu:", uintptr(t.menu))
	} else {
		Send(t.item, "setMenu:", 0)
	}
}

// statusMenu is a menu of items for a status item or the Dock: enabled
// as the app says (not by validation), without shortcuts.
func (h *Host) statusMenu(items []ops.MenuItem, action SEL, tagBase int) ID {
	m := New("NSMenu")
	Send(m, "setAutoenablesItems:", 0)
	h.addMenuItems(m, items, action, tagBase, false)
	setMenuDirection(m, h.rtl)
	return Autorelease(m)
}

// loadTrayImage shows the icon's image, decoded off the main thread, or
// its symbol, or, with neither, the app's icon.
func (h *Host) loadTrayImage(t *trayIcon) {
	button := Send(t.item, "button")
	if t.image == "" {
		t.loading = ""
		img := symbolImage(t.symbol)
		if img != 0 {
			Send(img, "setTemplate:", 1)
		} else if t.symbol == "" {
			img = Send(Send(Class("NSApplication"), "sharedApplication"), "applicationIconImage")
			if img != 0 {
				img = Send(img, "copy")
				Autorelease(img)
				MsgSetSize(img, Sel("setSize:"), Size{Width: 18, Height: 18})
			}
		}
		Send(button, "setImage:", uintptr(img))
		h.placeTrayImage(t)
		return
	}
	src, id := t.image, t.id
	t.loading = src
	go func() {
		img, err := decodeImage(src, h.assets)
		h.Post(func() {
			WithPool(func() {
				t := trayState.icons[id]
				if t == nil || t.loading != src {
					if err == nil {
						Release(img)
					}
					return
				}
				t.loading = ""
				button := Send(t.item, "button")
				if err != nil {
					// The symbol, if any, instead.
					img := symbolImage(t.symbol)
					if img != 0 {
						Send(img, "setTemplate:", 1)
					}
					Send(button, "setImage:", uintptr(img))
					h.placeTrayImage(t)
					return
				}
				// 16 points tall, its aspect kept.
				sz := MsgSize(img, Sel("size"))
				if sz.Height > 0 {
					MsgSetSize(img, Sel("setSize:"), Size{Width: sz.Width * 16 / sz.Height, Height: 16})
				}
				Send(img, "setTemplate:", Bool(t.flags&trayTemplate != 0))
				Send(button, "setImage:", uintptr(img))
				Release(img)
				h.placeTrayImage(t)
			})
		})
	}()
}

// placeTrayImage puts the image before the title (after it, right to
// left), or alone.
func (h *Host) placeTrayImage(t *trayIcon) {
	button := Send(t.item, "button")
	switch {
	case t.title == "":
		Send(button, "setImagePosition:", 1) // NSImageOnly
	case Send(button, "image") == 0:
		Send(button, "setImagePosition:", 0) // NSNoImage
	default:
		Send(button, "setImagePosition:", 7) // NSImageLeading
	}
}

// removeTrayIcon takes an icon out of the menu bar.
func (h *Host) removeTrayIcon(id uint32) {
	s := &trayState
	t := s.icons[id]
	if t == nil {
		return
	}
	delete(s.icons, id)
	WithPool(func() {
		Send(Send(Class("NSStatusBar"), "systemStatusBar"), "removeStatusItem:", uintptr(t.item))
		Release(t.item)
		if t.menu != 0 {
			Release(t.menu)
		}
	})
}

// trayClicked is a click on an icon's button: OnClick, or, a secondary
// click, or any without OnClick, its menu.
func (h *Host) trayClicked(button ID) {
	var t *trayIcon
	for _, x := range trayState.icons {
		if Send(x.item, "button") == button {
			t = x
		}
	}
	if t == nil {
		return
	}
	ev := Send(Send(Class("NSApplication"), "sharedApplication"), "currentEvent")
	secondary := false
	if ev != 0 {
		typ := SendInt(ev, "type")
		secondary = typ == 3 || typ == 4 || SendInt(ev, "modifierFlags")&modControl != 0 // right mouse down, up
	}
	if t.flags&trayClickable != 0 && !secondary {
		trayHappened(t.id, 1, 0)
		return
	}
	if t.menu != 0 {
		h.openTrayMenu(t)
	}
}

// openTrayMenu opens an icon's menu below it, its button highlighted, as
// AppKit opens a status item's own menu.
func (h *Host) openTrayMenu(t *trayIcon) {
	Send(t.item, "setMenu:", uintptr(t.menu))
	Send(Send(t.item, "button"), "performClick:", 0)
	if t.flags&trayClickable != 0 {
		Send(t.item, "setMenu:", 0)
	}
}

// dockMenu is the Dock menu's items (applicationDockMenu:), or none.
func (h *Host) dockMenu() ID {
	if len(trayState.dock) == 0 {
		return 0
	}
	return h.statusMenu(trayState.dock, Sel("dockMenuItem:"), 0)
}

// backgroundPolicy is the activation policy the app runs with: an
// accessory (no Dock icon, not in the app switcher) when it runs in the
// background (app.RunInBackground) or its bundle says so (LSUIElement,
// asuka.json's "background"), else a regular app.
func backgroundPolicy() uintptr {
	if inBackground() {
		return 1 // NSApplicationActivationPolicyAccessory
	}
	return 0
}

// inBackground reports whether the app runs in the background.
func inBackground() bool {
	if boot.InBackground() {
		return true
	}
	info := Send(Send(Class("NSBundle"), "mainBundle"), "infoDictionary")
	if info == 0 {
		return false
	}
	v := Send(info, "objectForKey:", uintptr(String("LSUIElement")))
	return v != 0 && SendBool(v, "boolValue")
}

// traySnapshots writes each tray icon's button as a PNG beside the
// window's snapshot (path.tray.png, path.tray2.png...), in the order the
// app made them, and returns what they show and their menus hold, and
// the Dock menu's (ASUKA_DUMP).
func (h *Host) traySnapshots(path string) string {
	var ids []uint32
	for id := range trayState.icons {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	base := strings.TrimSuffix(path, ".png")
	var b strings.Builder
	for i, id := range ids {
		t := trayState.icons[id]
		name := base + ".tray.png"
		if i > 0 {
			name = base + ".tray" + itoa(i+1) + ".png"
		}
		writeViewPNG(Send(t.item, "button"), name)
		fmt.Fprintf(&b, "tray %d title %q tooltip %q\n", id, t.title, t.tip)
		dumpMenuItems(&b, t.items, 1)
	}
	if len(trayState.dock) > 0 {
		b.WriteString("dock\n")
		dumpMenuItems(&b, trayState.dock, 1)
	}
	return b.String()
}

// dumpMenuItems writes a menu's items, a line each, its submenus'
// indented under them.
func dumpMenuItems(b *strings.Builder, items []ops.MenuItem, depth int) {
	for _, it := range items {
		b.WriteString(strings.Repeat("  ", depth))
		if it.Is(ops.MenuSeparator) {
			b.WriteString("---\n")
			continue
		}
		fmt.Fprintf(b, "%q", it.Title)
		if it.Role == ops.MenuRoleQuit {
			b.WriteString(" quit")
		}
		if it.Is(ops.MenuChecked) {
			b.WriteString(" checked")
		}
		if it.Is(ops.MenuDisabled) {
			b.WriteString(" disabled")
		}
		b.WriteString("\n")
		dumpMenuItems(b, it.Items, depth+1)
	}
}
