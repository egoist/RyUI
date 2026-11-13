//go:build linux && !android

// Package linux is RyUI's Linux host: GTK 4 widgets, with libadwaita's
// navigation, dialogs and styles, made and driven from Go through purego
// (internal/gtk, no cgo), in the app's process. It reads the core's
// batches as Go structs, without encoding them.
//
// Run starts the app: it locks the main goroutine to the main thread
// (this package's init does), makes the application and a window whose
// content is the root, and runs GLib's main loop.
//
// Each node is a RyuiView, a widget of the host's own class: it draws the
// node's background, border, corners, gradient and shadow, and places its
// children at the frames the core gives them. A control (a label, a
// button, an entry) is the only child of its node's view, filling it less
// the content inset.
package linux

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/ryui/host"
	"github.com/egoist/ryui/internal/boot"
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/ui"
)

// GTK calls come on the main thread, where main starts.
func init() { LockMain() }

// node is a view the core made, or a list cell the host made.
type node struct {
	id       ops.NodeID
	kind     ops.Kind
	widget   Ptr // its RyuiView
	inner    Ptr // the control in it (0: none), filling it less the inset
	children []*node
	parent   *node
	listens  [2]uint64 // bits by EventKind
	win      *win      // the window it's in, for a host-sized root (Screen, Sheet, Window...)

	frame  [4]float32 // x, y, width, height: points, in its parent
	placed bool       // has had a frame
	hidden bool

	// Text props.
	textStyle, fontWeight, textAlign int32
	fontSize                         float32
	fontFamily                       string
	color                            ops.Color
	lineLimit                        int
	inset                            [4]float32 // ContentInset: top, right, bottom, left points

	// Visual props, drawn by the RyuiView.
	background, borderColor ops.Color
	cornerRadius            float32
	borderWidth             float32
	opacity                 float32
	shadow                  shadow
	gradient                []gradientStop
	gradientLine            [4]float32
	transform               transform
	clip                    bool
	material                int32

	menu        []action // the Menu prop
	menuTrigger int32

	// Kind-specific.
	title, message   string // Button, Screen, Alert
	variant          int32
	icon             string
	actions          []action // a Screen's bar buttons, an Alert's buttons
	alertStyle       int32
	text             string // Text, TextField's last reported value
	multiline        bool
	plain            bool
	secure           bool
	placeholder      string
	items            []string // Segmented, Wheel
	selected         int
	value            float64
	min, max, step   float64
	source           string // Image
	imagePlaceholder string
	imageMode        int32
	symbol           string
	texture          Ptr // an Image's decoded picture
	loading          string
	scroll           *scrollView
	nav              *nav
	dying            bool
	disabled         bool
	a11yLabel        string
	interceptBack    bool
	dir              ops.LayoutDirection
	rtl              bool

	handlers []uint64 // signal handlers on inner, disconnected as it goes
	updating bool     // the host is setting the control's value: no event

	field      *field      // a TextField's
	events     *events     // its event controllers (events.go)
	transition *transition // Style.Transition's
	date       *datePicker // a DatePicker's
	loaded     bool        // an Image's Source loaded
	pulsed     time.Time   // an indeterminate Progress's last pulse
	minWidth   float32     // a Window's least content size
	minHeight  float32
	ext        any // an area's own state (a list's, a web view's...)
}

func (n *node) listening(e ops.EventKind) bool {
	return e < 128 && n.listens[e/64]&(1<<(e%64)) != 0
}

type action struct {
	role  ops.AlertButtonRole
	title string
	icon  string
}

// Host is the Linux host.
type Host struct {
	core     host.Core
	nodes    map[ops.NodeID]*node
	byWidget map[Ptr]*node
	root     *node
	app      Ptr
	main     *win
	wins     map[Ptr]*win // by GtkWindow
	env      host.Environment

	depth   int // calls into the core in progress
	pending []ops.Event
	later   []func() // work to do once the core has returned

	postMu sync.Mutex
	posted []func()
	wake   func()

	animating bool
	anim      animation
	accent    ops.Color
	dark      bool
	theme     palette
	nextCell  ops.NodeID
	assets    string
	appName   string
	appID     string
	base      baseFont // the system's UI font
	fontScale float32
	css       Ptr // the app's style provider (accent, the host's classes)

	locale      string
	rtl         bool
	languages   string
	savedLocale string
	localePath  string

	modules       map[string]map[string]method
	windowOptions windowOptions
	activeWin     *win
	screenReader  bool
	hourCycle     uint8
	chromeQueued  bool
	menusVersion  int
	appMenu       Ptr
	appActions    []string
	appItems      []appItem
	popups        []Ptr
	tweens        map[*node]*tween
	ticking       bool
	frames        bool
	inhibit       uint32
	inBackground  bool // no window of the app's shows (lifecycle.go)
	quitting      bool
	fontFamilies  map[string]string
}

var theHost *Host

// Run runs root as the app's window until the app quits. Main thread:
// call it from main.
func Run(root func() ui.Node) {
	startCrash()
	MustLoad()
	h := newHost()
	h.run(root)
}

func newHost() *Host {
	registerClasses()
	h := &Host{
		nodes:    map[ops.NodeID]*node{},
		byWidget: map[Ptr]*node{},
		wins:     map[Ptr]*win{},
	}
	theHost = h
	h.appName = appName()
	h.appID = appID(h.appName)
	h.assets = assetsDir()
	h.wake = Waker(h.runPosted)
	return h
}

func (h *Host) run(root func() ui.Node) {
	// A notification's systemd timer runs the app to show it: no window,
	// no GTK (notify_timer.go).
	if id := os.Getenv("RYUI_NOTIFY"); id != "" {
		os.Exit(h.notifyHeadless(id))
	}
	// One instance per app ID (a second launch activates the first, and
	// hands it the URLs it was opened with), but not in automation runs,
	// tests and ryui dev, nor without an ID of the app's.
	flags := uint32(1 << 2) // G_APPLICATION_HANDLES_OPEN: its URL scheme
	if os.Getenv("RYUI_SNAPSHOT") != "" || os.Getenv("RYUI_SCRIPT") != "" || os.Getenv("RYUI_STATE") != "" ||
		os.Getenv("RYUI_COMMANDS") != "" || boot.ID == "" {
		flags |= 1 << 5 // G_APPLICATION_NON_UNIQUE
	}
	h.app = AdwApplicationNew(h.appID, flags)
	notifyUnique = flags&(1<<5) == 0
	h.installNotifyActions() // a notification due, or tapped, reaches it
	h.watchApp()
	h.watchSignals()
	h.watchMemory()
	started := false
	start := func() bool {
		if started {
			if h.main != nil {
				WindowPresent(h.main.window)
			}
			return false
		}
		started = true
		h.start(root)
		return true
	}
	Connect(h.app, "activate", func() { start() })
	// Opened with URLs (its scheme, from a browser or xdg-open): each is
	// IncomingURL on the root, once it's mounted.
	Connect(h.app, "open", func(files, n, hint uintptr) {
		start()
		for i := 0; i < int(int32(n)); i++ {
			f := *At[Ptr](files + uintptr(i)*8)
			if u := GoStringFree(FileGetURI(f)); u != "" {
				h.incomingURL(u)
			}
		}
	})
	// GApplication reads its arguments: the program and the URLs it was
	// opened with (the desktop file's %U), not the rest, which aren't
	// its to parse.
	args := []string{os.Args[0]}
	for _, a := range os.Args[1:] {
		if strings.Contains(a, ":") && !strings.HasPrefix(a, "-") {
			args = append(args, a)
		}
	}
	argv := make([]*byte, len(args)+1)
	for i, a := range args {
		argv[i] = CString(a)
	}
	os.Exit(int(ApplicationRun(h.app, int32(len(args)), Addr(&argv[0]))))
}

// incomingURL reports a URL the app was opened with to the root.
func (h *Host) incomingURL(u string) {
	h.afterCore(func() {
		if h.root == nil {
			return
		}
		h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventIncomingURL, Str: u})
	})
}

// start makes the main window and mounts the root in it.
func (h *Host) start(root func() ui.Node) {
	h.setup()
	h.enter(func() { ui.Mount(h, root) })
	WindowPresent(h.main.window)
	h.notifyActionAtStart() // launched by a notification's tap
	h.automation()
}

// setup reads the style and the user's languages and makes the main
// window, once the application has started (GTK is initialized).
func (h *Host) setup() {
	h.loadTheme()
	h.initLocale()
	h.main = h.newMainWindow()
	h.theme = h.readPalette(h.main.content)
	h.env = h.environment(h.main)
	h.watchSettings()
}

// Connect implements host.Connector.
func (h *Host) Connect(c host.Core) { h.core = c }

// Environment implements host.Host.
func (h *Host) Environment() host.Environment { return h.env }

// Post implements host.Host. Safe from any goroutine.
func (h *Host) Post(f func()) {
	h.postMu.Lock()
	h.posted = append(h.posted, f)
	first := len(h.posted) == 1
	h.postMu.Unlock()
	if first {
		h.wake()
	}
}

func (h *Host) runPosted() {
	h.postMu.Lock()
	fs := h.posted
	h.posted = nil
	h.postMu.Unlock()
	for _, f := range fs {
		h.enter(f)
	}
}

// enter runs f, a call into the core; events raised meanwhile (GTK
// emitting a signal while a batch is applied) wait until it returns, as
// does work that would call back into the core.
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
	h.batchApplied()
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
			h.unlisten(n, op.Event)
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
	if n.widget != 0 {
		h.byWidget[n.widget] = n
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
	for _, c := range n.children {
		c.parent = nil
	}
	// Going out with a transition: shown until it's gone.
	if h.exit(n) {
		if p := n.parent; p != nil {
			for i, x := range p.children {
				if x == n {
					p.children = append(p.children[:i], p.children[i+1:]...)
					break
				}
			}
			n.parent = nil
		}
		return
	}
	if n.parent != nil {
		h.remove(n.parent, n)
	}
	h.release(n)
}

// release lets go of n's widgets.
func (h *Host) release(n *node) {
	h.releaseMore(n)
	h.releaseEvents(n)
	if n.inner != 0 {
		for _, id := range n.handlers {
			Disconnect(n.inner, id)
		}
		n.handlers = nil
	}
	if n.texture != 0 {
		ObjectUnref(n.texture)
		n.texture = 0
	}
	if n.widget != 0 {
		if WidgetGetParent(n.widget) != 0 {
			h.detach(n.widget)
		}
		delete(h.byWidget, n.widget)
		ObjectUnref(n.widget) // the host's reference, taken as it was made
		n.widget = 0
	}
}

// detach takes a widget out of its parent, whatever kind the parent is.
func (h *Host) detach(w Ptr) {
	p := WidgetGetParent(w)
	if p == 0 {
		return
	}
	if pn := h.byWidget[p]; pn != nil || isView(p) {
		WidgetUnparent(w)
		if pn != nil {
			Call(FnQueueAllocate, p)
		}
		return
	}
	h.detachFromContainer(p, w)
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
	if c.widget == 0 {
		return
	}
	if p.scroll != nil {
		p.scroll.setContent(h, c)
		return
	}
	h.detach(c.widget)
	// Above the widgets of the children before it, below those after: in
	// GTK, later siblings draw on top.
	var next Ptr
	for _, x := range p.children[i+1:] {
		if x.widget != 0 && WidgetGetParent(x.widget) == p.widget {
			next = x.widget
			break
		}
	}
	if next != 0 {
		WidgetInsertBefore(c.widget, p.widget, next)
	} else {
		WidgetInsertBefore(c.widget, p.widget, 0) // last
	}
	Call(FnQueueAllocate, p.widget)
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
	if p.scroll != nil {
		p.scroll.setContent(h, nil)
		return
	}
	if c.widget != 0 && WidgetGetParent(c.widget) == p.widget {
		WidgetUnparent(c.widget)
		Call(FnQueueAllocate, p.widget)
	}
}

func (h *Host) setRoot(n *node) {
	h.root = n
	h.main.setRoot(h, n)
	// The window may have changed since the core read the environment
	// (WindowOptions come in the same batch, before the root is set).
	h.env = h.environment(h.main)
	h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: h.env})
}

func (h *Host) setFrame(n *node, f [4]float32) {
	if h.frameMore(n, f) {
		return
	}
	if n.frame == f && n.placed {
		return
	}
	was, old := n.placed, n.frame
	n.frame = f
	n.placed = true
	if n.widget == 0 {
		return
	}
	if p := n.parent; p != nil && p.scroll != nil {
		// A scroll's content: its size is what scrolls.
		WidgetQueueResize(n.widget)
		return
	}
	if n.kind == ops.KindTabs {
		h.tabsResized(n)
	}
	switch {
	case h.animating && was:
		h.animate(n, old)
		return
	case h.animating && n.transition != nil:
		// Comes in from its transition's state; its first frame doesn't
		// animate.
		h.enterFrom(n)
	}
	h.relayout(n)
}

// Measure implements host.Host.
func (h *Host) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	n := h.nodes[id]
	if n == nil || n.widget == 0 {
		return layout.Size{}
	}
	return h.measure(n, c)
}

func (h *Host) batchApplied() {
	h.updateChrome()
}

func fail(format string, args ...any) {
	panic(fmt.Sprintf("ryui: the Linux host "+format, args...))
}
