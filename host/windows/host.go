//go:build windows && amd64

// Package windows is RyUI's Windows host: Win32 windows and the common
// controls (version 6), made and driven from Go through syscall (no cgo),
// in the app's process. It reads the core's batches as Go structs,
// without encoding them.
//
// Run starts the app: it locks the main goroutine to the main thread
// (this package's init does), makes a window whose client area is the
// root, and runs the message loop.
package windows

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/egoist/ryui/host"
	"github.com/egoist/ryui/internal/boot"
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/ui"
)

// Win32 windows belong to the thread that made them: the main thread,
// where main starts.
func init() { runtime.LockOSThread() }

// node is a view the core made, or one the host made itself (a list's
// rows and cells, a stack's header, a tab bar).
type node struct {
	id       ops.NodeID
	kind     ops.Kind
	hwnd     uintptr // its window
	ctl      uintptr // a control inside it (a TextField's EDIT)
	ctl2     uintptr // a second one (a Stepper's plus)
	children []*node
	parent   *node
	listens  [2]uint64  // bits by EventKind
	frame    [4]float32 // x, y, width, height in points, in its native parent
	k        float32    // pixels per point of the top-level window it's in; 0: the main window's

	// Text props.
	textStyle, fontWeight, textAlign int32
	fontSize                         float32
	fontFamily                       string
	color                            ops.Color
	lineLimit                        int

	// Visual props.
	background, borderColor ops.Color
	cornerRadius            float32
	borderWidth             float32
	gradient                []byte
	shadow                  []byte
	opacity                 float32
	hidden                  bool
	disabled                bool

	menu        []action
	menuTrigger int32

	// State of the mouse on a view of the host's own drawing.
	hot, pressed bool
	pressAt      Point
	longFired    bool
	panning      bool
	panLast      Point
	panTime      int64
	focused      bool

	// Kind-specific.
	title, message string // Button, Screen, Alert
	variant        int32
	icon           string
	actions        []action // a Screen's bar buttons, an Alert's buttons
	alertStyle     int32
	text           string // Text, TextField's last reported value
	utext          []uint16
	placeholder    string
	multiline      bool
	settingText    bool // the field's text is being set from the core
	secure         bool
	keyboard       int32
	items          []string // Segmented, Wheel
	selected       int
	value          float64 // Slider, Stepper, Progress, DatePicker
	min, max, step float64
	on             bool    // Toggle
	indeterminate  bool    // Progress
	animating      bool    // Activity
	phase          float64 // Activity, indeterminate Progress
	dateMode       int32
	horizontal     bool // Scroll
	scrollX        float32
	scrollY        float32
	contentW       float32
	contentH       float32
	wheelAcc       int32
	image          *image
	symbol         string
	list           *list
	bar            *scrollBar // a Scroll's or List's thumb
	sbar           *scrollBar // the thumb a kindScrollBar view is
	row            *rowView
	cell           *cell
	nav            *nav
	rich           *rich
	shape          *shape
	web            *web
	presented      bool
	detents        int64
	dying          bool
	a11yLabel      string
	colorValue     ops.Color
	host           func(h *Host, n *node, m uint32, w, l uintptr) (uintptr, bool) // a host-made view's messages
}

// Kinds of the views the host makes itself, past the schema's.
const (
	kindRow ops.Kind = 0xF000 + iota
	kindCell
	kindHeader
	kindTabBar
	kindSheetWindow
	kindScrollBar
	kindWindowFrame // a Window's top-level window
)

func (n *node) listening(e ops.EventKind) bool {
	return e < 128 && n.listens[e/64]&(1<<(e%64)) != 0
}

type action struct {
	role  ops.AlertButtonRole
	title string
	icon  string
}

// Host is the Windows host.
type Host struct {
	core   host.Core
	nodes  map[ops.NodeID]*node
	byHwnd map[uintptr]*node
	root   *node
	window uintptr // the app's window
	park   uintptr // a hidden window views wait in until they're inserted
	msgWin uintptr // a message-only window: Post's wake-ups
	env    host.Environment
	dpi    int
	scale  float32 // pixels per point: dpi / 96
	dark   bool
	theme  palette

	depth   int // calls into the core in progress
	pending []ops.Event
	later   []func() // work to do once the core has returned (list tiling, modal loops)

	postMu   sync.Mutex
	posted   []func()
	postSent bool

	accent    ops.Color
	nextCell  ops.NodeID
	frames    bool
	modules   map[string]func(method string, args []byte) ([]byte, error)
	assets    string
	appName   string
	fonts     map[fontKey]uintptr
	measureDC uintptr
	brushes   map[uint32]uintptr
	anim      *animation
	modal     int // modal loops (menus, dialogs) running
	sheets    []*node
	quitting  bool

	wins     []*node          // the Windows open, in the order they opened
	inUse    uintptr          // the app's window activated last
	minSize  [2]float32       // the main window's least client size (WindowOptions), in points
	appMenus []appMenu        // MenuBar's
	appItems []appItem        // their items, in order
	accel    uintptr          // their shortcuts' accelerator table
	coreEnv  host.Environment // the environment the core was last told

	// The main window's placement (placement.go).
	placementPath  string
	placed         bool // it has shown: what the user does with it is kept
	sizing         bool // the user moves or sizes it
	normal         Rect // its rectangle when not maximized or minimized
	savedPlacement []byte

	// Owner-drawn menu items (menudraw.go).
	owned      map[uintptr]*ownerItem
	ownedNext  uintptr
	ownedMenus []*ownerMenu // the owner-drawn menus
	barOwned   []*ownerMenu // the menu bar's
	menuStyles map[menuStyleKey]*menuStyle
}

var theHost *Host

// newHost makes the app's window. Main thread.
func newHost() *Host {
	DPIAware()
	ActivateCommonControls()
	StartGDIPlus()
	registerClasses()
	h := &Host{
		nodes:   map[ops.NodeID]*node{},
		byHwnd:  map[uintptr]*node{},
		fonts:   map[fontKey]uintptr{},
		brushes: map[uint32]uintptr{},
	}
	theHost = h
	h.appName = appName()
	h.placementPath = placementFile()
	h.assets = assetsDir()
	h.measureDC = CreateCompatibleDC(0)
	h.msgWin = CreateWindow(0, classMessage, "", 0, 0, 0, 0, 0, ^uintptr(2), 0) // HWND_MESSAGE: -3
	h.park = CreateWindow(0, classWindow, "", WS_POPUP|WS_CLIPCHILDREN, 0, 0, 0, 0, 0, 0)
	h.dark = !AppsUseLightTheme()
	switch os.Getenv("RYUI_COLOR_SCHEME") { // automation: snapshots in either
	case "light":
		h.dark, scheme = false, ops.ColorSchemeLight
	case "dark":
		h.dark, scheme = true, ops.ColorSchemeDark
	}
	SetAppDark(h.dark)
	h.theme = newPalette(h.dark, h.accent)
	h.makeWindow()
	h.env = h.environment()
	return h
}

// Run runs root as the app's window until it's closed. Main thread: call
// it from main.
func Run(root func() ui.Node) {
	h := newHost()
	restoreState()
	h.enter(func() { ui.Mount(h, root) })
	// Where the user left it, over WindowOptions' size.
	maximized := h.restorePlacement()
	if os.Getenv("RYUI_SNAPSHOT") != "" {
		// Automation: shown, without taking the user's focus.
		ShowWindow(h.window, SW_SHOWNOACTIVATE)
	} else {
		if maximized {
			ShowWindow(h.window, SW_SHOWMAXIMIZED)
		} else {
			ShowWindow(h.window, SW_SHOWNORMAL)
		}
		SetForegroundWindow(h.window)
	}
	if !maximized {
		h.normal = WindowRect(h.window)
	}
	h.placed = true
	UpdateWindow(h.window)
	for _, n := range h.wins {
		h.showWindow(n.nav.window)
	}
	h.automation()
	h.loop()
	saveState()
}

// loop is the message loop: it dispatches the thread's messages until the
// window is closed.
func (h *Host) loop() {
	var m Msg
	for GetMessage(&m) {
		if m.Message == WM_MOUSEMOVE {
			h.mouseOver(m.Hwnd)
		}
		if h.preTranslate(&m) {
			continue
		}
		TranslateMessage(&m)
		DispatchMessage(&m)
	}
}

// preTranslate handles a key message for one of the app's windows before
// it's dispatched: the menus' shortcuts (TranslateAccelerator, against
// the window it's for), the host's keys, and Tab and arrow key
// navigation among the controls, as in a dialog (IsDialogMessage).
func (h *Host) preTranslate(m *Msg) bool {
	w := h.keyWindow(m.Hwnd)
	if w == 0 {
		return false
	}
	return h.accelerate(w, m) || h.keyMessage(m) || IsDialogMessage(w, m)
}

// keyWindow is the top-level window of the app a message is for: the
// main one, a sheet's or a Window's.
func (h *Host) keyWindow(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	w := RootWindow(hwnd)
	if w == h.window {
		return w
	}
	if n := h.byHwnd[w]; n != nil && (n.kind == kindSheetWindow || n.kind == kindWindowFrame) {
		return w
	}
	return 0
}

// Connect implements host.Connector.
func (h *Host) Connect(c host.Core) { h.core = c }

// Environment implements host.Host.
func (h *Host) Environment() host.Environment {
	h.coreEnv = h.env
	return h.env
}

// Post implements host.Host. Safe from any goroutine.
func (h *Host) Post(f func()) {
	h.postMu.Lock()
	h.posted = append(h.posted, f)
	wake := !h.postSent
	h.postSent = true
	h.postMu.Unlock()
	if wake {
		PostMessage(h.msgWin, wmRunPosted, 0, 0)
	}
}

func (h *Host) runPosted() {
	h.postMu.Lock()
	fs := h.posted
	h.posted = nil
	h.postSent = false
	h.postMu.Unlock()
	for _, f := range fs {
		h.enter(f)
	}
}

// enter runs f, a call into the core; events raised meanwhile (a control
// notifying its parent while a batch is applied) wait until it returns,
// as does work that would re-enter the core (tiling a list) or run a
// modal loop (a menu, a dialog).
func (h *Host) enter(f func()) {
	h.depth++
	f()
	h.depth--
	for h.depth == 0 && (len(h.pending) > 0 || len(h.later) > 0) {
		if len(h.later) > 0 {
			fs := h.later
			h.later = nil
			for _, f := range fs {
				f()
			}
			continue
		}
		ev := h.pending[0]
		h.pending = h.pending[1:]
		h.dispatch(ev)
	}
}

// dispatch sends an event to the core, or queues it while a call into
// the core is in progress.
func (h *Host) dispatch(ev ops.Event) {
	if h.core == nil {
		return
	}
	if h.depth > 0 {
		h.pending = append(h.pending, ev)
		return
	}
	h.enter(func() { h.core.Dispatch(ev) })
}

// emit sends the event if the core listens for it on n.
func (h *Host) emit(n *node, ev ops.Event) {
	if n == nil || !n.listening(ev.Kind) {
		return
	}
	ev.ID = n.id
	h.dispatch(ev)
}

// afterCore runs f once the call into the core in progress has returned
// (now, if none is).
func (h *Host) afterCore(f func()) {
	if h.depth == 0 {
		f()
		return
	}
	h.later = append(h.later, f)
}

// Apply implements host.Host.
func (h *Host) Apply(b *ops.Batch) {
	h.depth++
	defer func() { h.depth-- }()
	for i := range b.Ops {
		h.apply(&b.Ops[i])
	}
}

func (h *Host) apply(op *ops.Op) {
	switch op.Code {
	case ops.OpCreate:
		h.create(op.ID, op.Kind)
	case ops.OpDestroy:
		h.destroy(op.ID)
	case ops.OpInsert:
		p, c := h.nodes[op.ID], h.nodes[op.Child]
		if p != nil && c != nil {
			h.insert(p, c, int(op.Index))
		}
	case ops.OpRemove:
		p, c := h.nodes[op.ID], h.nodes[op.Child]
		if p != nil && c != nil {
			h.remove(p, c)
		}
	case ops.OpSetProp:
		if n := h.nodes[op.ID]; n != nil {
			if op.Prop < ops.FirstKindProp {
				h.setCommon(n, op.Prop, &op.Value)
			} else {
				h.setOwn(n, op.Prop, &op.Value)
			}
		}
	case ops.OpSetFrame:
		if n := h.nodes[op.ID]; n != nil {
			h.setFrame(n, op.Frame)
		}
	case ops.OpListen:
		if n := h.nodes[op.ID]; n != nil && op.Event < 128 {
			n.listens[op.Event/64] |= 1 << (op.Event % 64)
			h.listen(n, op.Event)
		}
	case ops.OpUnlisten:
		if n := h.nodes[op.ID]; n != nil && op.Event < 128 {
			n.listens[op.Event/64] &^= 1 << (op.Event % 64)
		}
	case ops.OpCommand:
		if n := h.nodes[op.ID]; n != nil {
			h.command(n, op.Cmd, op.Value.Bytes)
		}
	case ops.OpSetRoot:
		if n := h.nodes[op.ID]; n != nil {
			h.setRoot(n)
		}
	}
}

func (h *Host) add(n *node) {
	h.nodes[n.id] = n
	if n.hwnd != 0 {
		h.byHwnd[n.hwnd] = n
	}
	if n.ctl != 0 {
		h.byHwnd[n.ctl] = n
	}
}

func (h *Host) destroy(id ops.NodeID) {
	n := h.nodes[id]
	if n == nil {
		return
	}
	delete(h.nodes, id)
	if h.destroyNav(n) {
		return
	}
	if n.parent != nil {
		h.remove(n.parent, n)
	}
	for _, c := range n.children {
		c.parent = nil
	}
	h.release(n)
}

// release lets go of n's windows: DestroyWindow takes its native
// descendants with it, so they're forgotten first (a window handle is
// reused once its window is gone).
func (h *Host) release(n *node) {
	if n.list != nil {
		n.list.release(h)
	}
	if n.web != nil {
		n.web.close()
	}
	if n.image != nil {
		n.image.release()
	}
	if n.hwnd != 0 {
		hw := n.hwnd
		h.forgetTree(hw)
		DestroyWindow(hw)
	}
	n.hwnd, n.ctl = 0, 0
}

var enumChildren = syscall.NewCallback(func(hwnd, l uintptr) uintptr {
	list := Native[[]uintptr](l)
	*list = append(*list, hwnd)
	return 1
})

// forgetTree forgets a window and its descendants.
func (h *Host) forgetTree(hwnd uintptr) {
	var list []uintptr
	EnumChildWindows(hwnd, enumChildren, &list)
	list = append(list, hwnd)
	for _, w := range list {
		Unsubclass(w)
		if n := h.byHwnd[w]; n != nil {
			if n.hwnd == w {
				n.hwnd = 0
			}
			if n.ctl == w {
				n.ctl = 0
			}
			KillTimer(w, timerLongPress)
			KillTimer(w, timerSpin)
			KillTimer(w, timerBars)
			delete(h.byHwnd, w)
		}
	}
}

func (h *Host) insert(p, c *node, index int) {
	if h.insertNav(p, c, index) {
		return
	}
	i := min(index, len(p.children))
	p.children = append(p.children, nil)
	copy(p.children[i+1:], p.children[i:])
	p.children[i] = c
	c.parent = p
	if c.hwnd == 0 || p.hwnd == 0 {
		return
	}
	SetParent(c.hwnd, p.hwnd)
	h.adopt(p, c)
	h.placeFrame(c)
	// Above the windows of the children before it, below those after.
	after := uintptr(HWND_TOP)
	for _, next := range p.children[i+1:] {
		if next.hwnd != 0 && Parent(next.hwnd) == p.hwnd {
			after = next.hwnd
			break
		}
	}
	if after == HWND_TOP {
		// Above the content, below what the host keeps on top (a stack's header).
		after = h.topOfChildren(p)
	}
	SetWindowPos(c.hwnd, after, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	if p.rich != nil {
		p.rich.placeInlines(h, p)
	}
}

func (h *Host) remove(p, c *node) {
	for i, x := range p.children {
		if x == c {
			p.children = append(p.children[:i], p.children[i+1:]...)
			break
		}
	}
	c.parent = nil
	if h.removeNav(p, c) {
		return
	}
	if c.hwnd != 0 {
		// Parked: it may be inserted again (a moved view).
		SetParent(c.hwnd, h.park)
	}
}

func (h *Host) setRoot(n *node) {
	h.root = n
	// The window changed before the root was set (WindowOptions, a menu
	// bar): the core hears of it now.
	if h.env != h.coreEnv {
		h.coreEnv = h.env
		h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: h.env})
	}
	if n.hwnd != 0 {
		SetParent(n.hwnd, h.window)
		r := ClientRect(h.window)
		SetWindowPos(n.hwnd, HWND_BOTTOM, 0, 0, r.W(), r.H(), SWP_NOACTIVATE)
	}
}

// scaleOf is the pixels per point of the top-level window n is in: a
// Window's or sheet's own DPI's (k), else the main window's (n nil, or
// k 0: the main window's views, and those waiting to be inserted).
func (h *Host) scaleOf(n *node) float32 {
	if n != nil && n.k > 0 {
		return n.k
	}
	return h.scale
}

// dpiOf is the DPI of the top-level window n is in.
func (h *Host) dpiOf(n *node) int {
	if n != nil && n.k > 0 {
		return int(math.Round(float64(n.k * 96)))
	}
	return h.dpi
}

// px converts points to pixels in n's window.
func (h *Host) px(n *node, v float32) int32 {
	return int32(math.Round(float64(v * h.scaleOf(n))))
}

// pt converts pixels in n's window to points.
func (h *Host) pt(n *node, v int32) float32 { return float32(v) / h.scaleOf(n) }

func (h *Host) setFrame(n *node, f [4]float32) {
	if n.cell != nil {
		n.cell.setHeight(f[3])
		return
	}
	if n.nav != nil && n.nav.sizedByHost(n) {
		return
	}
	if n.parent != nil && n.parent.rich != nil {
		// An inline component: its size is the core's, its place the text's.
		n.frame[2], n.frame[3] = f[2], f[3]
		n.parent.rich.placeInlines(h, n.parent)
		return
	}
	if h.anim != nil && h.anim.active && n.hwnd != 0 && n.frame != f && IsWindowVisible(n.hwnd) && n.frame[2] > 0 {
		h.anim.add(n, n.frame, f)
		n.frame = f
		return
	}
	n.frame = f
	h.placeFrame(n)
	if p := n.parent; p != nil && p.kind == ops.KindScroll {
		h.updateScroll(p)
	}
}

// placeFrame moves n's window to its frame, in pixels, offset by its
// scroll view's position.
func (h *Host) placeFrame(n *node) {
	if n.hwnd == 0 || n == h.root {
		return
	}
	h.placeAt(n, n.frame)
}

func (h *Host) placeAt(n *node, f [4]float32) {
	x, y := f[0], f[1]
	if p := n.parent; p != nil && p.kind == ops.KindScroll {
		x -= p.scrollX
		y -= p.scrollY
	}
	x0, y0 := h.px(n, x), h.px(n, y)
	w, ht := h.px(n, x+f[2])-x0, h.px(n, y+f[3])-y0
	SetWindowPos(n.hwnd, 0, x0, y0, w, ht, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOOWNERZORDER|SWP_NOCOPYBITS)
	h.resized(n)
}

// resized lays out what's inside a view that changed size: a field's
// control, a list's rows, a stack's screens.
func (h *Host) resized(n *node) {
	switch {
	case n.kind == ops.KindTextField:
		h.layoutField(n)
	case n.list != nil:
		n.list.resized(h)
	case n.kind == ops.KindStack || n.kind == ops.KindTabs:
		h.layoutScreens(n)
	case n.kind == ops.KindScroll:
		h.updateScroll(n)
	case n.kind == ops.KindStepper:
		h.layoutStepper(n)
	case n.rich != nil:
		n.rich.resized(h, n)
	case n.web != nil:
		n.web.resized(h, n)
	}
	if n.cornerRadius > 0 || n.gradient != nil || n.borderWidth > 0 {
		Invalidate(n.hwnd)
	}
}

// Measure implements host.Host.
func (h *Host) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	n := h.nodes[id]
	if n == nil || n.hwnd == 0 {
		return layout.Size{}
	}
	return h.measure(n, c)
}

// appName is the app's name: ryui.json's, or the executable's.
func appName() string {
	if boot.Name != "" {
		return boot.Name
	}
	exe, err := os.Executable()
	if err != nil {
		return "ryui"
	}
	name := exe
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '\\' || name[i] == '/' {
			name = name[i+1:]
			break
		}
	}
	if len(name) > 4 && (name[len(name)-4:] == ".exe" || name[len(name)-4:] == ".EXE") {
		name = name[:len(name)-4]
	}
	return name
}

// assetsDir is where the app's assets are: assets beside the executable
// (ryui build), RYUI_ASSETS, or assets in the working directory (ryui dev,
// go run).
func assetsDir() string {
	if d := os.Getenv("RYUI_ASSETS"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		dir := exe
		for i := len(dir) - 1; i >= 0; i-- {
			if dir[i] == '\\' || dir[i] == '/' {
				dir = dir[:i]
				break
			}
		}
		if st, err := os.Stat(dir + `\assets`); err == nil && st.IsDir() {
			return dir + `\assets`
		}
	}
	return "assets"
}

func fail(format string, args ...any) {
	panic(fmt.Sprintf("ryui: the Windows host "+format, args...))
}

// topOfChildren is where a child inserted last goes in its parent's
// z-order: on top, but under the bar a stack or tabs keep above their
// screens.
func (h *Host) topOfChildren(p *node) uintptr {
	if p.nav != nil && p.nav.bar != nil && p.nav.bar.hwnd != 0 {
		return p.nav.bar.hwnd
	}
	return HWND_TOP
}

// adopt gives a view inserted into p the scale of p's window, if it was
// at another's (made in the parking window, or moved from another
// window).
func (h *Host) adopt(p, c *node) {
	switch {
	case c.k == p.k || c.hwnd == 0:
	case h.scaleOf(c) == h.scaleOf(p):
		h.setScale(c.hwnd, p.k)
	default:
		h.rescale(c.hwnd, p.k)
	}
}
