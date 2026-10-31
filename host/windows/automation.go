//go:build windows && amd64

package windows

import (
	"fmt"
	stdimage "image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

func snapshotting() bool { return os.Getenv("NUI_SNAPSHOT") != "" }

// automation starts the debug automation: NUI_SCRIPT's steps (tap a
// view by its text, back, wait, snap), then NUI_SNAPSHOT (write the
// window as a PNG once it has shown, NUI_DUMP the view tree beside it)
// and quit.
func (h *Host) automation() {
	script := os.Getenv("NUI_SCRIPT")
	path := os.Getenv("NUI_SNAPSHOT")
	if script == "" && path == "" {
		return
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // images and fonts load
		for _, step := range strings.Split(script, ";") {
			step = strings.TrimSpace(step)
			if step == "" {
				continue
			}
			verb, arg, _ := strings.Cut(step, ":")
			if verb == "wait" {
				d, _ := time.ParseDuration(arg)
				time.Sleep(d)
				continue
			}
			done := make(chan struct{})
			h.Post(func() {
				defer close(done)
				h.step(verb, arg)
			})
			<-done
			time.Sleep(400 * time.Millisecond)
		}
		if path == "" {
			return
		}
		h.Post(func() {
			h.snapshot(path)
			if os.Getenv("NUI_DUMP") != "" {
				os.WriteFile(path+".txt", []byte(h.dump()), 0o644)
			}
			if os.Getenv("NUI_SNAPSHOT_STAY") == "" {
				saveState()
				for _, n := range h.nodes {
					if n.web != nil {
						n.web.close() // WebView2's browser goes quietly
					}
				}
				os.Exit(0)
			}
		})
	}()
}

// step runs one automation step: tap:<text> clicks the view showing the
// text (a button's title, a text, a tab), type:<text> types into the
// focused field, back pops, key:<shortcut> presses keys (ctrl+shift+k,
// ctrl+enter, f5) in the window in use or the menu open, menu:<title>
// chooses the menu bar's item, open:<title> opens a menu bar's menu,
// escape closes a menu or a sheet, close closes the window in use (as its
// close button), dpi:<dpi> moves that window to a monitor at that DPI
// (WM_DPICHANGED), snap:<file> writes a snapshot.
func (h *Host) step(verb, arg string) {
	switch verb {
	case "tap":
		if !h.tap(arg) {
			fmt.Fprintf(os.Stderr, "nui: automation: nothing shows %q\n", arg)
		}
	case "type":
		if f := Focus(); f != 0 {
			for _, r := range arg {
				SendMessage(f, WM_CHAR, uintptr(r), 0)
			}
		}
	case "submit":
		if f := Focus(); f != 0 {
			SendMessage(f, WM_KEYDOWN, VK_RETURN, 0)
		}
	case "back":
		if len(h.sheets) > 0 {
			h.back(h.sheets[len(h.sheets)-1])
		} else {
			h.back(h.root)
		}
	case "key":
		h.pressKeys(arg)
	case "open":
		// A menu bar's menu, below its title, as clicking it shows it
		// (the window isn't active: Alt and its key wouldn't open it).
		bar := GetMenu(h.window)
		for i, m := range h.appMenus {
			if m.title != arg {
				continue
			}
			r := MenuItemRect(h.window, bar, i)
			h.afterCore(func() {
				h.menuLoop(true)
				got := TrackPopupMenu(SubMenu(bar, i), TPM_LEFTALIGN|TPM_TOPALIGN, r.Left, r.Bottom, h.window)
				h.menuLoop(false)
				if got > 0 {
					h.menuCommand(got)
				}
			})
			return
		}
		fmt.Fprintf(os.Stderr, "nui: automation: no menu %q\n", arg)
	case "menu":
		for _, it := range h.appItems {
			if it.title == arg {
				SendMessage(h.window, WM_COMMAND, uintptr(menuBase+it.index), 0)
				return
			}
		}
		fmt.Fprintf(os.Stderr, "nui: automation: no menu item %q\n", arg)
	case "close":
		w := h.windowInUse()
		if len(h.wins) > 0 && w == h.window {
			w = h.wins[len(h.wins)-1].nav.window
		}
		SendMessage(w, WM_CLOSE, 0, 0)
	case "dpi":
		// As if the window in use (or the last Window) went to a monitor
		// at this DPI: Windows' WM_DPICHANGED, with the size it suggests
		// (its client area's points kept). The frame, drawn by Windows,
		// keeps its DPI.
		w := h.windowInUse()
		if len(h.wins) > 0 && w == h.window {
			w = h.wins[len(h.wins)-1].nav.window
		}
		dpi, _ := strconv.Atoi(arg)
		if dpi < 48 {
			return
		}
		cr, wr := ClientRect(w), WindowRect(w)
		k := float32(DPI(w)) / 96
		if f := h.byHwnd[w]; f != nil && f.k > 0 {
			k = f.k
		} else if w == h.window {
			k = h.scale
		}
		nk := dpiScale(dpi)
		s := Rect{Left: wr.Left, Top: wr.Top}
		s.Right = s.Left + wr.W() - cr.W() + int32(float32(cr.W())/k*nk+0.5)
		s.Bottom = s.Top + wr.H() - cr.H() + int32(float32(cr.H())/k*nk+0.5)
		SendMessage(w, WM_DPICHANGED, MakeLong(uint16(dpi), uint16(dpi)), Ptr(&s))
	case "resize":
		// The window's client area to w,h points.
		ws, hs, _ := strings.Cut(arg, ",")
		w, _ := strconv.Atoi(ws)
		ht, _ := strconv.Atoi(hs)
		r := Rect{Right: h.px(nil, float32(w)), Bottom: h.px(nil, float32(ht))}
		AdjustWindowRect(&r, uint32(WindowLong(h.window, GWL_STYLE)), 0, h.dpi)
		SetWindowPos(h.window, 0, 0, 0, r.W(), r.H(), SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE)
	case "scroll":
		// The innermost scroll view showing, to a y.
		y, _ := strconv.ParseFloat(arg, 32)
		var s *node
		for _, n := range h.nodes {
			if (n.kind == ops.KindScroll || n.kind == ops.KindList) && h.onScreen(n) && (s == nil || n.id > s.id) {
				s = n
			}
		}
		if s != nil {
			h.scrollToOffset(s, 0, float32(y))
		}
	case "escape":
		if OpenMenuWindow() != 0 {
			EndMenu()
			return
		}
		if len(h.sheets) > 0 {
			h.cancelSheet(h.byHwnd[h.sheets[len(h.sheets)-1].nav.window])
		}
	case "snap":
		h.snapshot(arg)
		if os.Getenv("NUI_DUMP") != "" {
			os.WriteFile(arg+".txt", []byte(h.dump()), 0o644)
		}
	}
}

// tap clicks what shows text: the mouse's messages, at its center, to
// the window under it that takes them.
func (h *Host) tap(text string) bool {
	var found *node
	var tab, tabIndex = (*node)(nil), -1
	for _, n := range h.nodes {
		if !h.onScreen(n) {
			continue
		}
		if n.text == text && n.kind != ops.KindTextField || n.title == text && n.kind == ops.KindButton || n.kind == ops.KindTextField && n.placeholder == text {
			if found == nil || n.id > found.id {
				found = n
			}
		}
		if n.kind == ops.KindTabs {
			for i, s := range n.children {
				if s.title == text {
					tab, tabIndex = n, i
				}
			}
		}
		if n.kind == kindHeader && n.nav.screen != nil {
			for i, a := range n.nav.screen.actions {
				if a.title == text && i < len(n.nav.buttons) {
					SendMessage(n.nav.buttons[i], BM_CLICK, 0, 0)
					return true
				}
			}
		}
	}
	if found == nil && tab != nil {
		h.selectTab(tab, tabIndex)
		return true
	}
	if found == nil {
		return false
	}
	if found.kind == ops.KindTextField {
		SetFocus(found.ctl)
		return true
	}
	if found.kind == ops.KindButton || found.kind == ops.KindToggle {
		SendMessage(found.ctl, BM_CLICK, 0, 0)
		return true
	}
	// The nearest window that takes the mouse.
	t := found
	for t != nil && !t.interactive() {
		t = t.parent
	}
	if t == nil || t.hwnd == 0 {
		return false
	}
	c := ClientRect(found.hwnd)
	p := MapPoint(found.hwnd, t.hwnd, Point{X: c.W() / 2, Y: c.H() / 2})
	lp := MakeLong(uint16(p.X), uint16(p.Y))
	SendMessage(t.hwnd, WM_LBUTTONDOWN, MK_LBUTTON, lp)
	SendMessage(t.hwnd, WM_LBUTTONUP, 0, lp)
	return true
}

// onScreen reports whether n's window shows: it and its ancestors are
// visible, in a window of the app.
func (h *Host) onScreen(n *node) bool {
	if n.hwnd == 0 || !IsWindowVisible(n.hwnd) {
		return false
	}
	w := RootWindow(n.hwnd)
	if w != h.window && h.byHwnd[w] == nil {
		return false
	}
	// Its center is in its parents' (not scrolled out of a list's).
	r := WindowRect(n.hwnd)
	cx, cy := (r.Left+r.Right)/2, (r.Top+r.Bottom)/2
	for p := Parent(n.hwnd); p != 0; p = Parent(p) {
		pr := WindowRect(p)
		if cx < pr.Left || cx >= pr.Right || cy < pr.Top || cy >= pr.Bottom {
			return false
		}
		if p == w {
			break
		}
	}
	return true
}

// snapshot writes the window, its title bar too, as a PNG (PrintWindow
// draws it without bringing it to the front). With sheets open, the top
// sheet's window is written beside it (file.sheet.png), and each Window
// open (file.window.png, file.window2.png…).
func (h *Host) snapshot(path string) {
	writeWindowPNG(h.window, path)
	base := strings.TrimSuffix(path, ".png")
	if len(h.sheets) > 0 {
		s := h.sheets[len(h.sheets)-1]
		writeWindowPNG(s.nav.window, base+".sheet.png")
	}
	if w := OpenMenuWindow(); w != 0 {
		// A menu: as it shows on the screen (DWM rounds and borders it).
		writeScreenPNG(WindowRect(w), base+".menu.png")
	}
	for i, w := range h.wins {
		name := base + ".window.png"
		if i > 0 {
			name = fmt.Sprintf("%s.window%d.png", base, i+1)
		}
		writeWindowPNG(w.nav.window, name)
	}
}

// pressKeys presses a shortcut (ctrl+shift+k) in the window in use, as
// the keyboard would: the modifiers held (the thread's key state) while
// the key's down message goes through the message loop's handling.
func (h *Host) pressKeys(spec string) {
	parts := strings.Split(strings.ToLower(spec), "+")
	var mods byte
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl", "control":
			mods |= modControl
		case "shift":
			mods |= modShift
		case "alt":
			mods |= modAlt
		}
	}
	a, ok := accelerator(parts[len(parts)-1], mods)
	if !ok {
		fmt.Fprintf(os.Stderr, "nui: automation: no key %q\n", spec)
		return
	}
	if w := OpenMenuWindow(); w != 0 {
		// A menu showing takes the key (arrows, Enter, an access key).
		if r := []rune(parts[len(parts)-1]); len(r) == 1 && mods == 0 {
			SendMessage(w, WM_CHAR, uintptr(r[0]), 0)
		} else {
			SendMessage(w, WM_KEYDOWN, uintptr(a.Key), 0)
		}
		return
	}
	saved := KeyboardState()
	st := saved
	for _, k := range []struct {
		flag byte
		vk   int
	}{{FCONTROL, VK_CONTROL}, {FSHIFT, VK_SHIFT}, {FALT, VK_MENU}} {
		if a.Virt&k.flag != 0 {
			st[k.vk] = 0x80
		}
	}
	SetKeyboardState(st)
	target := Focus()
	if target == 0 || h.keyWindow(target) == 0 {
		target = h.windowInUse()
	}
	msg := uint32(WM_KEYDOWN)
	if a.Virt&FALT != 0 {
		msg = WM_SYSKEYDOWN
	}
	m := Msg{Hwnd: target, Message: msg, WParam: uintptr(a.Key)}
	if !h.preTranslate(&m) {
		TranslateMessage(&m)
		DispatchMessage(&m)
	}
	SetKeyboardState(saved)
}

func writeWindowPNG(hwnd uintptr, path string) {
	r := WindowRect(hwnd)
	vis := FrameBounds(hwnd)
	w, ht := r.W(), r.H()
	if w <= 0 || ht <= 0 {
		return
	}
	screen := GetDC(0)
	dc := CreateCompatibleDC(screen)
	ReleaseDC(0, screen)
	bmp, pixels := DIB(w, ht)
	old := SelectObject(dc, bmp)
	PrintWindow(hwnd, dc)
	// Only what shows: not the invisible resize borders.
	x0, y0 := int(vis.Left-r.Left), int(vis.Top-r.Top)
	vw, vh := int(vis.W()), int(vis.H())
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, vw, vh))
	for y := range vh {
		for x := range vw {
			i, o := ((y+y0)*int(w)+x+x0)*4, (y*vw+x)*4
			if i+3 < len(pixels) {
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = pixels[i+2], pixels[i+1], pixels[i], 255
			}
		}
	}
	SelectObject(dc, old)
	DeleteObject(bmp)
	DeleteDC(dc)
	f, err := os.Create(path)
	if err != nil {
		return
	}
	png.Encode(f, img)
	f.Close()
}

// dump describes the native view tree: each node's kind, frame (points)
// and what it shows.
func (h *Host) dump() string {
	var b strings.Builder
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		b.WriteString(strings.Repeat("  ", depth))
		fmt.Fprintf(&b, "%d %s", n.id&^ops.HostIDBit, kindName(n.kind))
		if n.hwnd != 0 {
			p := Parent(n.hwnd)
			r := WindowRectIn(n.hwnd, p)
			fmt.Fprintf(&b, " [%.0f,%.0f %.0fx%.0f]", h.pt(n, r.Left), h.pt(n, r.Top), h.pt(n, r.W()), h.pt(n, r.H()))
			if !IsWindowVisible(n.hwnd) {
				b.WriteString(" hidden")
			}
			if p == h.park {
				b.WriteString(" parked")
			}
		}
		if n.text != "" {
			fmt.Fprintf(&b, " %q", n.text)
		}
		if n.title != "" {
			fmt.Fprintf(&b, " title=%q", n.title)
		}
		b.WriteByte('\n')
		for _, c := range n.children {
			walk(c, depth+1)
		}
		if n.list != nil {
			for r, rv := range n.list.shown {
				for _, c := range rv.cells {
					b.WriteString(strings.Repeat("  ", depth+1))
					fmt.Fprintf(&b, "row %d:\n", r)
					for _, k := range c.n.children {
						walk(k, depth+2)
					}
				}
			}
		}
	}
	if h.root != nil {
		walk(h.root, 0)
	}
	for _, s := range h.sheets {
		b.WriteString("sheet:\n")
		walk(s, 1)
	}
	for _, w := range h.wins {
		fmt.Fprintf(&b, "window %q:\n", WindowText(w.nav.window))
		walk(w, 1)
	}
	if bar := GetMenu(h.window); bar != 0 {
		b.WriteString("menu bar:\n")
		for i := range MenuItemCount(bar) {
			fmt.Fprintf(&b, "  %s\n", MenuString(bar, i))
			sub := SubMenu(bar, i)
			for j := range MenuItemCount(sub) {
				s := MenuString(sub, j)
				if MenuState(sub, j)&MF_SEPARATOR != 0 {
					s = "---"
				} else if MenuState(sub, j)&MF_GRAYED != 0 {
					s += " (off)"
				}
				fmt.Fprintf(&b, "    %q\n", s)
			}
		}
	}
	return b.String()
}

func kindName(k ops.Kind) string {
	switch k {
	case kindRow:
		return "Row"
	case kindCell:
		return "Cell"
	case kindHeader:
		return "Header"
	case kindTabBar:
		return "TabBar"
	case kindWindowFrame:
		return "WindowFrame"
	}

	return k.String()
}

// writeScreenPNG writes what the screen shows at r.
func writeScreenPNG(r Rect, path string) {
	if r.W() <= 0 || r.H() <= 0 {
		return
	}
	screen := GetDC(0)
	dc := CreateCompatibleDC(screen)
	bmp, pixels := DIB(r.W(), r.H())
	old := SelectObject(dc, bmp)
	BitBlt(dc, 0, 0, r.W(), r.H(), screen, r.Left, r.Top)
	ReleaseDC(0, screen)
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, int(r.W()), int(r.H())))
	for i := 0; i < int(r.W()*r.H()); i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = pixels[i*4+2], pixels[i*4+1], pixels[i*4], 255
	}
	SelectObject(dc, old)
	DeleteObject(bmp)
	DeleteDC(dc)
	if f, err := os.Create(path); err == nil {
		png.Encode(f, img)
		f.Close()
	}
}
