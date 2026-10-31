//go:build windows && amd64

package windows

import (
	"fmt"
	stdimage "image"
	"image/png"
	"os"
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
				os.Exit(0)
			}
		})
	}()
}

// step runs one automation step: tap:<text> clicks the view showing the
// text (a button's title, a text, a tab), type:<text> types into the
// focused field, back pops, snap:<file> writes a snapshot.
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
		h.back()
	case "escape":
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
// sheet's window is written beside it (file.sheet.png).
func (h *Host) snapshot(path string) {
	writeWindowPNG(h.window, path)
	if len(h.sheets) > 0 {
		s := h.sheets[len(h.sheets)-1]
		writeWindowPNG(s.nav.window, strings.TrimSuffix(path, ".png")+".sheet.png")
	}
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
			fmt.Fprintf(&b, " [%.0f,%.0f %.0fx%.0f]", h.pt(r.Left), h.pt(r.Top), h.pt(r.W()), h.pt(r.H()))
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
	}
	return k.String()
}
