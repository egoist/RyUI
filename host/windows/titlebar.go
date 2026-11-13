//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A window with a hidden title bar (ui.TitleBarHidden) gives its client
// area, and so its content, the whole window: WM_NCCALCSIZE keeps the
// left, right and bottom borders, which Windows 10 and later draw
// invisible outside the window and which still resize it, and gives the
// top to the client area (Windows 11 keeps a pixel of it, which the
// window's border covers: without any non-client area at the top it
// opens no snap layouts over the maximize button). A maximized one fits
// its monitor's work area. As MyGo's windows do, two child windows of the
// host's sit above the content:
//
//   - The caption buttons, minimize, maximize or restore, and close, drawn
//     as Windows 11 draws its own (Segoe Fluent Icons' glyphs, Segoe MDL2
//     Assets' on Windows 10) in a layered window whose pixels carry their
//     alpha, so the content shows around them, at the top right (top
//     left, mirrored, right to left). It answers WM_NCHITTEST with the
//     buttons' codes, which brings Windows 11's snap layouts over the
//     maximize button, and runs the buttons from the non-client mouse
//     messages that follow, as system commands.
//   - The top edge: an invisible window along the rest of the top, as tall
//     as the sizing border, answering HTTOP (HTTOPLEFT, HTTOPRIGHT near
//     the corners), and handing presses to the window, which then resizes
//     from its top edge as one with a caption does.
//
// Below them, the strip as tall as a title bar (32 points) moves the
// window where the content takes no clicks: views that don't let the
// mouse through to their window (HTTRANSPARENT) and the window answers
// HTCAPTION there, so a drag moves it, a double click maximizes it and a
// right click opens its system menu. The window's Environment reports the
// strip's height as SafeTop, so SafeArea keeps clear of the buttons. The
// main window has no menu bar then (the menus' shortcuts still work):
// Windows draws a menu bar under the caption, which isn't there.

const (
	// Windows 11's caption buttons are 46 points wide, as tall as the
	// title bar, with glyphs of 10.
	captionHeight      = 32
	captionButtonWidth = 46
	captionGlyphSize   = 10
	// resizeCornerSize is how far from a corner the top edge resizes the
	// corner, as Chromium's frames do.
	resizeCornerSize = 16
)

// hiddenTop is the non-client pixels a hidden title bar keeps at the top.
func hiddenTop() int32 {
	if Windows11() {
		return 1
	}
	return 0
}

type captionButton int

const (
	buttonMinimize captionButton = iota
	buttonMaximize
	buttonClose
)

// glyph is the button's glyph in Segoe Fluent Icons and Segoe MDL2
// Assets, which have them at the same code points.
func (b captionButton) glyph(maximized bool) uint16 {
	switch b {
	case buttonMinimize:
		return 0xE921 // ChromeMinimize
	case buttonMaximize:
		if maximized {
			return 0xE923 // ChromeRestore
		}
		return 0xE922 // ChromeMaximize
	}
	return 0xE8BB // ChromeClose
}

func (b captionButton) hitTest() uintptr {
	switch b {
	case buttonMinimize:
		return HTMINBUTTON
	case buttonMaximize:
		return HTMAXBUTTON
	}
	return HTCLOSE
}

// captionBar is a hidden title bar's buttons' window.
type captionBar struct {
	hwnd  uintptr
	shown []captionButton
	// hot is the index of the button under the pointer, pressed that of
	// the button pressed; -1: none.
	hot, pressed int
	tracking     bool
	active       bool
}

// hideTitleBar gives a window its hidden title bar's buttons and top edge.
func (h *Host) hideTitleBar(c *wctl) {
	c.hidden = true
	if Windows11() {
		// No accent on what's left of the caption ("Show accent color on
		// title bars" on).
		SetCaptionColor(c.hwnd, 0xFFFFFFFE) // DWMWA_COLOR_NONE
	}
	if b := CreateWindow(WS_EX_LAYERED, classView, "", WS_CHILD|WS_CLIPSIBLINGS, 0, 0, 0, 0, c.hwnd, 0); b != 0 {
		c.caption = &captionBar{hwnd: b, hot: -1, pressed: -1, active: true}
		h.chrome[b] = c
	}
	if e := CreateWindow(WS_EX_LAYERED|WS_EX_NOREDIRECTIONBITMAP, classView, "", WS_CHILD|WS_CLIPSIBLINGS, 0, 0, 0, 0, c.hwnd, 0); e != 0 {
		// A layered window shows, and takes the mouse, once it has
		// attributes; this one has no pixels to show.
		SetLayeredOpaque(e)
		c.topEdge = e
		h.chrome[e] = c
	}
	if c.main && GetMenu(h.window) != 0 {
		h.buildMenuBar() // without its bar now
	}
}

// forgetChrome forgets a window's hidden title bar's windows, which
// Windows destroys with it.
func (h *Host) forgetChrome(c *wctl) {
	if c.caption != nil {
		delete(h.chrome, c.caption.hwnd)
	}
	if c.topEdge != 0 {
		delete(h.chrome, c.topEdge)
	}
}

// buttons are the caption buttons a window shows, left to right: close
// alone when it can be neither minimized nor maximized, as Windows does.
func (c *wctl) buttons() []captionButton {
	style := WindowLong(c.hwnd, GWL_STYLE)
	if style&(WS_MINIMIZEBOX|WS_MAXIMIZEBOX) == 0 {
		return []captionButton{buttonClose}
	}
	return []captionButton{buttonMinimize, buttonMaximize, buttonClose}
}

func (c *wctl) buttonEnabled(b captionButton) bool {
	style := WindowLong(c.hwnd, GWL_STYLE)
	switch b {
	case buttonMinimize:
		return style&WS_MINIMIZEBOX != 0
	case buttonMaximize:
		return style&WS_MAXIMIZEBOX != 0
	}
	return !c.has(ops.WindowNoClose)
}

// mirrored reports a window laid out right to left (WS_EX_LAYOUTRTL): its
// buttons are at its top left, close first.
func (c *wctl) mirrored() bool { return WindowLong(c.hwnd, GWL_EXSTYLE)&WS_EX_LAYOUTRTL != 0 }

func round(v float32) int32 { return int32(math.Round(float64(v))) }

// layoutChrome puts a hidden title bar's buttons at the top right, above
// the content, and the top edge along the rest of the top, and paints the
// buttons; or hides them where they have no place (full screen, the top
// edge of a maximized window or one the user can't resize).
func (h *Host) layoutChrome(c *wctl) {
	if !c.hidden || IsIconic(c.hwnd) {
		return
	}
	k := c.k(h)
	r := ClientRect(c.hwnd)
	width := int32(0)
	if b := c.caption; b != nil {
		b.shown = c.buttons()
		if b.hot >= len(b.shown) || b.pressed >= len(b.shown) {
			b.hot, b.pressed = -1, -1
		}
		width = round(captionButtonWidth*k) * int32(len(b.shown))
		show := uint32(SWP_SHOWWINDOW)
		if c.full != nil {
			show = SWP_HIDEWINDOW
		}
		// HWND_TOP: above the content's window.
		SetWindowPos(b.hwnd, HWND_TOP, r.Right-width, 0, width, round(captionHeight*k), SWP_NOACTIVATE|show)
		b.paint(h, c)
	}
	if c.topEdge != 0 {
		show := uint32(SWP_SHOWWINDOW)
		if c.full != nil || IsZoomed(c.hwnd) || WindowLong(c.hwnd, GWL_STYLE)&WS_THICKFRAME == 0 {
			show = SWP_HIDEWINDOW
		}
		SetWindowPos(c.topEdge, HWND_TOP, 0, 0, max(r.W()-width, 0), resizeBorder(DPI(c.hwnd)), SWP_NOACTIVATE|show)
	}
}

// resizeBorder is the height of the band along the top edge that resizes
// a window: the thickness of its sizing frame, as above a caption.
func resizeBorder(dpi int) int32 {
	if b := SystemMetric(SM_CYSIZEFRAME, dpi) + SystemMetric(SM_CXPADDEDBORDER, dpi); b > 0 {
		return int32(b)
	}
	return int32(8 * dpi / 96)
}

// titleBarMessage handles a hidden title bar's top-level messages: the
// client area the whole window, the strip at the top a caption.
func (h *Host) titleBarMessage(c *wctl, m uint32, wp, lp uintptr) (uintptr, bool) {
	if !c.hidden {
		return 0, false
	}
	switch m {
	case WM_NCCALCSIZE:
		if wp == 0 || c.full != nil {
			return 0, false
		}
		p := Native[NCCalcSizeParams](lp)
		if IsZoomed(c.hwnd) {
			// A maximized window would overflow its monitor by its
			// borders: its work area, the proposed rectangle's monitor's
			// (a window restored from minimized is still parked off the
			// screen as it's asked).
			p.Rects[0] = MonitorWorkArea(p.Rects[0])
			return 0, true
		}
		top := p.Rects[0].Top
		DefWindowProc(c.hwnd, m, wp, lp)
		p.Rects[0].Top = top + hiddenTop()
		return 0, true
	case WM_NCHITTEST:
		r := DefWindowProc(c.hwnd, m, wp, lp)
		if r == HTCLIENT && c.full == nil {
			pt := ScreenToClient(c.hwnd, Point{X: XParam(lp), Y: YParam(lp)})
			if pt.Y < round(captionHeight*c.k(h)) {
				return HTCAPTION, true
			}
		}
		return r, true
	}
	return 0, false
}

// chromeMessage handles the messages of a hidden title bar's buttons and
// top edge; ok false lets DefWindowProc run.
func (h *Host) chromeMessage(c *wctl, hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	if hwnd == c.topEdge {
		switch m {
		case WM_NCHITTEST:
			pt := ScreenToClient(c.hwnd, Point{X: XParam(lp), Y: YParam(lp)})
			edge, client := ClientRect(c.topEdge), ClientRect(c.hwnd)
			return topEdgeHit(pt.X, edge.Right, client.Right, round(resizeCornerSize*c.k(h))), true
		case WM_NCLBUTTONDOWN, WM_NCLBUTTONDBLCLK:
			// The window resizes from its top edge, or a double click
			// stretches it to the screen's height, as from a caption.
			return SendMessage(c.hwnd, m, wp, lp), true
		}
		return 0, false
	}
	b := c.caption
	if b == nil || hwnd != b.hwnd {
		return 0, false
	}
	switch m {
	case WM_NCHITTEST:
		if i := b.indexAt(h, c, lp); i >= 0 && c.buttonEnabled(b.shown[i]) {
			return b.shown[i].hitTest(), true
		}
		return HTCLIENT, true
	case WM_NCMOUSEMOVE:
		b.setHot(h, c, b.indexAt(h, c, lp))
		if !b.tracking {
			TrackNonClientLeave(hwnd, true)
			b.tracking = true
		}
		// DefWindowProc brings the snap layouts over HTMAXBUTTON.
		return 0, false
	case WM_NCMOUSELEAVE, WM_MOUSELEAVE:
		b.tracking = false
		b.hot, b.pressed = -1, -1
		b.paint(h, c)
		return 0, true
	case WM_NCLBUTTONDOWN, WM_NCLBUTTONDBLCLK:
		// Not DefWindowProc: it would run the system's buttons on this
		// child window.
		if i := b.indexAt(h, c, lp); i >= 0 && c.buttonEnabled(b.shown[i]) {
			b.pressed = i
			b.paint(h, c)
		}
		return 0, true
	case WM_NCLBUTTONUP:
		i, pressed := b.indexAt(h, c, lp), b.pressed
		b.pressed = -1
		b.paint(h, c)
		if i >= 0 && i == pressed {
			b.click(c, b.shown[i])
		}
		return 0, true
	case WM_NCRBUTTONDOWN, WM_NCRBUTTONUP, WM_NCRBUTTONDBLCLK:
		return 0, true
	case WM_ERASEBKGND:
		return 1, true
	}
	return 0, false
}

// topEdgeHit is the hit-test code at x, in pixels from the left of the
// window's client area, on a top edge edgeWidth pixels wide in a client
// area clientWidth wide: the corner within corner pixels of the left, or
// of the right where the edge reaches it, and the top elsewhere.
func topEdgeHit(x, edgeWidth, clientWidth, corner int32) uintptr {
	switch {
	case x < corner:
		return HTTOPLEFT
	case edgeWidth >= clientWidth && x >= clientWidth-corner:
		return HTTOPRIGHT
	}
	return HTTOP
}

// indexAt is the index of the button at a non-client mouse message's
// screen position, or -1.
func (b *captionBar) indexAt(h *Host, c *wctl, lp uintptr) int {
	pt := ScreenToClient(b.hwnd, Point{X: XParam(lp), Y: YParam(lp)})
	k := c.k(h)
	bw := round(captionButtonWidth * k)
	if pt.X < 0 || pt.Y < 0 || pt.Y >= round(captionHeight*k) || bw <= 0 {
		return -1
	}
	i := int(pt.X / bw)
	if i >= len(b.shown) {
		return -1
	}
	if c.mirrored() {
		i = len(b.shown) - 1 - i
	}
	return i
}

func (b *captionBar) setHot(h *Host, c *wctl, i int) {
	if i >= 0 && !c.buttonEnabled(b.shown[i]) {
		i = -1
	}
	if i != b.hot {
		b.hot = i
		b.paint(h, c)
	}
}

func (b *captionBar) activate(h *Host, active bool) {
	if b.active != active {
		b.active = active
		if c := h.chrome[b.hwnd]; c != nil {
			b.paint(h, c)
		}
	}
}

// click runs a button as the system's do: a system command, sent.
func (b *captionBar) click(c *wctl, btn captionButton) {
	if !c.buttonEnabled(btn) {
		return
	}
	cmd := uintptr(SC_CLOSE)
	switch btn {
	case buttonMinimize:
		cmd = SC_MINIMIZE
	case buttonMaximize:
		cmd = SC_MAXIMIZE
		if IsZoomed(c.hwnd) {
			cmd = SC_RESTORE
		}
	}
	SendMessage(c.hwnd, WM_SYSCOMMAND, cmd, 0)
}

// Windows 11's close button is red under the pointer, Windows 10's a
// brighter red.
var (
	closeRed11 = rgba{0xC4, 0x2B, 0x1C, 0xFF}
	closeRed10 = rgba{0xE8, 0x11, 0x23, 0xFF}
)

// buttonsDark reports whether a window's caption buttons draw for a dark
// background: its own background's, else the app's appearance.
func (h *Host) buttonsDark(c *wctl) bool {
	if c.cfg.Background != 0 {
		bg := h.bgOf(c.hwnd)
		return 0.299*float64(bg.r)+0.587*float64(bg.g)+0.114*float64(bg.b) < 128
	}
	return h.dark
}

// colors are the i-th button's backplate and glyph colors, after Windows'
// own: the glyph in the text color, fainter while the window is inactive
// or the button disabled; under the pointer and pressed, a backplate of
// the glyph's color at 10% and 20%, or red behind a white glyph for
// close. At rest the backplate is all but transparent, which keeps the
// buttons' window taking the mouse there.
func (b *captionBar) colors(c *wctl, i int, btn captionButton, dark bool) (back, fore rgba) {
	fore = rgba{0, 0, 0, 0xE4}
	if dark {
		fore = rgba{0xFF, 0xFF, 0xFF, 0xFF}
	}
	back = rgba{0, 0, 0, 1}
	red := closeRed10
	if Windows11() {
		red = closeRed11
	}
	white := rgba{0xFF, 0xFF, 0xFF, 0xFF}
	hot := i == b.hot && (b.pressed < 0 || b.pressed == i)
	switch {
	case !c.buttonEnabled(btn):
		fore.a = 0x5C
	case hot && b.pressed == i && btn == buttonClose:
		back, fore = rgba{red.r, red.g, red.b, 0xCC}, white
	case hot && btn == buttonClose:
		back, fore = red, white
	case hot && b.pressed == i:
		back = rgba{fore.r, fore.g, fore.b, 0x33}
	case hot:
		back = rgba{fore.r, fore.g, fore.b, 0x1A}
	case !b.active:
		fore.a = 0x72
	}
	return back, fore
}

// blend lays a glyph color of coverage cov (0 to 255) over a backplate,
// into a premultiplied BGRA pixel, as UpdateLayeredWindow takes it.
func blend(px []byte, fore rgba, cov uint32, back rgba) {
	fa := uint32(fore.a) * cov / 255
	ba := uint32(back.a) * (255 - fa) / 255
	ch := func(f, b uint8) byte { return byte((uint32(f)*fa + uint32(b)*ba) / 255) }
	px[0], px[1], px[2], px[3] = ch(fore.b, back.b), ch(fore.g, back.g), ch(fore.r, back.r), byte(fa+ba)
}

// paint draws the buttons for the window's state and appearance into
// their layered window: the glyphs drawn white on black give their
// coverage, laid in their colors over their backplates.
func (b *captionBar) paint(h *Host, c *wctl) {
	if len(b.shown) == 0 {
		return
	}
	k := c.k(h)
	bw, height := round(captionButtonWidth*k), round(captionHeight*k)
	width := bw * int32(len(b.shown))
	if width <= 0 || height <= 0 {
		return
	}
	img, px := DIB(width, height)
	mask, cov := DIB(width, height)
	if img == 0 || mask == 0 {
		DeleteObject(img)
		DeleteObject(mask)
		return
	}
	defer DeleteObject(img)
	defer DeleteObject(mask)
	mdc := CreateCompatibleDC(0)
	defer DeleteDC(mdc)
	oldMask := SelectObject(mdc, mask)
	oldFont := SelectObject(mdc, h.font(fontSpec{size: captionGlyphSize, weight: 400, family: h.iconFace(), k: k, gray: true}))
	SetBkMode(mdc, TRANSPARENT)
	SetTextColor(mdc, 0xFFFFFF)
	maximized := IsZoomed(c.hwnd)
	mirrored := c.mirrored()
	slot := func(i int) int32 {
		if mirrored {
			return int32(len(b.shown) - 1 - i)
		}
		return int32(i)
	}
	for i, btn := range b.shown {
		g := []uint16{btn.glyph(maximized)}
		x := slot(i) * bw
		r := Rect{Left: x, Top: 0, Right: x + bw, Bottom: height}
		DrawText(mdc, g, &r, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	}
	SelectObject(mdc, oldFont)
	SelectObject(mdc, oldMask)
	GdiFlush()
	dark := h.buttonsDark(c)
	for i, btn := range b.shown {
		back, fore := b.colors(c, i, btn, dark)
		x0 := slot(i) * bw
		for y := int32(0); y < height; y++ {
			row := y * width
			for x := x0; x < x0+bw; x++ {
				o := (row + x) * 4
				blend(px[o:o+4], fore, uint32(cov[o+1]), back) // the green channel: the glyph's coverage
			}
		}
	}
	idc := CreateCompatibleDC(0)
	old := SelectObject(idc, img)
	UpdateLayered(b.hwnd, idc, width, height)
	SelectObject(idc, old)
	DeleteDC(idc)
}
