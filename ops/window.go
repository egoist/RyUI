package ops

import "math"

// WindowConfig is a window's options when it opens: the main window's,
// as the root's WindowOptions command carries them, and a Window's, as
// its Config prop does. Sizes are its content's (the area the core lays
// out), in points; positions its frame's top left on the screen, in
// points from the main display's top left.
type WindowConfig struct {
	Width, Height       float32 // 0: the host's (800 by 600)
	MinWidth, MinHeight float32 // 0: the host's least
	MaxWidth, MaxHeight float32 // 0: no most
	X, Y                float32 // with WindowPositioned
	Flags               WindowFlags
	TitleBar            TitleBar
	Background          Color  // 0: the platform's window background
	Parent              NodeID // a Window's: the Window node or root it belongs to (with WindowChild or WindowModal)
	// StateKey names where the host remembers the window's place (its
	// position, size and whether it's maximized) across launches; empty:
	// not remembered. The main window's is "main" when the app gives none.
	StateKey string
	Title    string // the main window's (a Window's is its Title prop)
}

// WindowFlags are a WindowConfig's switches.
type WindowFlags uint32

const (
	WindowFixedSize   WindowFlags = 1 << iota // the user can't resize it
	WindowNoMinimize                          // no minimize button
	WindowNoMaximize                          // no maximize (zoom) button
	WindowNoClose                             // no close button
	WindowAlwaysOnTop                         // above other apps' windows
	WindowHidden                              // opens hidden: a WindowAction Show shows it
	WindowSkipTaskbar                         // no taskbar button (Windows), not in the Window menu (macOS)
	WindowCentered                            // centered on its display
	WindowPositioned                          // at X, Y
	WindowTransparent                         // no background: what's behind shows where the content draws nothing
	WindowMaximized                           // opens maximized (zoomed)
	WindowFullscreen                          // opens in full screen
	WindowChild                               // above its Parent, and goes with it
	WindowModal                               // a child that blocks its Parent until it closes
	// WindowPanel is a launcher's or a palette's window: floating above
	// every app's, on every Space or virtual desktop and over apps in
	// full screen, out of the taskbar and the window cycle, taking the
	// keyboard without activating the app (macOS), closed as the user
	// closes it on an Escape its content leaves or as it loses the
	// keyboard.
	WindowPanel
	// WindowFlyout is a window hanging from an anchor (the Window's
	// Flyout prop): a tray icon, or a rectangle of its Parent; frameless,
	// out of the taskbar, closed as the user closes it as it loses the
	// keyboard or on an Escape its content leaves.
	WindowFlyout
	// WindowFitContent sizes the window to its content: the core lays
	// its content out at its own size, within the least and most size,
	// and sends WindowAction Fit as that changes.
	WindowFitContent
	// WindowClickThrough lets the mouse go through the window to what's
	// behind it (WindowAction ClickThrough and NotClickThrough switch it).
	WindowClickThrough
	// WindowMenuBar: it shows the app's menu bar (MenuBar) as the main
	// window does: Windows' menu bar, Linux's main menu (a Mac's menu bar
	// is the app's, whatever the window): a document's window.
	WindowMenuBar
)

// WindowStateFlags are a WindowState event's bits.
type WindowStateFlags uint8

const (
	WindowVisible WindowStateFlags = 1 << iota
	WindowFocused
	WindowMinimizedState
	WindowMaximizedState
	WindowFullscreenState
	WindowOnTop
	WindowClickThroughState
)

// WindowState is what a WindowState event reports.
type WindowState struct {
	X, Y, Width, Height float32
	Flags               WindowStateFlags
}

// AppendWindowConfig appends c as the WindowOptions command and the
// Window kind's Config prop carry it: f32 width, height, min width, min
// height, max width, max height, x, y; u32 flags; u8 TitleBar; u64
// background; u32 parent; u32-length state key; then the title, to the
// end.
func AppendWindowConfig(b []byte, c *WindowConfig) []byte {
	for _, v := range [...]float32{c.Width, c.Height, c.MinWidth, c.MinHeight, c.MaxWidth, c.MaxHeight, c.X, c.Y} {
		b = le.AppendUint32(b, math.Float32bits(v))
	}
	b = le.AppendUint32(b, uint32(c.Flags))
	b = append(b, byte(c.TitleBar))
	b = le.AppendUint64(b, uint64(c.Background))
	b = le.AppendUint32(b, uint32(c.Parent))
	b = appendStr(b, c.StateKey)
	return append(b, c.Title...)
}

// DecodeWindowConfig reads what AppendWindowConfig writes. Args cut short
// leave the rest zero; the first four floats alone are the older form
// (width, height, min width, min height, then the title).
func DecodeWindowConfig(b []byte) WindowConfig {
	var c WindowConfig
	if len(b) < 32 {
		r := &reader{b: b}
		for _, p := range []*float32{&c.Width, &c.Height, &c.MinWidth, &c.MinHeight} {
			*p = r.f32()
		}
		if r.err == nil {
			c.Title = string(b[16:])
		}
		return c
	}
	r := &reader{b: b}
	for _, p := range []*float32{&c.Width, &c.Height, &c.MinWidth, &c.MinHeight, &c.MaxWidth, &c.MaxHeight, &c.X, &c.Y} {
		*p = r.f32()
	}
	c.Flags = WindowFlags(r.u32())
	c.TitleBar = TitleBar(r.u8())
	c.Background = Color(r.u64())
	c.Parent = NodeID(r.u32())
	c.StateKey = string(r.bytes())
	if r.err == nil {
		c.Title = string(r.b)
	} else {
		c.StateKey = ""
	}
	return c
}

// AppendWindowState appends s as a WindowState event carries it.
func AppendWindowState(b []byte, s WindowState) []byte {
	for _, v := range [...]float32{s.X, s.Y, s.Width, s.Height} {
		b = le.AppendUint32(b, math.Float32bits(v))
	}
	return append(b, byte(s.Flags))
}

// DecodeWindowState reads what AppendWindowState writes.
func DecodeWindowState(b []byte) WindowState {
	r := &reader{b: b}
	s := WindowState{X: r.f32(), Y: r.f32(), Width: r.f32(), Height: r.f32()}
	s.Flags = WindowStateFlags(r.u8())
	if r.err != nil {
		return WindowState{}
	}
	return s
}

// windowActionArgs reports whether a carries two floats: a size or a
// place.
func windowActionArgs(a WindowAction) bool {
	return a == WindowActionSize || a == WindowActionPosition || a == WindowActionFit
}

// AppendWindowAction appends a WindowAction command's args: the action,
// then for Size, Fit and Position the two floats.
func AppendWindowAction(b []byte, a WindowAction, x, y float32) []byte {
	b = append(b, byte(a))
	if windowActionArgs(a) {
		b = le.AppendUint32(b, math.Float32bits(x))
		b = le.AppendUint32(b, math.Float32bits(y))
	}
	return b
}

// DecodeWindowAction reads a WindowAction command's args.
func DecodeWindowAction(b []byte) (a WindowAction, x, y float32, ok bool) {
	if len(b) == 0 {
		return 0, 0, 0, false
	}
	a = WindowAction(b[0])
	if windowActionArgs(a) {
		if len(b) < 9 {
			return 0, 0, 0, false
		}
		r := &reader{b: b[1:]}
		x, y = r.f32(), r.f32()
	}
	return a, x, y, true
}

// Flyout is what a flyout window (WindowFlyout) hangs from, as its
// Window's Flyout prop carries it: a tray icon, or a rectangle of its
// parent's content.
type Flyout struct {
	// Tray is the tray icon it hangs from (asuka/sdk/tray's id for it); 0:
	// none, the rectangle instead.
	Tray uint32
	// X, Y, Width and Height are the rectangle, in points from the top
	// left of its parent's content (the window it's declared in); a
	// rectangle of no size is a point.
	X, Y, Width, Height float32
	// Placement is the side of the anchor it goes to, and how it lines
	// up with the anchor along that side.
	Placement Placement
	// Gap is the room between the anchor and the flyout, in points.
	Gap float32
}

// AppendFlyout appends f as the Window's Flyout prop carries it: u32
// tray, f32 x, y, width, height, u8 placement, f32 gap.
func AppendFlyout(b []byte, f Flyout) []byte {
	b = le.AppendUint32(b, f.Tray)
	for _, v := range [...]float32{f.X, f.Y, f.Width, f.Height} {
		b = le.AppendUint32(b, math.Float32bits(v))
	}
	b = append(b, byte(f.Placement))
	return le.AppendUint32(b, math.Float32bits(f.Gap))
}

// DecodeFlyout reads what AppendFlyout writes; ok is false for args cut
// short.
func DecodeFlyout(b []byte) (f Flyout, ok bool) {
	r := &reader{b: b}
	f.Tray = r.u32()
	f.X, f.Y, f.Width, f.Height = r.f32(), r.f32(), r.f32(), r.f32()
	f.Placement = Placement(r.u8())
	f.Gap = r.f32()
	if r.err != nil {
		return Flyout{}, false
	}
	return f, true
}
