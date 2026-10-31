//go:build darwin && !ios

// Package darwin is nui's macOS host: AppKit views made and driven from
// Go through the Objective-C runtime (purego, no cgo), in the app's
// process. It reads the core's batches as Go structs, without encoding
// them.
//
// Run starts the app: it locks the main goroutine to the main thread
// (this package's init does), makes the application and a window whose
// content is the root, and runs AppKit's event loop.
package darwin

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/internal/boot"
	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/ui"
)

// AppKit calls come on the main thread, where main starts.
func init() { runtime.LockOSThread() }

// node is a view the core made, or a list cell the host made.
type node struct {
	id       ops.NodeID
	kind     ops.Kind
	view     ID // its NSView
	children []*node
	parent   *node
	listens  [2]uint64 // bits by EventKind

	// Text props.
	textStyle, fontWeight, textAlign int32
	fontSize                         float32
	fontFamily                       string
	color                            ops.Color
	lineLimit                        int

	// Visual props.
	background, borderColor ops.Color
	cornerRadius            float64
	borderWidth             float64
	shadow                  []byte
	gradient                []byte
	gradientLayer           ID
	transform               []byte
	effect                  ID // an NSVisualEffectView behind its content (Material)
	frame                   Rect

	menu        ID // an NSMenu (the Menu prop)
	menuActions []action
	menuTrigger int32

	// Kind-specific.
	title, message string // Button, Screen, Alert
	variant        int32
	icon           string
	actions        []action // a Screen's bar buttons, an Alert's buttons
	alertStyle     int32
	text           string // Text, TextField's last reported value
	multiline      bool
	items          []string // Segmented, Wheel
	source         string   // Image
	placeholder    string
	loaded         bool
	imageMode      int32
	symbol         string
	list           *list
	cell           *cell
	nav            *nav
	rich           *rich
	shape          *shape
	web            *web
	video          *video
	selected       int
	min, max       float64
	scroll         bool // observes its scroll position
	dying          bool
	presented      bool
	detents        int64
	tracking       ID // a TextField's text view (Multiline)
	a11yLabel      string
	adjustable     bool
	placed         bool   // has had a frame
	transition     []byte // Style.Transition's
	entering       bool
}

func (n *node) listening(e ops.EventKind) bool {
	return e < 128 && n.listens[e/64]&(1<<(e%64)) != 0
}

// container is the view children's views go in.
func (n *node) container() ID {
	switch {
	case n.cell != nil:
		return n.view
	case n.kind == ops.KindScroll:
		return 0 // its child is the document view
	}
	return n.view
}

type action struct {
	role  ops.AlertButtonRole
	title string
	icon  string
}

// Host is the macOS host.
type Host struct {
	core    host.Core
	nodes   map[ops.NodeID]*node
	byView  map[ID]*node
	byMenu  map[ID]*node
	root    *node
	window  ID
	content ID // the window's content view
	target  ID // NuiTarget: actions, delegates and notifications
	env     host.Environment

	depth   int // calls into the core in progress
	pending []ops.Event
	later   []func() // work to do once the core has returned (list reloads)

	postMu sync.Mutex
	posted []func()
	source *MainSource

	animating    bool
	anim         animation
	accent       ops.Color
	nextCell     ops.NodeID
	frames       *frameClock
	modules      map[string]map[string]method
	assets       string
	appName      string
	dark         bool
	lists        map[ID]*list
	videos       map[ID]ops.NodeID // by AVPlayerItem
	main         *win              // the main window, holding the root
	wins         map[ID]*win       // the Windows' windows, by NSWindow
	appMenus     []appMenu         // MenuBar's
	chromeQueued bool
	a11yActions  map[ID]a11yAction
	exiting      map[ID]bool // views going out with a transition
}

var theHost *Host

// newHost makes the application and its window. Main thread.
func newHost() *Host {
	Load()
	registerClasses()
	h := &Host{
		nodes:       map[ops.NodeID]*node{},
		byView:      map[ID]*node{},
		byMenu:      map[ID]*node{},
		lists:       map[ID]*list{},
		videos:      map[ID]ops.NodeID{},
		a11yActions: map[ID]a11yAction{},
		exiting:     map[ID]bool{},
		wins:        map[ID]*win{},
		target:      New("NuiTarget"),
	}
	purego.RegisterFunc(&msgShowPicker, MsgSendAddr())
	theHost = h
	h.appName = appName()
	h.assets = assetsDir()
	h.source = NewMainSource(h.runPosted)
	app := Send(Class("NSApplication"), "sharedApplication")
	Send(app, "setActivationPolicy:", 0) // regular: a Dock icon and a menu bar
	Send(Class("NSWindow"), "setAllowsAutomaticWindowTabbing:", 0)
	Send(app, "setDelegate:", uintptr(h.target))
	h.makeMenu(app)
	h.makeWindow()
	h.env = h.environment()
	h.observe()
	return h
}

// Run runs root as the app's window until the app quits. Main thread:
// call it from main.
func Run(root func() ui.Node) {
	if !IsMainThread() {
		panic("nui: darwin.Run must be called from main, on the main thread")
	}
	WithPool(func() {
		h := newHost()
		restoreState()
		h.enter(func() { ui.Mount(h, root) })
		if os.Getenv("NUI_SNAPSHOT") != "" {
			// Automation: shown, without taking the user's focus.
			Send(h.window, "orderFront:", 0)
		} else {
			Send(h.window, "makeKeyAndOrderFront:", 0)
			// A plain executable (go run, nui dev) doesn't come to the
			// front on its own.
			Send(Send(Class("NSApplication"), "sharedApplication"), "activateIgnoringOtherApps:", 1)
		}
		h.automation()
	})
	Send(Send(Class("NSApplication"), "sharedApplication"), "run")
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
		h.source.Signal()
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

// enter runs f, a call into the core; events raised meanwhile (AppKit
// calling a delegate while a batch is applied) wait until it returns,
// as does work AppKit would do re-entrantly (reloading a list).
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

// afterCore runs f once the call into the core in progress has
// returned (now, if none is).
func (h *Host) afterCore(f func()) {
	if h.depth == 0 {
		f()
		return
	}
	h.later = append(h.later, f)
}

// Apply implements host.Host.
func (h *Host) Apply(b *ops.Batch) {
	WithPool(func() {
		h.depth++
		defer func() { h.depth-- }()
		for i := range b.Ops {
			h.apply(&b.Ops[i])
		}
	})
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
	if n.view != 0 {
		h.byView[n.view] = n
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
	// Going out with a transition, or inside a view that is: shown until
	// it's gone.
	if h.exit(n) || h.inExiting(n.view) {
		if n.parent != nil {
			for i, x := range n.parent.children {
				if x == n {
					n.parent.children = append(n.parent.children[:i], n.parent.children[i+1:]...)
					break
				}
			}
			n.parent = nil
		}
		h.forget(n)
		return
	}
	if n.parent != nil {
		h.remove(n.parent, n)
	}
	h.release(n)
}

// release lets go of n's native objects.
func (h *Host) release(n *node) {
	for a, x := range h.a11yActions {
		if x.n == n {
			delete(h.a11yActions, a)
			Release(a)
		}
	}
	if n.view != 0 {
		Send(n.view, "removeFromSuperview")
		delete(h.byView, n.view)
		if n.web != nil {
			n.web.close()
		}
		if n.video != nil {
			n.video.close()
		}
		if n.scroll {
			center := Send(Class("NSNotificationCenter"), "defaultCenter")
			Send(center, "removeObserver:name:object:", uintptr(h.target), 0, uintptr(Send(n.view, "contentView")))
		}
		Release(n.view)
		n.view = 0
	}
	if n.menu != 0 {
		delete(h.byMenu, n.menu)
		Release(n.menu)
		n.menu = 0
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
	if h.animating && c.transition != nil && !c.placed {
		c.entering = true
	}
	if p.kind == ops.KindScroll {
		Send(p.view, "setDocumentView:", uintptr(c.view))
		return
	}
	if p.rich != nil {
		Send(p.view, "addSubview:", uintptr(c.view))
		return
	}
	cv := p.container()
	if cv == 0 || c.view == 0 {
		return
	}
	// Above the views of the children before it, below those after.
	if i+1 < len(p.children) {
		next := p.children[i+1].view
		if next != 0 && Send(next, "superview") == cv {
			Send(cv, "addSubview:positioned:relativeTo:", uintptr(c.view), nsWindowBelow, uintptr(next))
			return
		}
	}
	if p.effect != 0 || p.gradientLayer != 0 {
		Send(cv, "addSubview:positioned:relativeTo:", uintptr(c.view), nsWindowAbove, 0)
		return
	}
	Send(cv, "addSubview:", uintptr(c.view))
}

const (
	nsWindowBelow = ^uintptr(0) // -1
	nsWindowAbove = 1
)

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
	if p.kind == ops.KindScroll {
		if Send(p.view, "documentView") == c.view {
			Send(p.view, "setDocumentView:", 0)
		}
		return
	}
	if c.view != 0 {
		Send(c.view, "removeFromSuperview")
	}
}

func (h *Host) setRoot(n *node) {
	h.root = n
	h.main.root = n
	Send(n.view, "setAutoresizingMask:", 0)
	if Send(n.view, "superview") != h.content {
		Send(h.content, "addSubview:", uintptr(n.view))
	}
	h.updateWindowChrome()
	// The window may have changed since the core read the environment
	// (WindowOptions come in the same batch, before the root is set).
	h.env = h.environment()
	h.dispatch(ops.Event{ID: n.id, Kind: ops.EventEnvironment, Env: h.env})
}

func (h *Host) setFrame(n *node, f [4]float32) {
	r := Rect{Origin: Point{X: float64(f[0]), Y: float64(f[1])}, Size: Size{Width: float64(f[2]), Height: float64(f[3])}}
	if n.cell != nil {
		n.cell.setHeight(r.Size.Height)
		return
	}
	if n.view == 0 || n.nav != nil && n.nav.sizedByHost(n) {
		return
	}
	if n.parent != nil && n.parent.rich != nil {
		// An inline component: its size is the core's, its place the text's.
		n.frame.Size = r.Size
		MsgSetSize(n.view, Sel("setFrameSize:"), r.Size)
		n.parent.rich.placeInlines(h, n.parent)
		return
	}
	n.frame = r
	if n.transform != nil {
		// A transformed view's frame isn't its layout's: set the identity
		// first, then transform again.
		h.setTransform(n, nil)
	}
	v := n.view
	switch {
	case n.entering:
		// Comes in from its transition's state, to its first frame.
		n.entering = false
		h.enter_(n, r)
	case h.animating && n.placed:
		MsgSetRect(Send(v, "animator"), selSetFrame, r)
	default:
		// A view's first frame doesn't animate: it wasn't on screen.
		MsgSetRect(v, selSetFrame, r)
	}
	n.placed = true
	if n.transform != nil {
		h.setTransform(n, n.transform)
	}
	if n.gradientLayer != 0 {
		h.layoutGradient(n)
	}
	if n.effect != 0 {
		MsgSetRect(n.effect, selSetFrame, Rect{Size: r.Size})
	}
	if n.list != nil {
		n.list.resized(h)
	}
	if n.shape != nil {
		n.shape.resized(n)
	}
	if n.cornerRadius != 0 {
		h.applyCorner(n)
	}
	if n.kind == ops.KindStack || n.kind == ops.KindTabs {
		h.layoutScreens(n)
	}
	if n.rich != nil {
		n.rich.placeInlines(h, n)
	}
}

var selSetFrame = SEL(0)

// Measure implements host.Host.
func (h *Host) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	n := h.nodes[id]
	if n == nil || n.view == 0 {
		return layout.Size{}
	}
	var s layout.Size
	WithPool(func() { s = h.measure(n, c) })
	return s
}

// appName is the app's name: its bundle's, or the executable's.
func appName() string {
	if boot.Name != "" {
		return boot.Name
	}
	b := Send(Class("NSBundle"), "mainBundle")
	info := Send(b, "infoDictionary")
	if info != 0 {
		for _, k := range []string{"CFBundleDisplayName", "CFBundleName"} {
			if v := Send(info, "objectForKey:", uintptr(String(k))); v != 0 {
				return GoString(v)
			}
		}
	}
	return GoString(Send(Send(Class("NSProcessInfo"), "processInfo"), "processName"))
}

// assetsDir is where the app's assets are: the bundle's Resources/assets,
// or assets beside the working directory (nui dev, go run).
func assetsDir() string {
	b := Send(Class("NSBundle"), "mainBundle")
	if r := GoString(Send(b, "resourcePath")); r != "" {
		if st, err := os.Stat(r + "/assets"); err == nil && st.IsDir() {
			return r + "/assets"
		}
	}
	if d := os.Getenv("NUI_ASSETS"); d != "" {
		return d
	}
	return "assets"
}

func fail(format string, args ...any) {
	panic(fmt.Sprintf("nui: the macOS host "+format, args...))
}
