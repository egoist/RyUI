// Package fake is a host for tests. It records every op, keeps a tree of
// pretend views (panicking on any op a real host couldn't apply),
// measures views by fixed rules, and counts its work.
//
// Measuring rules: Text is 8pt per character on 20pt lines, wrapping at
// the max width; Button is its title at 8pt per character plus 24, by
// 44; TextField 200×36; Toggle 51×31; Slider takes the max width (200 if
// unbounded) by 30; Activity 20×20; anything else 0×0.
package fake

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/host"
	"github.com/egoist/asuka/internal/fileurl"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
)

// KindCell is the kind the fake host gives the list cells it makes.
const KindCell ops.Kind = 0xffff

// View is a pretend native view.
type View struct {
	ID       ops.NodeID
	Kind     ops.Kind
	Parent   *View
	Children []*View
	Props    map[ops.PropID]ops.Value
	Frame    [4]float32
	Listens  map[ops.EventKind]bool
	Root     bool
	// A List's, as its last ListUpdate had them.
	Items    []int64
	Sections []ListSection

	presented bool
	// list is the List a cell is bound into (Bind): keys go on there
	// from it.
	list *View
}

// ListSection is a List's section, as ListUpdate sends it.
type ListSection struct {
	Start int
	ID    int64
}

// String returns a prop as text ("" when unset).
func (v *View) String(p ops.PropID) string { return v.Props[p].Str }

// Bool returns a bool prop.
func (v *View) Bool(p ops.PropID) bool { return v.Props[p].Int != 0 }

// Int returns an int (or enum) prop.
func (v *View) Int(p ops.PropID) int64 { return v.Props[p].Int }

// Float returns a float prop.
func (v *View) Float(p ops.PropID) float64 { return v.Props[p].Float }

// Strings decodes a list of strings prop (a Wheel's or Picker's items, a
// Segmented control's segments).
func (v *View) Strings(p ops.PropID) []string { return ops.DecodeStrings(v.Props[p].Bytes) }

// Host is the fake host.
type Host struct {
	Views map[ops.NodeID]*View
	// Ops are the ops applied since the last Reset.
	Ops []ops.Op
	// Applies counts Apply calls; Measures counts Measure calls.
	Applies, Measures int
	Env               host.Environment
	// MeasureFunc, if set, measures views instead of the built-in rules.
	MeasureFunc func(v *View, c layout.Constraints) layout.Size
	// Modules answer Calls, by module name: as a host's, with Result
	// events (posted: run them with RunPosted).
	Modules map[string]func(method string, args []byte) ([]byte, error)
	// AsyncModules answer Calls later, as a host's methods that wait do
	// (a "next" answered at the next change): reply posts the Result,
	// once, from any goroutine. A module here is asked before Modules.
	AsyncModules map[string]func(method string, args []byte, reply func([]byte, error))

	core     host.Core
	posted   []func()
	postMu   sync.Mutex
	wake     chan struct{} // a Post's signal to WaitPost
	nextCell ops.NodeID
	opsAlloc []byte
	wins     map[ops.NodeID]*WindowInfo // window.go
	key      ops.NodeID                 // the window in use (window.go): 0 the main one
}

// New makes a fake host with a 390×844 portrait phone environment.
func New() *Host {
	return &Host{
		Views: map[ops.NodeID]*View{},
		wake:  make(chan struct{}, 1),
		Env: host.Environment{
			Width: 390, Height: 844, SafeTop: 47, SafeBottom: 34,
			Scale: 3, FontScale: 1, Locale: "en_US", Languages: "en-US",
		},
	}
}

// Connect implements host.Connector.
func (h *Host) Connect(c host.Core) { h.core = c }

// Environment implements host.Host.
func (h *Host) Environment() host.Environment { return h.Env }

// Post implements host.Host: f runs at the next RunPosted. Safe from
// any goroutine.
func (h *Host) Post(f func()) {
	h.postMu.Lock()
	h.posted = append(h.posted, f)
	h.postMu.Unlock()
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Pending reports how many posted functions wait for RunPosted.
func (h *Host) Pending() int {
	h.postMu.Lock()
	defer h.postMu.Unlock()
	return len(h.posted)
}

// WaitPost waits until a function is posted, or d has passed, and
// reports whether one waits for RunPosted: what a test waits on while
// the app's goroutines work. Main thread.
func (h *Host) WaitPost(d time.Duration) bool {
	if h.Pending() > 0 {
		return true
	}
	if h.wake == nil {
		return false
	}
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-h.wake:
			if h.Pending() > 0 {
				return true
			}
		case <-t.C:
			return h.Pending() > 0
		}
	}
}

// RunPosted runs what was posted, in order, as the main thread would.
func (h *Host) RunPosted() {
	for {
		h.postMu.Lock()
		if len(h.posted) == 0 {
			h.postMu.Unlock()
			return
		}
		f := h.posted[0]
		h.posted = h.posted[1:]
		h.postMu.Unlock()
		f()
	}
}

// Reset forgets the recorded ops and counts.
func (h *Host) Reset() {
	clear(h.Ops)
	h.Ops = h.Ops[:0]
	h.opsAlloc = nil // the views' props still hold the bytes in it
	h.Applies, h.Measures = 0, 0
}

// Count returns how many recorded ops have code c.
func (h *Host) Count(c ops.Code) int {
	n := 0
	for i := range h.Ops {
		if h.Ops[i].Code == c {
			n++
		}
	}
	return n
}

// Log prints the recorded ops with kind-aware prop names.
func (h *Host) Log() string {
	var b strings.Builder
	for i := range h.Ops {
		op := &h.Ops[i]
		if op.Code == ops.OpSetProp {
			k := ops.Kind(0)
			if v := h.Views[op.ID]; v != nil {
				k = v.Kind
			}
			fmt.Fprintf(&b, "SetProp %d %s=%s\n", op.ID, ops.KindPropName(k, op.Prop), op.Value)
			continue
		}
		b.WriteString(op.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func (h *Host) view(id ops.NodeID, op string) *View {
	v := h.Views[id]
	if v == nil {
		panic(fmt.Sprintf("fake: %s: no view %d", op, id))
	}
	return v
}

// Apply implements host.Host.
func (h *Host) Apply(b *ops.Batch) {
	h.Applies++
	for i := range b.Ops {
		op := b.Ops[i]
		if len(op.Value.Bytes) > 0 { // the batch's bytes die with it
			start := len(h.opsAlloc)
			h.opsAlloc = append(h.opsAlloc, op.Value.Bytes...)
			op.Value.Bytes = h.opsAlloc[start:]
		}
		h.Ops = append(h.Ops, op)
		h.apply(&op)
	}
}

func (h *Host) apply(op *ops.Op) {
	switch op.Code {
	case ops.OpCreate:
		if h.Views[op.ID] != nil {
			panic(fmt.Sprintf("fake: Create: view %d exists", op.ID))
		}
		if op.ID&ops.HostIDBit != 0 {
			panic(fmt.Sprintf("fake: Create: %d is a host ID", op.ID))
		}
		if op.Kind.String() == fmt.Sprintf("Kind(%d)", op.Kind) {
			panic(fmt.Sprintf("fake: Create: unknown kind %d", op.Kind))
		}
		h.Views[op.ID] = &View{ID: op.ID, Kind: op.Kind, Props: map[ops.PropID]ops.Value{}, Listens: map[ops.EventKind]bool{}}
	case ops.OpDestroy:
		v := h.view(op.ID, "Destroy")
		if v.Parent != nil {
			v.Parent.removeChild(v)
		}
		for _, c := range v.Children {
			c.Parent = nil
		}
		delete(h.Views, op.ID)
		delete(h.wins, op.ID)
	case ops.OpInsert:
		p, c := h.view(op.ID, "Insert"), h.view(op.Child, "Insert")
		if c.Parent != nil {
			panic(fmt.Sprintf("fake: Insert: %d already has parent %d", c.ID, c.Parent.ID))
		}
		if int(op.Index) > len(p.Children) {
			panic(fmt.Sprintf("fake: Insert: index %d of %d into %d", op.Index, len(p.Children), p.ID))
		}
		if (p.Kind == ops.KindScroll || p.Kind == ops.KindSheet || p.Kind == ops.KindWindow) && len(p.Children) > 0 {
			panic(fmt.Sprintf("fake: Insert: a %s has one child", p.Kind))
		}
		if (p.Kind == ops.KindStack || p.Kind == ops.KindTabs) && c.Kind != ops.KindScreen {
			panic(fmt.Sprintf("fake: Insert: a %s holds Screens, not a %s", p.Kind, c.Kind))
		}
		p.Children = slices.Insert(p.Children, int(op.Index), c)
		c.Parent = p
	case ops.OpRemove:
		p, c := h.view(op.ID, "Remove"), h.view(op.Child, "Remove")
		if c.Parent != p {
			panic(fmt.Sprintf("fake: Remove: %d is not a child of %d", c.ID, p.ID))
		}
		p.removeChild(c)
	case ops.OpSetProp:
		v := h.view(op.ID, "SetProp")
		t, ok := ops.PropType(v.Kind, op.Prop)
		if !ok || t != op.Value.Type {
			panic(fmt.Sprintf("fake: SetProp: %s has no %s prop %d", v.Kind, typeName(op.Value.Type), op.Prop))
		}
		if v.Kind == ops.KindSheet && op.Prop == ops.PropSheetFullScreen && v.presented {
			panic("fake: SetProp: a sheet's FullScreen after its Present: a host presents it as it was")
		}
		v.Props[op.Prop] = op.Value
	case ops.OpSetFrame:
		h.view(op.ID, "SetFrame").Frame = op.Frame
	case ops.OpListen:
		v := h.view(op.ID, "Listen")
		v.Listens[op.Event] = true
		if op.Event == ops.EventWindowState {
			h.reportWindow(v)
		}
	case ops.OpUnlisten:
		delete(h.view(op.ID, "Unlisten").Listens, op.Event)
	case ops.OpCommand:
		v := h.view(op.ID, "Command")
		if op.Cmd == ops.CmdCall {
			h.call(op.ID, op.Value.Bytes)
			return
		}
		if op.Cmd == ops.CmdListUpdate {
			v.Items, v.Sections = decodeListUpdate(op.Value.Bytes)
		}
		if op.Cmd == ops.CmdPresent {
			if v.Kind != ops.KindSheet && v.Kind != ops.KindPopover && v.Kind != ops.KindAlert && v.Kind != ops.KindWindow && v.Kind != ops.KindSnackbar {
				panic(fmt.Sprintf("fake: Present on a %s", v.Kind))
			}
			v.presented = true
		}
		h.windowCommand(v, op.Cmd, op.Value.Bytes)
	case ops.OpSetRoot:
		for _, v := range h.Views {
			v.Root = false
		}
		h.view(op.ID, "SetRoot").Root = true
	default:
		panic(fmt.Sprintf("fake: unknown op %d", op.Code))
	}
}

// decodeListUpdate decodes ListUpdate's args: the item IDs, then the
// sections, if there are any.
func decodeListUpdate(b []byte) ([]int64, []ListSection) {
	le := binary.LittleEndian
	n := int(le.Uint32(b))
	b = b[4:]
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(le.Uint64(b[8*i:]))
	}
	b = b[8*n:]
	if len(b) < 4 {
		return ids, nil
	}
	ns := int(le.Uint32(b))
	b = b[4:]
	var secs []ListSection
	for i := range ns {
		secs = append(secs, ListSection{Start: int(le.Uint32(b[12*i:])), ID: int64(le.Uint64(b[12*i+4:]))})
	}
	if secs != nil && secs[0].Start != 0 {
		panic("fake: a list's first section doesn't start at 0")
	}
	return ids, secs
}

func typeName(t ops.Type) string {
	return [...]string{"?", "bool", "int", "float", "string", "color", "edges", "enum", "bytes"}[t]
}

func (v *View) removeChild(c *View) {
	i := slices.Index(v.Children, c)
	v.Children = slices.Delete(v.Children, i, i+1)
	c.Parent = nil
}

// Measure implements host.Host.
func (h *Host) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	h.Measures++
	v := h.view(id, "Measure")
	if h.MeasureFunc != nil {
		return h.MeasureFunc(v, c)
	}
	var s layout.Size
	switch v.Kind {
	case ops.KindText:
		s = measureText(v.String(ops.PropTextText), c)
	case ops.KindButton:
		s = layout.Size{W: float32(len([]rune(v.String(ops.PropButtonTitle))))*8 + 24, H: 44}
	case ops.KindTextField:
		s = layout.Size{W: 200, H: 36}
		if v.Bool(ops.PropTextFieldMultiline) {
			// 20pt lines, as many as its text has, up to its line limit.
			lines := strings.Count(v.String(ops.PropTextFieldValue), "\n") + 1
			if l := int(v.Props[ops.PropLineLimit].Int); l > 0 {
				lines = min(lines, l)
			}
			s.H = float32(16 + 20*lines)
		}
	case ops.KindToggle:
		s = layout.Size{W: 51, H: 31}
	case ops.KindPageControl:
		s = layout.Size{W: float32(v.Int(ops.PropPageControlCount))*16 + 16, H: 26}
	case ops.KindSlider:
		s = layout.Size{W: 200, H: 30}
		if fin(c.AvailW) {
			s.W = c.AvailW
		}
	case ops.KindActivity:
		s = layout.Size{W: 20, H: 20}
	case ops.KindSymbol:
		s = layout.Size{W: 22, H: 22}
	case ops.KindWheel:
		s = layout.Size{W: 320, H: 216} // UIPickerView's
	case ops.KindPicker:
		// A pop-up button: its title (the chosen item, else its label),
		// 8pt characters, and the indicator.
		title := v.String(ops.PropPickerLabel)
		if items, i := v.Strings(ops.PropPickerItems), int(v.Int(ops.PropPickerSelected)); i >= 0 && i < len(items) {
			title = items[i]
		}
		s = layout.Size{W: float32(len([]rune(title)))*8 + 32, H: 34}
	case ops.KindStepper:
		s = layout.Size{W: 94, H: 32} // UIStepper's
	case ops.KindColorPicker:
		s = layout.Size{W: 28, H: 28} // UIColorWell's
	case ops.KindSegmented:
		b := v.Props[ops.PropSegmentedSegments].Bytes
		n := 0
		if len(b) >= 4 {
			n = int(binary.LittleEndian.Uint32(b))
		}
		s = layout.Size{W: float32(70 * n), H: 32}
	case ops.KindDatePicker:
		s = layout.Size{W: 120, H: 34}
		if v.Props[ops.PropDatePickerMode].Int == int64(ops.DateModeDateTime) {
			s.W = 200
		}
	case ops.KindProgress:
		s = layout.Size{W: 100, H: 4}
		if fin(c.AvailW) {
			s.W = c.AvailW
		}
	case ops.KindProgressRing:
		// Square: a style that sizes one side sizes the other.
		s = layout.Size{W: 20, H: 20}
		if c.Width == c.Width {
			s.H = c.Width
		} else if c.Height == c.Height {
			s.W = c.Height
		}
	case ops.KindRichText:
		// As text: the paragraphs' text, a line each at least, as tall
		// as its inline components.
		var h float32
		inl := v.Inlines()
		next := 0
		for _, p := range v.Rich() {
			t := measureText(p.Text(), c)
			s.W = max(s.W, t.W)
			for _, r := range p {
				if r.Style.Inline && next < len(inl) {
					t.H = max(t.H, inl[next].H)
					next++
				}
			}
			h += max(t.H, 20)
		}
		s.H = h
	}
	if c.Width == c.Width {
		s.W = c.Width
	}
	if c.Height == c.Height {
		s.H = c.Height
	}
	return s
}

func fin(f float32) bool { return f == f && f < layout.Inf && f > -layout.Inf }

// measureText lays out 8pt characters on 20pt lines, wrapping anywhere
// (min-content is one character wide).
func measureText(text string, c layout.Constraints) layout.Size {
	n := len([]rune(text))
	if n == 0 {
		return layout.Size{}
	}
	w := c.Width
	if w != w {
		switch {
		case c.AvailW == layout.MinContentSpace:
			w = 8
		case fin(c.AvailW):
			w = min(float32(n*8), max(8, float32(int(c.AvailW/8)*8)))
		default:
			w = float32(n * 8)
		}
	}
	perLine := max(1, int(w/8))
	lines := (n + perLine - 1) / perLine
	return layout.Size{W: w, H: float32(lines * 20)}
}

// call runs a Call with h.Modules (or h.AsyncModules) and posts its
// Result.
func (h *Host) call(root ops.NodeID, b []byte) {
	le := binary.LittleEndian
	id := le.Uint32(b)
	b = b[4:]
	n := le.Uint32(b)
	module := string(b[4 : 4+n])
	b = b[4+n:]
	n = le.Uint32(b)
	method := string(b[4 : 4+n])
	args := append([]byte(nil), b[4+n:]...)
	var once sync.Once
	reply := func(res []byte, err error) { once.Do(func() { h.result(root, id, res, err) }) }
	if f := h.AsyncModules[module]; f != nil {
		f(method, args, reply)
		return
	}
	if f := h.Modules[module]; f != nil {
		reply(f(method, args))
		return
	}
	reply(nil, fmt.Errorf("no module %q", module))
}

// result posts a Call's Result.
func (h *Host) result(root ops.NodeID, id uint32, res []byte, err error) {
	out := binary.LittleEndian.AppendUint32(nil, id)
	if err != nil {
		out = append(append(out, 0), err.Error()...)
	} else {
		out = append(append(out, 1), res...)
	}
	h.Post(func() { h.dispatch(ops.Event{ID: root, Kind: ops.EventResult, Bytes: out}) })
}

// Root returns the view set as root.
func (h *Host) Root() *View {
	for _, v := range h.Views {
		if v.Root {
			return v
		}
	}
	return nil
}

// Find returns the views of kind k whose text, title or value is s, in
// ID order.
func (h *Host) Find(k ops.Kind, s string) []*View {
	var out []*View
	for _, v := range h.Views {
		if v.Kind != k {
			continue
		}
		if v.Label() == s || (v.Kind == ops.KindTextField && v.String(ops.PropTextFieldValue) == s) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// One returns the only view of kind k with text s, failing if there
// isn't exactly one.
func (h *Host) One(k ops.Kind, s string) *View {
	vs := h.Find(k, s)
	if len(vs) != 1 {
		panic(fmt.Sprintf("fake: %d %s views with %q", len(vs), k, s))
	}
	return vs[0]
}

// OfKind returns the views of kind k in ID order.
func (h *Host) OfKind(k ops.Kind) []*View {
	var out []*View
	for _, v := range h.Views {
		if v.Kind == k {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (h *Host) dispatch(e ops.Event) {
	if h.core == nil {
		panic("fake: not connected; mount a root first")
	}
	h.core.Dispatch(e)
}

func (h *Host) listening(v *View, e ops.EventKind) {
	if !v.Listens[e] {
		panic(fmt.Sprintf("fake: view %d (%s) doesn't listen for %s", v.ID, v.Kind, e))
	}
}

// Tap presses v, as a user would.
func (h *Host) Tap(v *View) {
	h.listening(v, ops.EventPress)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventPress})
}

// LongPress long-presses v.
func (h *Host) LongPress(v *View) {
	h.listening(v, ops.EventLongPress)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventLongPress})
}

// Type replaces a text field's text, as typing does: the widget changes
// first, then it reports.
func (h *Host) Type(v *View, s string) {
	v.Props[ops.PropTextFieldValue] = ops.Value{Type: ops.TString, Str: s}
	if v.Listens[ops.EventTextChanged] {
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventTextChanged, Str: s})
	}
}

// Submit presses return in a text field.
func (h *Host) Submit(v *View) {
	h.listening(v, ops.EventSubmit)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventSubmit})
}

// Key presses a key while focused (a view, or nil: nothing, in the key
// window) has the keyboard's focus, as the platforms route it: to the
// nearest view, from focused out, whose Keys prop has the key (name: a
// character, any case, or a named key; mods: 1 primary, 2 shift, 4 alt, 8
// control), which gets Key with its index. It reports whether a view took
// it (false: the focused control or the menus would have). Out is through
// the parents (KeyOut); with nothing focused a key starts at the
// KeyWindow's root (the root, or a Window); pass a sheet's or a popover's
// view for one with the keyboard and nothing focused in it.
func (h *Host) Key(focused *View, name string, mods uint8) bool {
	if focused == nil {
		focused = h.KeyWindow()
	}
	for v := focused; v != nil; v = h.KeyOut(v) {
		b, ok := v.Props[ops.PropKeys]
		if !ok {
			continue
		}
		if i := ops.MatchKey(ops.DecodeKeys(b.Bytes), name, mods); i >= 0 {
			h.listening(v, ops.EventKey)
			h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventKey, Int: int64(i)})
			return true
		}
	}
	return false
}

// KeyOut is where a key goes on from v, not taking it: v's parent; for
// a node the core leaves without one, where it shows: a cell (a List's
// row) its List, a sheet the window it's declared in (its Window prop:
// the root, a Window), a popover the root of what its
// anchor is in (a window's, or a sheet's node). The root and a Window
// are ends (nil): a Window's keys are its own.
func (h *Host) KeyOut(v *View) *View {
	if v.Parent != nil {
		return v.Parent
	}
	if v.list != nil && h.Views[v.list.ID] == v.list {
		return v.list
	}
	switch v.Kind {
	case ops.KindSheet:
		if w := h.PresentedOn(v); w != nil && w != v {
			return w
		}
		return h.Root()
	case ops.KindPopover:
		if a := h.Views[ops.NodeID(v.Int(ops.PropPopoverAnchor))]; a != nil {
			for a.Parent != nil || a.list != nil {
				if a.Parent != nil {
					a = a.Parent
				} else {
					a = a.list
				}
			}
			if a != v {
				return a
			}
		}
		return h.Root()
	}
	return nil
}

// Flip toggles a switch.
func (h *Host) Flip(v *View) {
	on := !v.Bool(ops.PropToggleOn)
	v.Props[ops.PropToggleOn] = ops.Value{Type: ops.TBool, Int: b2i(on)}
	h.listening(v, ops.EventToggled)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventToggled, Bool: on})
}

// PickColor chooses a ColorPicker's color, as the user does.
func (h *Host) PickColor(v *View, c ops.Color) {
	v.Props[ops.PropColorPickerValue] = ops.Value{Type: ops.TColor, Int: int64(c)}
	h.listening(v, ops.EventColorChanged)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventColorChanged, Int: int64(c)})
}

// Slide moves a slider.
func (h *Host) Slide(v *View, value float64) {
	v.Props[ops.PropSliderValue] = ops.Value{Type: ops.TFloat, Float: value}
	h.listening(v, ops.EventValueChanged)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventValueChanged, Float: value})
}

// SetEnvironment changes the environment and reports it for the root.
func (h *Host) SetEnvironment(e host.Environment) {
	h.Env = e
	id := ops.NodeID(0)
	if r := h.Root(); r != nil {
		id = r.ID
	}
	h.dispatch(ops.Event{ID: id, Kind: ops.EventEnvironment, Env: e})
}

// OpenURL opens the app with a URL (its scheme's, a link), as a host
// does: ui.OnOpenURL gets it.
func (h *Host) OpenURL(u string) {
	h.dispatch(ops.Event{ID: h.rootID(), Kind: ops.EventIncomingURL, Str: u})
}

// OpenFiles opens the app with files, together, as a host does (a
// double click in the file manager, a second launch's arguments):
// ui.OnOpenFiles gets them.
func (h *Host) OpenFiles(paths ...string) {
	h.dispatch(ops.Event{ID: h.rootID(), Kind: ops.EventIncomingURL, Str: fileurl.Join(paths)})
}

func (h *Host) rootID() ops.NodeID {
	if r := h.Root(); r != nil {
		return r.ID
	}
	return 0
}

// Lifecycle reports an app lifecycle change.
func (h *Host) Lifecycle(s ops.Lifecycle) {
	h.dispatch(ops.Event{Kind: ops.EventLifecycle, Int: int64(s)})
}

// PresentedOn is the window a Sheet, an Alert or a Snackbar goes on, as
// a desktop host presents it: its Window prop's view (the root, a
// Window); nil if it has none.
func (h *Host) PresentedOn(v *View) *View {
	p := ops.PropSheetWindow
	switch v.Kind {
	case ops.KindAlert:
		p = ops.PropAlertWindow
	case ops.KindSnackbar:
		p = ops.PropSnackbarWindow
	}
	w, ok := v.Props[p]
	if !ok {
		return nil
	}
	return h.Views[ops.NodeID(w.Int)]
}

// NewCell makes a list cell, as a collection view does.
func (h *Host) NewCell() *View {
	h.nextCell++
	id := ops.HostIDBit | h.nextCell
	v := &View{ID: id, Kind: KindCell, Props: map[ops.PropID]ops.Value{}, Listens: map[ops.EventKind]bool{}}
	h.Views[id] = v
	return v
}

// Bind asks the core to bind item index of list into cell.
func (h *Host) Bind(list *View, index int, cell *View) {
	cell.list = list
	h.core.ListBind(list.ID, index, cell.ID)
}

// Unbind asks the core to let go of cell.
func (h *Host) Unbind(list *View, cell *View) {
	cell.list = nil
	h.core.ListUnbind(list.ID, cell.ID)
}

// SelectRow selects (or deselects) a selecting list's row as the user
// does: the cell shows it, then the core hears of it.
func (h *Host) SelectRow(cell *View, selected bool) {
	h.listening(cell, ops.EventRowSelected)
	var v int64
	if selected {
		v = 1
	}
	cell.Props[ops.PropSelected] = ops.Value{Type: ops.TBool, Int: v}
	h.dispatch(ops.Event{ID: cell.ID, Kind: ops.EventRowSelected, Bool: selected})
}

// ListCount asks the core how many items list has.
func (h *Host) ListCount(list *View) int { return h.core.ListCount(list.ID) }

// Label returns the view's own text: a Text's text, a Button's title.
func (v *View) Label() string {
	switch v.Kind {
	case ops.KindText:
		return v.String(ops.PropTextText)
	case ops.KindButton:
		return v.String(ops.PropButtonTitle)
	}
	return ""
}

// Text returns the texts and titles under v, depth first, joined by "|".
func (v *View) Text() string {
	var parts []string
	var walk func(v *View)
	walk = func(v *View) {
		if s := v.Label(); s != "" {
			parts = append(parts, s)
		}
		for _, c := range v.Children {
			walk(c)
		}
	}
	walk(v)
	return strings.Join(parts, "|")
}

// Tree prints the view tree under v: kind, frame and text.
func (v *View) Tree() string {
	var b strings.Builder
	var walk func(v *View, depth int)
	walk = func(v *View, depth int) {
		fmt.Fprintf(&b, "%s%s %g,%g %gx%g", strings.Repeat("  ", depth), kindName(v.Kind), v.Frame[0], v.Frame[1], v.Frame[2], v.Frame[3])
		if s := v.Label(); s != "" {
			fmt.Fprintf(&b, " %q", s)
		}
		b.WriteByte('\n')
		for _, c := range v.Children {
			walk(c, depth+1)
		}
	}
	walk(v, 0)
	return b.String()
}

func kindName(k ops.Kind) string {
	if k == KindCell {
		return "Cell"
	}
	return k.String()
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Back pops a Stack's top screen as the user's back gesture does: the
// host removes it, then reports Popped. A screen with InterceptBack is
// sent Back instead, and stays.
func (h *Host) Back(stack *View) {
	if len(stack.Children) < 2 {
		panic("fake: Back: nothing to pop")
	}
	top := stack.Children[len(stack.Children)-1]
	if top.Bool(ops.PropInterceptBack) {
		h.listening(top, ops.EventBack)
		h.dispatch(ops.Event{ID: top.ID, Kind: ops.EventBack})
		return
	}
	stack.removeChild(top)
	h.listening(stack, ops.EventPopped)
	h.dispatch(ops.Event{ID: stack.ID, Kind: ops.EventPopped, Int: int64(top.ID)})
}

// BackFromRoot is Android's back with no screen left to pop: the root
// is sent Back if it takes it (InterceptBack), and true is returned;
// false: the system would leave the app.
func (h *Host) BackFromRoot() bool {
	r := h.Root()
	if r == nil || !r.Bool(ops.PropInterceptBack) {
		return false
	}
	h.listening(r, ops.EventBack)
	h.dispatch(ops.Event{ID: r.ID, Kind: ops.EventBack})
	return true
}

// Select selects a tab, a segment, a wheel's or a picker's item, as the
// user does.
func (h *Host) Select(tabs *View, index int) {
	p := ops.PropTabsSelected
	switch tabs.Kind {
	case ops.KindSegmented:
		p = ops.PropSegmentedSelected
	case ops.KindSplit:
		p = ops.PropSplitSelected
	case ops.KindPageControl:
		p = ops.PropPageControlPage
	case ops.KindWheel:
		p = ops.PropWheelSelected
	case ops.KindPicker:
		p = ops.PropPickerSelected
	}
	tabs.Props[p] = ops.Value{Type: ops.TInt, Int: int64(index)}
	h.listening(tabs, ops.EventSelected)
	h.dispatch(ops.Event{ID: tabs.ID, Kind: ops.EventSelected, Int: int64(index)})
}

// Dismiss dismisses a sheet or alert as the user does (a swipe, a tap
// outside). A sheet with InterceptBack is sent Back instead, and stays.
func (h *Host) Dismiss(v *View) {
	if v.Bool(ops.PropInterceptBack) {
		h.listening(v, ops.EventBack)
		h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventBack})
		return
	}
	h.listening(v, ops.EventDismissed)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventDismissed})
}

// Choose picks the item numbered index of a menu, an alert's buttons or
// a screen's bar buttons: items are numbered depth first, a submenu or
// section before its own items (ActionNamed finds one).
func (h *Host) Choose(v *View, index int) {
	h.listening(v, ops.EventAction)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventAction, Int: int64(index)})
}

// ToolbarItems decodes a Screen's Toolbar: its buttons, their flags and
// their menus' items, numbered as PressToolbar takes them.
func (v *View) ToolbarItems() []ops.ActionItem { return v.ActionItems(ops.PropScreenToolbar) }

// PrimaryItem decodes a Screen's Primary action (with its menu's items),
// or returns nil if it has none.
func (v *View) PrimaryItem() *ops.ActionItem {
	items := v.ActionItems(ops.PropScreenPrimary)
	if len(items) == 0 {
		return nil
	}
	return &items[0]
}

// PressToolbar presses the item of a screen's Toolbar with this title, at
// any depth (a button, or an item of a button's menu), as a host reports
// it; false if there's none.
func (h *Host) PressToolbar(screen *View, title string) bool {
	i := screen.ActionNamed(ops.PropScreenToolbar, title)
	if i < 0 {
		return false
	}
	h.chooseAt(screen, ops.ScreenToolbarActions+int64(i))
	return true
}

// PressPrimary presses a screen's Primary action, as a host reports it;
// false if it has none. PressPrimaryItem picks an item of its menu.
func (h *Host) PressPrimary(screen *View) bool {
	if screen.PrimaryItem() == nil {
		return false
	}
	h.chooseAt(screen, ops.ScreenPrimaryAction)
	return true
}

// PressPrimaryItem picks the item with this title of the menu a screen's
// Primary action opens; false if there's none.
func (h *Host) PressPrimaryItem(screen *View, title string) bool {
	i := screen.ActionNamed(ops.PropScreenPrimary, title)
	if i <= 0 {
		return false
	}
	h.chooseAt(screen, ops.ScreenPrimaryAction+int64(i))
	return true
}

func (h *Host) chooseAt(v *View, i int64) {
	h.listening(v, ops.EventAction)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventAction, Int: i})
}

// AlertFields decodes an Alert's Fields prop: its text fields, each
// with its text as it shows (TypeInAlert changes it).
func (v *View) AlertFields() []ops.AlertField {
	return ops.DecodeAlertFields(v.Props[ops.PropAlertFields].Bytes)
}

// TypeInAlert replaces the text of an alert's field (its index), as
// typing does: the field changes first, then it reports FieldChanged.
func (h *Host) TypeInAlert(v *View, field int, s string) {
	fs := v.AlertFields()
	if field < 0 || field >= len(fs) {
		panic(fmt.Sprintf("fake: alert %d has no field %d", v.ID, field))
	}
	fs[field].Text = s
	v.Props[ops.PropAlertFields] = ops.Value{Type: ops.TBytes, Bytes: ops.AppendAlertFields(nil, fs)}
	h.listening(v, ops.EventFieldChanged)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventFieldChanged, Bytes: ops.AppendFieldChanged(nil, field, s)})
}

// ReturnInAlert presses return in an alert's last field, as the hosts
// handle it: the default button (ops.DefaultAlertAction) is pressed
// unless it's disabled. It reports whether it was.
func (h *Host) ReturnInAlert(v *View) bool {
	if len(v.AlertFields()) == 0 {
		panic(fmt.Sprintf("fake: alert %d has no fields", v.ID))
	}
	items := v.ActionItems(ops.PropAlertButtons)
	i := ops.DefaultAlertAction(items)
	if i < 0 || items[i].Is(ops.MenuDisabled) {
		return false
	}
	h.Choose(v, i)
	return true
}

// Snackbar returns the snackbar showing (presented and not destroyed),
// or nil. Its Message, Action and Duration props are what it shows.
func (h *Host) Snackbar() *View {
	var top *View
	for _, v := range h.Views {
		if v.Kind == ops.KindSnackbar && v.presented && (top == nil || v.ID > top.ID) {
			top = v
		}
	}
	return top
}

// PressSnackbarAction presses a snackbar's action button, as the user
// does: it reports Action (0), and the core dismisses it.
func (h *Host) PressSnackbarAction(v *View) {
	if v.String(ops.PropSnackbarAction) == "" {
		panic(fmt.Sprintf("fake: snackbar %d has no action", v.ID))
	}
	h.Choose(v, 0)
}

// TimeOut reports a snackbar's time running out, as a host's timer
// does: Dismissed. (Dismiss is the user's swipe, which reports the
// same.)
func (h *Host) TimeOut(v *View) {
	if v.Kind != ops.KindSnackbar || !v.presented {
		panic(fmt.Sprintf("fake: TimeOut on %s %d, not a snackbar showing", v.Kind, v.ID))
	}
	h.Dismiss(v)
}

// Scroll scrolls a Scroll or List to the content offset x, y.
func (h *Host) Scroll(v *View, x, y float32) {
	h.listening(v, ops.EventScrolled)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventScrolled, X: float64(x), Y: float64(y)})
}

// PullToRefresh pulls a Scroll or List.
func (h *Host) PullToRefresh(v *View) {
	h.listening(v, ops.EventRefresh)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventRefresh})
}

// ReportSheet reports a presented sheet's Environment as a phone's host
// sizes it: a FullScreen sheet's is the screen's own, its whole size and
// safe area (the status bar, the home indicator) and keyboard; another's
// is the screen's width and the height under the status bar, less the
// gap iOS leaves above a sheet at its large detent, with no safe area at
// its top.
func (h *Host) ReportSheet(v *View) {
	if v.Kind != ops.KindSheet || !v.presented {
		panic(fmt.Sprintf("fake: ReportSheet on %d (%s), not a presented sheet", v.ID, v.Kind))
	}
	e := h.Env
	if !v.Bool(ops.PropSheetFullScreen) {
		e.Height -= e.SafeTop + 10
		e.SafeTop = 0
	}
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventEnvironment, Env: e})
}

// Presented returns the sheets and alerts that got Present and still
// exist, in ID order.
func (h *Host) Presented() []*View {
	var out []*View
	for _, v := range h.Views {
		if v.presented {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Commands returns the recorded commands of kind c.
func (h *Host) Commands(c ops.CommandID) []ops.Op {
	var out []ops.Op
	for _, op := range h.Ops {
		if op.Code == ops.OpCommand && op.Cmd == c {
			out = append(out, op)
		}
	}
	return out
}

// Actions decodes the titles of a Menu, Screen Actions, Alert Buttons
// or swipe actions prop's top level items.
func (v *View) Actions(p ops.PropID) []string {
	var out []string
	for _, it := range v.ActionItems(p) {
		out = append(out, it.Title)
	}
	return out
}

// ActionIcons decodes the icons of a Menu, Screen Actions or Alert
// Buttons prop's top level items.
func (v *View) ActionIcons(p ops.PropID) []string {
	var out []string
	for _, it := range v.ActionItems(p) {
		out = append(out, it.Icon)
	}
	return out
}

// ActionItems decodes a Menu, Screen Actions, Alert Buttons or swipe
// actions prop: its items, their flags and their submenus' and sections'
// items, numbered as Choose takes them.
func (v *View) ActionItems(p ops.PropID) []ops.ActionItem {
	return ops.DecodeActions(v.Props[p].Bytes)
}

// Badges returns a Tabs' tabs' badges, in order ("": none), as its tab
// bar shows them.
func (v *View) Badges() []string {
	out := make([]string, len(v.Children))
	for i, c := range v.Children {
		out[i] = c.String(ops.PropScreenBadge)
	}
	return out
}

// Sidebar decodes a SplitView's Items: its sections and their
// destinations' titles, icons and badges.
func (v *View) Sidebar() []ops.SidebarSection {
	return ops.DecodeSidebar(v.Props[ops.PropSplitItems].Bytes)
}

// ActionNamed returns the number of the item of a Menu, Screen Actions or
// Alert Buttons prop with this title, at any depth (for Choose), or -1.
func (v *View) ActionNamed(p ops.PropID, title string) int {
	for _, it := range ops.FlattenActions(v.ActionItems(p)) {
		if it.Title == title {
			return it.Index
		}
	}
	return -1
}

// Dispatch sends an event to the core, as a host does.
func (h *Host) Dispatch(e ops.Event) { h.dispatch(e) }

// RichStyle is a style of a RichText's content.
type RichStyle struct {
	TextStyle         ops.TextStyle
	Size              float32
	Weight            ops.FontWeight
	Italic, Mono      bool
	Underline, Strike bool
	Color, Background ops.Color
	Link              string
	Inline            bool // the run is an inline component
}

// RichRun is a run of a RichText's paragraph.
type RichRun struct {
	Style RichStyle
	Text  string
}

// RichParagraph is a paragraph of a RichText.
type RichParagraph []RichRun

// Text is the paragraph's text.
func (p RichParagraph) Text() string {
	var b strings.Builder
	for _, r := range p {
		b.WriteString(r.Text)
	}
	return b.String()
}

// RichInline is an inline component of a RichText, as its host was told.
type RichInline struct {
	ID   ops.NodeID
	W, H float32
}

// Inlines decodes a RichText's inline components.
func (v *View) Inlines() []RichInline {
	b := v.Props[ops.PropRichTextInlines].Bytes
	if len(b) < 4 {
		return nil
	}
	le := binary.LittleEndian
	out := make([]RichInline, le.Uint32(b))
	for i := range out {
		o := 4 + i*12
		out[i] = RichInline{ops.NodeID(le.Uint32(b[o:])), math.Float32frombits(le.Uint32(b[o+4:])), math.Float32frombits(le.Uint32(b[o+8:]))}
	}
	return out
}

// Rich decodes a RichText's content.
func (v *View) Rich() []RichParagraph {
	b := v.Props[ops.PropRichTextContent].Bytes
	le := binary.LittleEndian
	take := func(n int) []byte {
		if len(b) < n {
			panic("fake: short RichText content")
		}
		x := b[:n]
		b = b[n:]
		return x
	}
	u32 := func() uint32 { return le.Uint32(take(4)) }
	str := func() string { return string(take(int(u32()))) }
	if len(b) == 0 {
		return nil
	}
	styles := make([]RichStyle, u32())
	for i := range styles {
		st := &styles[i]
		st.TextStyle = ops.TextStyle(take(1)[0])
		st.Size = math.Float32frombits(u32())
		st.Weight = ops.FontWeight(take(1)[0])
		f := take(1)[0]
		st.Italic, st.Mono, st.Underline, st.Strike, st.Inline = f&1 != 0, f&2 != 0, f&4 != 0, f&8 != 0, f&16 != 0
		st.Color = ops.Color(le.Uint64(take(8)))
		st.Background = ops.Color(le.Uint64(take(8)))
		st.Link = str()
	}
	paras := make([]RichParagraph, u32())
	for i := range paras {
		runs := make(RichParagraph, u32())
		for j := range runs {
			runs[j].Style = styles[u32()]
			runs[j].Text = str()
		}
		paras[i] = runs
	}
	return paras
}

// MenuBarItem is an item of the menus the core last sent (CmdMenuBar).
type MenuBarItem struct {
	Menu, Title, Key string
	Flags, Mods      byte
	Role             ops.MenuRole
	// Depth is how deep in submenus it is (0: in the menu itself).
	Depth int
}

// MenuBar returns the items of the menus last sent, in order, with their
// submenus' items after the item that opens them (an item's index is the
// one ChooseMenuItem takes).
func (h *Host) MenuBar() []MenuBarItem {
	cmds := h.Commands(ops.CmdMenuBar)
	if len(cmds) == 0 {
		return nil
	}
	var out []MenuBarItem
	for _, m := range ops.DecodeMenuBar(cmds[len(cmds)-1].Value.Bytes) {
		var walk func(items []ops.MenuItem, depth int)
		walk = func(items []ops.MenuItem, depth int) {
			for _, it := range items {
				out = append(out, MenuBarItem{Menu: m.Title, Title: it.Title, Key: it.Key, Flags: byte(it.Flags), Mods: it.Mods, Role: it.Role, Depth: depth})
				walk(it.Items, depth+1)
			}
		}
		walk(m.Items, 0)
	}
	return out
}

// ChooseMenuItem chooses the menu bar's item i, as a click or its
// shortcut does: a Close item (MenuRoleClose) closes the window in use
// (KeyWindow), as a host does.
func (h *Host) ChooseMenuItem(i int) {
	if items := h.MenuBar(); i >= 0 && i < len(items) && items[i].Role == ops.MenuRoleClose {
		h.CloseWindow(h.KeyWindow())
		return
	}
	root := h.Root()
	h.listening(root, ops.EventAction)
	h.dispatch(ops.Event{ID: root.ID, Kind: ops.EventAction, Int: int64(i)})
}
