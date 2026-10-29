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

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
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

	presented bool
}

// String returns a prop as text ("" when unset).
func (v *View) String(p ops.PropID) string { return v.Props[p].Str }

// Bool returns a bool prop.
func (v *View) Bool(p ops.PropID) bool { return v.Props[p].Int != 0 }

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

	core     host.Core
	posted   []func()
	postMu   sync.Mutex
	nextCell ops.NodeID
	opsAlloc []byte
}

// New makes a fake host with a 390×844 portrait phone environment.
func New() *Host {
	return &Host{
		Views: map[ops.NodeID]*View{},
		Env: host.Environment{
			Width: 390, Height: 844, SafeTop: 47, SafeBottom: 34,
			Scale: 3, FontScale: 1, Locale: "en_US",
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
	case ops.OpInsert:
		p, c := h.view(op.ID, "Insert"), h.view(op.Child, "Insert")
		if c.Parent != nil {
			panic(fmt.Sprintf("fake: Insert: %d already has parent %d", c.ID, c.Parent.ID))
		}
		if int(op.Index) > len(p.Children) {
			panic(fmt.Sprintf("fake: Insert: index %d of %d into %d", op.Index, len(p.Children), p.ID))
		}
		if (p.Kind == ops.KindScroll || p.Kind == ops.KindSheet) && len(p.Children) > 0 {
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
		v.Props[op.Prop] = op.Value
	case ops.OpSetFrame:
		h.view(op.ID, "SetFrame").Frame = op.Frame
	case ops.OpListen:
		h.view(op.ID, "Listen").Listens[op.Event] = true
	case ops.OpUnlisten:
		delete(h.view(op.ID, "Unlisten").Listens, op.Event)
	case ops.OpCommand:
		v := h.view(op.ID, "Command")
		if op.Cmd == ops.CmdPresent {
			if v.Kind != ops.KindSheet && v.Kind != ops.KindAlert {
				panic(fmt.Sprintf("fake: Present on a %s", v.Kind))
			}
			v.presented = true
		}
	case ops.OpSetRoot:
		for _, v := range h.Views {
			v.Root = false
		}
		h.view(op.ID, "SetRoot").Root = true
	default:
		panic(fmt.Sprintf("fake: unknown op %d", op.Code))
	}
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
	case ops.KindSlider:
		s = layout.Size{W: 200, H: 30}
		if fin(c.AvailW) {
			s.W = c.AvailW
		}
	case ops.KindActivity:
		s = layout.Size{W: 20, H: 20}
	case ops.KindSymbol:
		s = layout.Size{W: 22, H: 22}
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

// Flip toggles a switch.
func (h *Host) Flip(v *View) {
	on := !v.Bool(ops.PropToggleOn)
	v.Props[ops.PropToggleOn] = ops.Value{Type: ops.TBool, Int: b2i(on)}
	h.listening(v, ops.EventToggled)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventToggled, Bool: on})
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

// Lifecycle reports an app lifecycle change.
func (h *Host) Lifecycle(s ops.Lifecycle) {
	h.dispatch(ops.Event{Kind: ops.EventLifecycle, Int: int64(s)})
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
	h.core.ListBind(list.ID, index, cell.ID)
}

// Unbind asks the core to let go of cell.
func (h *Host) Unbind(list *View, cell *View) {
	h.core.ListUnbind(list.ID, cell.ID)
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
// host removes it, then reports Popped.
func (h *Host) Back(stack *View) {
	if len(stack.Children) < 2 {
		panic("fake: Back: nothing to pop")
	}
	top := stack.Children[len(stack.Children)-1]
	stack.removeChild(top)
	h.listening(stack, ops.EventPopped)
	h.dispatch(ops.Event{ID: stack.ID, Kind: ops.EventPopped, Int: int64(top.ID)})
}

// Select selects a tab or a segment, as the user does.
func (h *Host) Select(tabs *View, index int) {
	p := ops.PropTabsSelected
	if tabs.Kind == ops.KindSegmented {
		p = ops.PropSegmentedSelected
	}
	tabs.Props[p] = ops.Value{Type: ops.TInt, Int: int64(index)}
	h.listening(tabs, ops.EventSelected)
	h.dispatch(ops.Event{ID: tabs.ID, Kind: ops.EventSelected, Int: int64(index)})
}

// Dismiss dismisses a sheet or alert as the user does (a swipe, a tap
// outside).
func (h *Host) Dismiss(v *View) {
	h.listening(v, ops.EventDismissed)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventDismissed})
}

// Choose picks item index of a menu, an alert's buttons or a screen's
// bar buttons.
func (h *Host) Choose(v *View, index int) {
	h.listening(v, ops.EventAction)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventAction, Int: int64(index)})
}

// PullToRefresh pulls a Scroll or List.
func (h *Host) PullToRefresh(v *View) {
	h.listening(v, ops.EventRefresh)
	h.dispatch(ops.Event{ID: v.ID, Kind: ops.EventRefresh})
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

// Actions decodes a Menu, Screen Actions or Alert Buttons prop.
func (v *View) Actions(p ops.PropID) []string {
	b := v.Props[p].Bytes
	if len(b) < 4 {
		return nil
	}
	n := int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	b = b[4:]
	var out []string
	for range n {
		l := int(uint32(b[1]) | uint32(b[2])<<8 | uint32(b[3])<<16 | uint32(b[4])<<24)
		out = append(out, string(b[5:5+l]))
		b = b[5+l:]
		l = int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
		b = b[4+l:] // the icon
	}
	return out
}

// ActionIcons decodes the icons of a Menu, Screen Actions or Alert
// Buttons prop.
func (v *View) ActionIcons(p ops.PropID) []string {
	b := v.Props[p].Bytes
	if len(b) < 4 {
		return nil
	}
	u32 := func(b []byte) int { return int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24) }
	n := u32(b)
	b = b[4:]
	var out []string
	for range n {
		l := u32(b[1:])
		b = b[5+l:]
		l = u32(b)
		out = append(out, string(b[4:4+l]))
		b = b[4+l:]
	}
	return out
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
