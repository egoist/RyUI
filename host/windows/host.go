//go:build windows

// Package windows is Asuka's Windows host: Win32 windows and the common
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
	"github.com/egoist/asuka/internal/rtf"
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/egoist/asuka/host"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/devstate"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/internal/winstate"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
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
	listens  [2]uint64 // bits by EventKind
	dir      ops.LayoutDirection
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
	xf                      xform        // Style.Transform's (transform.go)
	zoom                    float32      // the scale it gives what's in it, 0: 1 (own)
	pict                    *picture     // its picture, rotated or faded (picture.go)
	material                ops.Material // Style.Material (material.go)
	clip                    bool         // Style.Clip
	clipRects               []Rect       // its window's region's rows, a rounded clip's (clip.go)
	clipR                   float64      // and its radius, in pixels
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
	twoFinger    *twoFinger  // a pinch or rotate under way (gesture.go)
	searchOf     *node       // the header whose search field it is (search.go)
	native       *nativeView // a Native's: the app's view (native.go)
	refresh      *refresher  // a refreshable Scroll's or List's (refresh.go)
	transition   []byte      // Style.Transition's (transition.go)
	placed       bool        // has had a frame
	match        string      // Style.Match's key (match.go)
	focused      bool
	tracking     bool // a press of the mouse on it is under way (pressed while over it)
	releasing    bool // its capture is let go of by the host: not a cancel
	spaceDown    bool // Space is held on it: pressed until it's let go of

	// Drag and drop (interaction.go).
	dropAccept []string   // DropAccept: the MIME types it takes
	dragItems  []ops.Item // DragItems: what dragging it carries

	// Kind-specific.
	title, message string // Button, Screen, Alert
	variant        int32
	icon           string
	badge          string   // a tab's (Screen's Badge)
	actions        []action // a Screen's bar buttons, an Alert's buttons
	alertStyle     int32
	text           string // Text, TextField's last reported value
	utext          []uint16
	placeholder    string
	multiline      bool
	plain          bool     // TextFieldProps.Plain
	settingText    bool     // the field's text is being set from the core
	pasteTypes     []string // TextFieldProps.PasteTypes
	// A Rich TextField's (richfield.go): its control is a rich edit, the
	// styled text the core sent, its runs as last read, and the formats
	// last reported.
	richText       bool
	richContent    []byte
	richDoc        rtf.Doc                 // the defaults its RTF was written with
	richColors     map[rtf.Color]ops.Color // the colors its spans were sent with, by what they were drawn in
	fieldRuns      []fieldRun
	formatsKnown   bool
	formats        byte
	formatLink     string
	inset          [4]float32 // ContentInset: top, right, bottom, left points
	selection      [2]uint32  // a TextField's, as last reported or set (UTF-16, the core's text)
	selQueued      bool       // a report of its selection waits
	editRTL        bool       // its EDIT was made right to left
	editSide       uint32     // and with this alignment style
	secure         bool
	keyboard       int32
	items          []string // Segmented, Wheel
	pickerLabel    string   // a Picker's Label
	comboList      uintptr  // a Wheel's or Picker's combo box's list window (combo.go)
	comboHot       bool     // the pointer is over its combo box
	selected       int
	pages          int     // a PageControl's
	paging         bool    // a Scroll that moves by pages
	wheelSum       int32   // a paging Scroll's wheel, toward a page
	value          float64 // Slider, Stepper, Progress, ProgressRing, DatePicker
	shown          float64 // ProgressRing: the value drawn, moving to value
	min, max, step float64
	on             bool    // Toggle
	indeterminate  bool    // Progress, ProgressRing
	animating      bool    // Activity
	phase          float64 // Activity, indeterminate Progress
	dateMode       int32
	dateInline     bool // a calendar in place (datepicker.go)
	horizontal     bool // Scroll
	scrollX        float32
	scrollY        float32
	scrollEnd      float32 // right to left, a horizontal Scroll's distance from its trailing end
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
	snack          *snack // a Snackbar's (snackbar.go)
	rich           *rich
	shape          *shape
	web            *web
	presented      bool
	detents        int64
	dying          bool
	interceptBack  bool // ui.OnBack: going back asks the core (Back)
	tooltip        string
	keys           []ops.Key   // its Keys prop's, Control as primary (keys.go)
	canvas         []byte      // a Canvas's display list
	kage           *kageShader // a Shader's program, on the GPU
	paneNode       *node       // a SplitView's pane
	checkbox       bool        // a Toggle drawn as a check box
	pane           *splitPane  // a pane's rows
	a11yLabel      string
	a11yHint       string // the screen reader props (a11y.go)
	a11yRole       int32
	a11yHidden     bool
	a11yActions    []string
	adjustable     bool
	uia            uintptr // its UI Automation provider, once asked for
	colorValue     ops.Color
	host           func(h *Host, n *node, m uint32, w, l uintptr) (uintptr, bool) // a host-made view's messages
	alertOf        *node                                                          // a field of an alert's dialog: the alert
}

// Kinds of the views the host makes itself, past the schema's.
const (
	kindRow ops.Kind = 0xF000 + iota
	kindCell
	kindHeader
	kindTabBar
	kindSheetWindow
	kindScrollBar
	kindWindowFrame   // a Window's top-level window
	kindPopoverWindow // a Popover's popup window
	kindSidebar       // a SplitView's pane
	kindCommandBar    // a stack's command bar: its top screen's Toolbar and Primary (cmdbar.go)
	kindAlertWindow   // an alert's dialog (alertdialog.go)
)

func (n *node) listening(e ops.EventKind) bool {
	return e < 128 && n.listens[e/64]&(1<<(e%64)) != 0
}

type action struct {
	role  ops.AlertButtonRole
	title string
	icon  string
	index int64 // its number in its menu, depth first: what Action carries
	flags ops.MenuFlags
	items []action // a submenu's or section's (ops.MenuSubmenu)
}

func (a *action) is(f ops.MenuFlags) bool { return a.flags&f == f }

// Host is the Windows host.
type Host struct {
	core   host.Core
	nodes  map[ops.NodeID]*node
	byHwnd map[uintptr]*node
	root   *node
	window uintptr // the app's window
	park   uintptr // a hidden window views wait in until they're inserted
	msgWin uintptr // a message-only window: Post's wake-ups
	tips   uintptr // the tooltip control, made with the first tooltip
	env    host.Environment
	dpi    int
	scale  float32 // pixels per point: dpi / 96
	dark   bool
	theme  palette
	snacks []*node // the Snackbars showing, or going (snackbar.go)

	shadowBuf []shadowStep // paintShadows', reused

	roundClips int             // views shaped by a rounded clip (clip.go)
	clipOff    *node           // the one paintCorners draws whole
	drawing    *node           // the view a picture draws (picture.go)
	zoomed     int             // views whose transforms scale what's in them (transform.go)
	pictures   map[*node]*node // rotated views by their pictures (rotate.go)
	skipTree   uintptr         // a window paintTree leaves out
	materials  int             // views with a material (material.go)
	blurTmp    []byte          // the material's blur's buffer

	svc  services                          // the modules of Windows's services (services.go)
	more map[string]map[string]asyncMethod // the modules moduleHooks add (more.go)

	depth   int // calls into the core in progress
	pending []ops.Event
	later   []func() // work to do once the core has returned (list tiling, modal loops)

	postMu   sync.Mutex
	posted   []func()
	postSent bool

	accent      ops.Color
	nextCell    ops.NodeID
	frames      bool
	modules     map[string]func(method string, args []byte) ([]byte, error)
	assets      string
	appName     string
	locale      string // the language the app shows
	rtl         bool   // and its direction
	fonts       map[fontKey]uintptr
	measureDC   uintptr
	brushes     map[uint32]uintptr
	anim        *animation
	exiting     map[uintptr]*node // views going out with a transition, by window (transition.go)
	match       matching          // Style.Match (match.go)
	batchFrames bool              // the batch being applied sets frames
	modal       int               // modal loops (menus, dialogs) running
	sheets      []*node
	quitting    bool

	wins     []*node           // the Windows open, in the order they opened
	inUse    uintptr           // the app's window activated last
	minSize  [2]float32        // the main window's least client size (WindowOptions), in points
	appMenus []ops.MenuBarMenu // MenuBar's
	appItems []ops.MenuItem    // their items, in order, submenus' too (ops.FlattenMenu)
	accel    uintptr           // their shortcuts' accelerator table
	coreEnv  host.Environment  // the environment the core was last told

	// The user's languages and formats, and the app's own language (locale.go).
	intl        intl
	savedLocale string
	localePath  string

	// The windows the app controls (winctl.go), by window: the main one
	// and the Windows'; their hidden title bars' windows (titlebar.go);
	// where their placements are kept (placement.go).
	mainCtl       *wctl
	ctls          map[uintptr]*wctl
	chrome        map[uintptr]*wctl
	placementPath string
	states        *winstate.Store
	statesPath    string
	started       bool // showMain ran: Windows show as they open

	// Owner-drawn menu items (menudraw.go).
	owned      map[uintptr]*ownerItem
	ownedNext  uintptr
	ownedMenus []*ownerMenu // the owner-drawn menus
	barOwned   []*ownerMenu // the menu bar's
	tray       *trayState   // the tray module's icons (tray.go)
	menuStyles map[menuStyleKey]*menuStyle

	// Interaction states and drags (interaction.go).
	hovered        []*node // the views listening for Hover under the pointer
	hoverNext      []*node
	chain          []*node
	hoverViews     int     // views listening for Hover
	hoverWin       uintptr // the app's top-level window the pointer was last over, 0: none
	hoverPt        Point   // where, on the screen
	hoverQueued    bool
	hoverRefreshes int     // refreshes since the pointer last moved
	hovering       bool    // hoverAt is reporting
	hoverAgain     bool    // and was called meanwhile
	tracked        uintptr // the window TrackMouseEvent watches for the pointer leaving
	dragSource     ops.NodeID
	dragSnapshot   []ops.Item // its items as its drag began
	dropHelp       DropHelper
	dropHelperMade bool
	pendingURLs    []string // opened with before the root was mounted

	// The keys a view took (keys_windows.go): their letting go doesn't
	// reach the focused control or the menu bar, nor Alt's if it was held.
	keyUps [256]bool
}

var theHost *Host

// newHost makes the app's window. Main thread.
func newHost() *Host {
	DPIAware()
	ActivateCommonControls()
	OleInitialize() // drag and drop
	StartGDIPlus()
	registerClasses()
	h := &Host{
		nodes:   map[ops.NodeID]*node{},
		byHwnd:  map[uintptr]*node{},
		fonts:   map[fontKey]uintptr{},
		brushes: map[uint32]uintptr{},
		ctls:    map[uintptr]*wctl{},
		chrome:  map[uintptr]*wctl{},
	}
	theHost = h
	h.appName = appName()
	h.placementPath = placementFile()
	h.localePath = localeFile()
	h.savedLocale = readSavedLocale(h.localePath)
	h.intl = readIntl()
	h.assets = assetsDir()
	h.measureDC = CreateCompatibleDC(0)
	// HWND_MESSAGE (-3); titled, for a running-once app, so that a second
	// launch finds it (deeplink_windows.go).
	h.msgWin = CreateWindow(0, classMessage, msgWinTitle, 0, 0, 0, 0, 0, ^uintptr(2), 0)
	h.park = CreateWindow(0, classWindow, "", WS_POPUP|WS_CLIPCHILDREN, 0, 0, 0, 0, 0, 0)
	h.dark = !AppsUseLightTheme()
	switch os.Getenv("ASUKA_COLOR_SCHEME") { // automation: snapshots in either
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
	// Under asuka dev, until the run before this one has stopped: this one
	// starts behind it (state.go).
	stopped := devstate.Wait()
	dir, _ := os.Getwd()
	args := os.Args[1:]
	if launchDockItem >= 0 {
		// The Dock menu's item, for the running instance (tray.go).
		args = append(args, dockItemArg+itoa(launchDockItem))
	}
	if !takeInstance(dir, args) {
		return // the running instance has it
	}
	launch := launchOpens(os.Args[1:], dir)
	startCrash()
	h := newHost()
	restoreState(stopped)
	h.enter(func() { ui.Mount(h, root) })
	if launchDockItem >= 0 {
		h.trayEvent(0, 2, launchDockItem)
		launch = ""
	}
	h.startLinks(launch)
	h.startQuit() // files_windows.go
	h.startToasts()
	switch {
	case launchedForToast:
		h.waitForToast() // notify.go: shown if the click opens the app
	case boot.InBackground() && os.Getenv("ASUKA_SNAPSHOT") == "" && os.Getenv("ASUKA_SCRIPT") == "":
		// In the background: no window at launch (automation shows it,
		// to see it).
		h.started = true
	default:
		h.showMain()
	}
	h.automation()
	h.loop()
	saveState()
}

// showMain shows the app's window, where the user left it (unless it
// opens hidden: WindowAction Show shows it), and its Windows.
func (h *Host) showMain() {
	c := h.mainCtl
	// Where the user left it, over WindowOptions' size and place.
	h.restorePlacement(c)
	h.started = true
	if !c.has(ops.WindowHidden) {
		h.showCtl(c, true)
		UpdateWindow(h.window)
	}
	for _, n := range h.wins {
		if w := n.nav.ctl; w != nil && !w.has(ops.WindowHidden) && !IsWindowVisible(w.hwnd) {
			h.showCtl(w, true)
		}
	}
}

// loop is the message loop: it dispatches the thread's messages until the
// window is closed.
func (h *Host) loop() {
	var m Msg
	for GetMessage(&m) {
		switch m.Message {
		case WM_MOUSEMOVE:
			h.mouseOver(m.Hwnd)
			h.pointerMoved(m.Hwnd, m.Pt, true)
		case WM_MOUSELEAVE:
			h.mouseLeft(m.Hwnd)
		}
		if h.preTranslate(&m) {
			continue
		}
		TranslateMessage(&m)
		DispatchMessage(&m)
	}
}

// preTranslate handles a key message for one of the app's windows before
// it's dispatched: a view's Keys, the menus' shortcuts (TranslateAccelerator, against
// the window it's for), the host's keys, and Tab and arrow key
// navigation among the controls, as in a dialog (IsDialogMessage).
func (h *Host) preTranslate(m *Msg) bool {
	if h.routeKey(m) { // the views' Keys, first (keys_windows.go)
		return true
	}
	w := h.keyWindow(m.Hwnd)
	if w == 0 {
		return false
	}
	if d := h.byHwnd[w]; d != nil && d.kind == kindAlertWindow {
		// An alert's dialog is modal, as a task dialog is: the menus'
		// shortcuts wait, and its fields keep Ctrl+C and the like.
		return h.keyMessage(m) || IsDialogMessage(w, m)
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
	if n := h.byHwnd[w]; n != nil && (n.kind == kindSheetWindow || n.kind == kindWindowFrame || n.kind == kindAlertWindow) {
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
	if n != nil && n.searchOf != nil {
		h.searchEvent(n, ev) // a header's search field: its screen's
		return
	}
	if n != nil && n.alertOf != nil {
		h.alertFieldEvent(n, ev) // an alert's field: the alert's (alertdialog.go)
		return
	}
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
	h.batchFrames = false
	for i := range b.Ops {
		h.batchFrames = h.batchFrames || b.Ops[i].Code == ops.OpSetFrame
		h.apply(&b.Ops[i])
	}
	h.match.frames = nil // what the destroying noted
	h.refreshHover()
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
			if op.Event == ops.EventHover && !n.listening(op.Event) {
				h.hoverViews++
			}
			n.listens[op.Event/64] |= 1 << (op.Event % 64)
			h.listen(n, op.Event)
		}
	case ops.OpUnlisten:
		if n := h.nodes[op.ID]; n != nil && op.Event < 128 {
			if op.Event == ops.EventHover && n.listening(op.Event) {
				h.hoverViews--
			}
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
	h.destroyNative(n)
	h.forgetInteraction(n)
	if h.destroyNav(n) {
		return
	}
	if n.tooltip != "" {
		SetTooltip(h.tips, n.hwnd, "")
	}
	// Going out to its match or with a transition, or inside a view that
	// is: on screen until it's gone.
	if h.matchOut(n) || h.exitTransition(n) {
		return
	}
	if h.inExiting(n.hwnd) {
		if n.parent != nil {
			for i, x := range n.parent.children {
				if x == n {
					n.parent.children = append(n.parent.children[:i], n.parent.children[i+1:]...)
					break
				}
			}
			n.parent = nil
		}
		return // its window goes with the one it's in
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
	if n.image != nil {
		n.image.release()
	}
	if n.kage != nil {
		n.kage.release()
	}
	if n.clipRects != nil {
		n.clipRects = nil
		h.roundClips--
	}
	if n.material != ops.MaterialNone {
		n.material = ops.MaterialNone
		h.materials--
	}
	if n.pict != nil {
		p := n.pict.pic
		n.pict = nil
		h.dropPicture(p) // picture.go
	}
	if n.zoom != 0 {
		n.zoom = 0
		h.zoomed--
	}
	if n.hwnd != 0 {
		hw := n.hwnd
		h.forgetTree(hw)
		if n.web != nil {
			n.web.close(hw) // its window goes after its controller
		} else {
			DestroyWindow(hw)
		}
	} else if n.web != nil {
		n.web.close(0)
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
		delete(h.exiting, w) // a handle goes with its window
		if n := h.byHwnd[w]; n != nil {
			if n.hwnd == w {
				h.dropProvider(n, w)
				n.hwnd = 0
			}
			if n.ctl == w {
				if n.kind == ops.KindPicker {
					ClearAccessibleName(w) // as Dynamic Annotation asks
				}
				n.ctl = 0
			}
			KillTimer(w, timerLongPress)
			KillTimer(w, timerSpin)
			KillTimer(w, timerBars)
			KillTimer(w, timerShader)
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
	h.matchIn(c)
	h.adopt(p, c)
	if p.opacity < 1 && p.pict == nil {
		h.updatePicture(p) // faded with child windows now (picture.go)
	}
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

// scaleOfK is the pixels per point of a view of k (0: the main window's).
func (h *Host) scaleOfK(k float32) float32 {
	if k > 0 {
		return k
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
	if h.enterTransition(n, f) {
		return
	}
	if h.anim != nil && h.anim.active && n.hwnd != 0 && n.frame != f && IsWindowVisible(n.hwnd) && n.frame[2] > 0 {
		h.anim.add(n, n.frame, f)
		n.frame = f
		return
	}
	n.frame = f
	n.placed = true
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
	if p := n.parent; p != nil && p.paneNode != nil {
		h.layoutSplit(p) // a SplitView's detail: beside its pane
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
	if n.xf != (xform{}) && n.xf != identity {
		var x1, y1 int32
		x0, y0, x1, y1 = h.transformed(n, x, y, f[2], f[3]) // transform.go
		w, ht = x1-x0, y1-y0
	}
	shadow := len(n.shadow) > 0
	if shadow {
		h.invalidateShadow(n) // where it was
	}
	SetWindowPos(n.hwnd, 0, x0, y0, w, ht, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOOWNERZORDER|SWP_NOCOPYBITS)
	if shadow {
		h.invalidateShadow(n)
	}
	if n.pict != nil {
		h.placePicture(n) // picture.go
	}
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
	case n.paneNode != nil:
		h.layoutSplit(n)
	case n.kind == ops.KindScroll:
		h.updateScroll(n)
	case n.kind == ops.KindStepper:
		h.layoutStepper(n)
	case n.rich != nil:
		n.rich.resized(h, n)
	case n.web != nil:
		n.web.resized(h, n)
	case n.kage != nil:
		h.renderShader(n) // a frame for its size, before it paints
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

// appName is the app's name: asuka.json's, or the executable's.
func appName() string {
	if boot.Name != "" {
		return boot.Name
	}
	exe, err := os.Executable()
	if err != nil {
		return "asuka"
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
// (asuka build), ASUKA_ASSETS, or assets in the working directory (asuka dev,
// go run).
func assetsDir() string {
	if d := os.Getenv("ASUKA_ASSETS"); d != "" {
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
	panic(fmt.Sprintf("asuka: the Windows host "+format, args...))
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
	// What's in a view has its k times the scale of the transforms on the
	// way (transform.go).
	switch k := h.kFor(p.k, c.own()); {
	case c.k == k || c.hwnd == 0:
	case h.scaleOf(c) == h.scaleOfK(k):
		h.setScale(c.hwnd, p.k)
	default:
		h.rescale(c.hwnd, p.k)
	}
}
