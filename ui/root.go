// Package ui is RyUI's component layer: a tree of native views built once
// by components and updated by fine-grained signals.
//
// Components are Go functions that run once and return a Node. Props
// that change are signal.Accessor values; each becomes an effect that
// sends exactly the prop change it causes. Ops collect in a batch; a
// flush applies the props, lays out, and applies the frames: two
// crossings into native code, whatever changed.
package ui

import (
	"encoding/binary"
	"runtime/debug"
	"slices"
	"sync/atomic"
	"time"

	"github.com/egoist/ryui/host"
	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// Root is a mounted tree: on mobile, the app's screen; on desktop
// (later), a window.
type Root struct {
	frames    []*frameFn // OnFrame's
	layoutQ   []*element // OnLayout's, whose frames changed this flush
	laying    int        // OnLayout passes in progress
	framesOn  bool
	host      host.Host
	batch     *ops.Batch   // where ops go
	spare     []*ops.Batch // batches for ops written while one is applied
	el        *element
	env       *signal.Signal[host.Environment]
	envV      host.Environment
	scale     float32
	scales    bool // a provider's environment has another scale (a window on another monitor)
	dispose   func()
	pass      layout.Pass
	kidsQ     []*element
	frameQ    []*element
	boxBuf    []*layout.Node
	lists     []*element
	size      layout.Size
	flushes   int
	measures  measureCache
	provs     []*element // host-sized layout roots
	inlines   []*richState
	inlineBuf []byte

	anim      Animation // what ui.Animate asked for, for this flush
	animating bool
	animArgs  [10]byte

	menuBars   []*menuBar // MenuBar's, mounted; the last shows
	menuItems  []MenuItem // the menus' items, in order (Action's index)
	menuSent   []byte
	menuListen bool
}

var (
	roots       []*Root
	defaultHost atomic.Pointer[hostBox]
)

type hostBox struct{ h host.Host }

// Mount builds root on h, applies it and returns the mounted tree. It
// runs on the main thread.
func Mount(h host.Host, root func() Node) *Root {
	signal.BindThread()
	r := &Root{host: h, batch: new(ops.Batch)}
	r.envV = classify(h.Environment())
	setTimeZone(r.envV.TimeZone)
	r.scale = r.envV.Scale
	if r.scale <= 0 {
		r.scale = 1
	}
	r.env = signal.NewFunc(r.envV, envEqual)
	roots = append(roots, r)
	defaultHost.Store(&hostBox{h})
	if c, ok := h.(host.Connector); ok {
		c.Connect(core{})
	}
	withRoot(r, func() Node {
		signal.Root(func(dispose func()) {
			r.dispose = dispose
			r.el = newElement(ops.KindView, true)
			r.sendAccent()
			r.sendWindowOptions()
			if colorScheme != ColorSchemeSystem {
				r.batch.Command(r.el.id, ops.CmdColorScheme, []byte{byte(colorScheme)})
			}
			st := layout.DefaultStyle
			st.FlexDirection = layout.Column
			st.Width, st.Height = layout.Pct(100), layout.Pct(100)
			r.el.box.SetStyle(st)
			var n Node
			envCtx.Provide(r.env, func() { n = root() })
			if n.e != nil {
				fill(n)
				r.el.appendKid(n.e)
			}
		})
		return Node{}
	})
	r.batch.SetRoot(r.el.id)
	r.Flush()
	return r
}

// Dispose unmounts r: its effects stop and its views are destroyed.
func (r *Root) Dispose() {
	if r.dispose == nil {
		return
	}
	r.dispose()
	r.dispose = nil
	r.el.destroy()
	r.Flush()
	for i, x := range roots {
		if x == r {
			roots = append(roots[:i], roots[i+1:]...)
			break
		}
	}
}

// Environment returns the root view's environment; reading it in an
// effect tracks it.
func (r *Root) Environment() host.Environment { return r.env.Get() }

// Flush applies pending ops, lays out what changed and applies the
// frames. Events and posted functions flush on their own; tests call
// Flush after writing signals directly.
func (r *Root) Flush() {
	r.flushes++
	for _, e := range r.kidsQ {
		e.kidsDirty = false
		if !e.dead {
			e.relink()
		}
	}
	clear(r.kidsQ)
	r.kidsQ = r.kidsQ[:0]
	animating := r.animating
	if animating {
		r.animEnd()
	}
	r.apply()
	r.layout()
	if animating && len(r.frameQ) > 0 {
		r.animBegin()
	}
	for _, e := range r.frameQ {
		e.emitFrame()
	}
	if animating && len(r.frameQ) > 0 {
		r.animEnd()
	}
	r.animating = false
	clear(r.frameQ)
	r.frameQ = r.frameQ[:0]
	r.apply()
	r.reportLayouts()
}

// reportLayouts calls the OnLayout of the views whose frames changed, then
// flushes what they wrote. A few rounds at most: a callback that changes
// its own size in turn can't loop forever.
func (r *Root) reportLayouts() {
	if len(r.layoutQ) == 0 || r.laying >= 3 {
		return
	}
	q := slices.Clone(r.layoutQ)
	clear(r.layoutQ)
	r.layoutQ = r.layoutQ[:0]
	r.laying++
	defer func() { r.laying-- }()
	signal.Batch(func() {
		for _, e := range q {
			if !e.dead && e.h != nil && e.h.layout != nil {
				e.h.layout(Frame{X: e.sent[0], Y: e.sent[1], Width: e.sent[2], Height: e.sent[3]})
			}
		}
	})
	r.Flush()
}

// apply hands the batch to the host. A host may call back into the core
// while it applies one (Android lays a list out at once, binding its
// cells, which flushes): ops written meanwhile go into another batch, so
// a nested flush applies only those.
func (r *Root) apply() {
	b := r.batch
	if b.Len() == 0 {
		return
	}
	if n := len(r.spare); n > 0 {
		r.batch = r.spare[n-1]
		r.spare = r.spare[:n-1]
	} else {
		r.batch = new(ops.Batch)
	}
	r.host.Apply(b)
	b.Reset()
	if r.batch.Len() == 0 {
		r.spare = append(r.spare, r.batch)
		r.batch = b
	} else {
		r.spare = append(r.spare, b)
	}
}

func (r *Root) layout() {
	if r.el == nil || r.el.dead {
		return
	}
	size := layout.Size{W: r.envV.Width, H: r.envV.Height}
	p := &r.pass
	p.Changed = p.Changed[:0]
	if len(r.inlines) > 0 {
		r.layoutInlines(size.W, p)
	}
	if r.el.box.Dirty() || size != r.size {
		r.size = size
		r.el.box.Layout(size, p)
	}
	live := r.provs[:0]
	for _, e := range r.provs {
		if e.dead {
			continue
		}
		live = append(live, e)
		env := e.env.Peek()
		size := layout.Size{W: env.Width, H: env.Height}
		if size.W > 0 && size.H > 0 && (e.box.Dirty() || size != e.laidSize) {
			e.laidSize = size
			e.box.Layout(size, p)
		}
	}
	clear(r.provs[len(live):])
	r.provs = live
	for _, l := range r.lists {
		if !l.dead {
			l.list.layoutCells(p)
		}
	}
	for _, n := range p.Changed {
		e := n.Context.(*element)
		e.eachNative((*element).queueFrame)
	}
	clear(p.Changed)
	p.Changed = p.Changed[:0]
}

// sendAccent tells the host the app's accent color (ryui.json's), before
// any view is made with the theme.
func (r *Root) sendAccent() {
	c, ok := ParseHex(boot.Accent)
	if !ok && boot.DynamicColor == "" {
		return
	}
	var args [9]byte
	binary.LittleEndian.PutUint64(args[:], uint64(c))
	if boot.DynamicColor == "1" {
		args[8] = 1
	}
	r.batch.Command(r.el.id, ops.CmdAccent, args[:])
}

// ColorScheme is light or dark.
type ColorScheme = ops.ColorScheme

const (
	ColorSchemeSystem = ops.ColorSchemeSystem // the system's setting
	ColorSchemeLight  = ops.ColorSchemeLight
	ColorSchemeDark   = ops.ColorSchemeDark
)

var colorScheme ColorScheme

// SetColorScheme shows the app light or dark whatever the system's
// setting, as an app's own appearance setting does (SwiftUI's
// preferredColorScheme; AppCompat's night mode on Android);
// ColorSchemeSystem follows the system again. Semantic colors, the
// platform's controls and the system bars follow, and the environment's
// Dark says which it is. It holds for every root, those mounted later
// too. Main thread.
func SetColorScheme(s ColorScheme) {
	if s == colorScheme {
		return
	}
	colorScheme = s
	for _, r := range roots {
		r.batch.Command(r.el.id, ops.CmdColorScheme, []byte{byte(s)})
	}
}

// layoutInlines lays out the RichTexts' inline components before what
// holds them, which the host measures with their sizes: when those
// changed, it's told first (only then is there a crossing more).
func (r *Root) layoutInlines(width float32, p *layout.Pass) {
	live := r.inlines[:0]
	for _, st := range r.inlines {
		if st.e.dead {
			continue
		}
		live = append(live, st)
		st.layout(width, p)
	}
	clear(r.inlines[len(live):])
	r.inlines = live
	r.apply()
}

// setTimeZone makes the device's time zone Go's local time (Go on iOS and
// Android doesn't know it): time.Now().Format shows what the device does.
func setTimeZone(name string) {
	if name == "" || name == time.Local.String() {
		return
	}
	if loc, err := time.LoadLocation(name); err == nil {
		time.Local = loc
	}
}

func (r *Root) setEnvironment(env host.Environment) {
	setTimeZone(env.TimeZone)
	old := r.envV
	r.envV = env
	if env.Scale > 0 {
		r.scale = env.Scale
	}
	if env.FontScale != old.FontScale || env.Scale != old.Scale {
		r.remeasureText()
	}
	r.env.Set(env)
}

// Remeasure measures the root's views again (what the host measured them
// as may be stale: the remote core's, after a player came back), then
// flushes.
func (r *Root) Remeasure() {
	r.measures = measureCache{}
	r.remeasureText()
	r.Flush()
}

// remeasureText marks the root's measured views (text, controls) for
// measuring again: their fonts changed.
func (r *Root) remeasureText() { r.el.markMeasured() }

// freeMemory collects garbage and gives the Go heap's free pages back to
// the system now: the runtime otherwise keeps them a while after a big
// screen goes, and iOS counts them against the app.
func freeMemory() { debug.FreeOSMemory() }

func flushAll() {
	for _, r := range roots {
		r.Flush()
	}
}

// Dispatch delivers an event from a host, then flushes. Writes in event
// handlers are batched until the handler returns. Main thread.
func Dispatch(ev ops.Event) {
	signal.Batch(func() { dispatch(ev) })
	flushAll()
}

func dispatch(ev ops.Event) {
	switch ev.Kind {
	case ops.EventEnvironment:
		setTimeZone(ev.Env.TimeZone)
		env := classify(ev.Env)
		if e := elems[ev.ID]; e != nil && e.env != nil && !e.dead {
			e.setEnv(env)
			return
		}
		for _, r := range roots {
			if ev.ID == 0 || r.el.id == ev.ID {
				r.setEnvironment(env)
			}
		}
		return
	case ops.EventResult:
		result(ev.Bytes)
		return
	case ops.EventIncomingURL:
		incoming(ev.Str)
		return
	case ops.EventShared:
		sharedText(ev.Str)
		return
	case ops.EventFrame:
		for _, r := range roots {
			if r.el.id == ev.ID {
				r.frame(time.Duration(ev.Float * float64(time.Second)))
			}
		}
		return
	case ops.EventFontLoaded:
		for _, r := range roots {
			r.measures = measureCache{} // its measurements were in another font
			r.remeasureText()
		}
		return
	case ops.EventLifecycle:
		for _, f := range boot.Lifecycle {
			f(int(ev.Int))
		}
		switch ops.Lifecycle(ev.Int) {
		case ops.LifecycleMemoryWarning:
			for _, r := range roots {
				r.measures = measureCache{}
			}
			freeMemory()
		case ops.LifecycleBackground:
			// A backgrounded app is the first the system ends for memory.
			freeMemory()
		}
		return
	}
	e := elems[ev.ID]
	if e == nil || e.h == nil || e.dead {
		return
	}
	h := e.h
	switch ev.Kind {
	case ops.EventPress:
		call(h.press)
	case ops.EventLongPress:
		call(h.longPress)
	case ops.EventSubmit:
		call(h.submit)
	case ops.EventFocus:
		call(h.focus)
	case ops.EventBlur:
		call(h.blur)
	case ops.EventTextChanged:
		if h.text != nil {
			h.text(ev.Str)
		}
	case ops.EventToggled:
		if h.toggled != nil {
			h.toggled(ev.Bool)
		}
	case ops.EventValueChanged:
		if h.value != nil {
			h.value(ev.Float)
		}
	case ops.EventPopped:
		if h.popped != nil {
			h.popped(ev.Int)
		}
	case ops.EventSelected:
		if h.selected != nil {
			h.selected(ev.Int)
		}
	case ops.EventColorChanged:
		if h.color != nil {
			h.color(Color(ev.Int))
		}
	case ops.EventWebState, ops.EventRequest, ops.EventDownload, ops.EventNewWindow:
		if h.web != nil {
			webEvent(h.web, ev)
		}
	case ops.EventAction:
		if h.action != nil {
			h.action(ev.Int)
		}
	case ops.EventDismissed:
		call(h.dismissed)
	case ops.EventRefresh:
		call(h.refresh)
	case ops.EventPan:
		if h.pan != nil {
			h.pan(ev.G)
		}
	case ops.EventPinch:
		if h.pinch != nil {
			h.pinch(ev.G)
		}
	case ops.EventRotate:
		if h.rotate != nil {
			h.rotate(ev.G)
		}
	case ops.EventSwipe:
		if h.swipe != nil {
			h.swipe(SwipeDirection(ev.Int))
		}
	case ops.EventA11yAction:
		if i := int(ev.Int); i >= 0 && i < len(h.a11yActions) {
			h.a11yActions[i]()
		}
	case ops.EventAdjust:
		if h.adjust != nil {
			h.adjust(int(ev.Int))
		}
	case ops.EventEnded:
		if h.ended != nil {
			h.ended()
		}
	case ops.EventNavigated:
		if h.navigated != nil {
			h.navigated(ev.Str)
		}
	case ops.EventMessage:
		if h.message != nil {
			h.message(ev.Str)
		}
	case ops.EventScrolled:
		if h.scrolled != nil {
			h.scrolled(float32(ev.X), float32(ev.Y))
		}
	case ops.EventLoaded:
		if h.loaded != nil {
			h.loaded(float32(ev.X), float32(ev.Y))
		}
	case ops.EventFailed:
		if h.failed != nil {
			h.failed(ev.Str)
		}
	case ops.EventNative:
		if h.native != nil {
			h.native(ev.Bytes)
		}
	case ops.EventLink:
		if h.link != nil {
			h.link(ev.Str)
		}
	}
}

func call(f func()) {
	if f != nil {
		f()
	}
}

// Post runs f on the main thread, then flushes. Safe from any goroutine:
// it's how goroutines update signals.
func Post(f func()) {
	hb := defaultHost.Load()
	if hb == nil {
		panic("ui: Post before Mount")
	}
	hb.h.Post(func() {
		signal.Batch(f)
		flushAll()
	})
}

// core is what hosts call into.
type core struct{}

func (core) Dispatch(ev ops.Event) { Dispatch(ev) }

func (core) ListCount(list ops.NodeID) int {
	if e := elems[list]; e != nil && e.list != nil {
		return e.list.count()
	}
	return 0
}

func (core) ListBind(list ops.NodeID, index int, cell ops.NodeID) {
	e := elems[list]
	if e == nil || e.list == nil {
		return
	}
	signal.Batch(func() { e.list.bind(index, cell) })
	e.r.Flush()
}

func (core) ListUnbind(list ops.NodeID, cell ops.NodeID) {
	e := elems[list]
	if e == nil || e.list == nil {
		return
	}
	signal.Batch(func() { e.list.unbind(cell) })
	e.r.Flush()
}

// Core returns what hosts call into (mobile/ uses it).
func Core() host.Core { return core{} }

// Reattach tells the host again which view is each root's, and resends
// every frame: an Android Activity that was recreated took over the
// existing views. Main thread.
func Reattach() {
	for _, r := range roots {
		r.batch.SetRoot(r.el.id)
		var walk func(e *element)
		walk = func(e *element) {
			if e.real {
				e.sent = [4]float32{nan32, nan32, nan32, nan32}
				e.queueFrame()
			}
			for _, k := range e.kids {
				walk(k)
			}
		}
		walk(r.el)
		r.Flush()
	}
}
