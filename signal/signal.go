package signal

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
)

// Signal holds a value. Reading it inside a computation subscribes the
// computation; writing it re-runs exactly the computations that read it.
type Signal[T any] struct {
	n     node
	value T
	equal func(a, b T) bool
}

func eq[T comparable](a, b T) bool { return a == b }

// New makes a signal that skips writes equal to the current value.
func New[T comparable](v T) *Signal[T] {
	return &Signal[T]{value: v, equal: eq[T]}
}

// NewFunc makes a signal with a custom equality. With a nil equal, every
// write notifies.
func NewFunc[T any](v T, equal func(a, b T) bool) *Signal[T] {
	return &Signal[T]{value: v, equal: equal}
}

// Get returns the value and subscribes the current computation.
func (s *Signal[T]) Get() T {
	if activeSub != nil {
		track(&s.n, activeSub)
	}
	return s.value
}

// Peek returns the value without subscribing.
func (s *Signal[T]) Peek() T { return s.value }

// Set writes v. Outside a batch and a flush, the effects it dirties run
// before Set returns.
func (s *Signal[T]) Set(v T) {
	checkThread()
	if inMemo > 0 {
		panic("signal: Set inside a memo")
	}
	if s.equal != nil && s.equal(s.value, v) {
		return
	}
	s.value = v
	if s.n.subs != nil {
		propagate(s.n.subs, fDirty)
		if batchDepth == 0 && !flushing {
			flush()
		}
	}
}

// Update sets the value to f(current).
func (s *Signal[T]) Update(f func(T) T) { s.Set(f(s.value)) }

// Memo is a cached derived value. It is lazy: it computes on the first
// Get, and again only when a source changed.
type Memo[T any] struct {
	n     node
	owner Owner
	fn    func() T
	value T
	equal func(a, b T) bool
	ready bool
}

// NewMemo makes a memo owned by the current owner.
func NewMemo[T comparable](f func() T) *Memo[T] {
	return newMemo(f, eq[T])
}

// NewMemoFunc makes a memo with a custom equality; nil means every
// recomputation counts as a change.
func NewMemoFunc[T any](f func() T, equal func(a, b T) bool) *Memo[T] {
	return newMemo(f, equal)
}

func newMemo[T any](f func() T, equal func(a, b T) bool) *Memo[T] {
	m := &Memo[T]{fn: f, equal: equal}
	m.n.flags = fMemo | fDirty
	m.n.comp = m
	m.owner.comp = &m.n
	m.owner.attach(currentOwner)
	return m
}

// Get returns the memo's value, recomputing it if needed, and subscribes
// the current computation.
func (m *Memo[T]) Get() T {
	n := &m.n
	if n.flags&(fDirty|fPending) != 0 && n.flags&fDisposed == 0 {
		if n.flags&fDirty != 0 || checkDirty(n) {
			if m.update() && n.subs != nil {
				shallowPropagate(n.subs)
			}
		} else {
			n.flags &^= fPending
		}
	}
	if activeSub != nil {
		track(n, activeSub)
	}
	return m.value
}

func (m *Memo[T]) update() bool {
	m.owner.clean()
	n := &m.n
	prevSub, prevOwner := activeSub, currentOwner
	activeSub, currentOwner = n, &m.owner
	inMemo++
	startTracking(n)
	defer func() {
		inMemo--
		activeSub, currentOwner = prevSub, prevOwner
	}()
	v := m.fn()
	endTracking(n)
	changed := !m.ready || m.equal == nil || !m.equal(m.value, v)
	m.value, m.ready = v, true
	return changed
}

// unwatched: with no subscribers left, a memo lets go of its sources so
// it stops being notified, and recomputes on its next Get.
func (m *Memo[T]) unwatched() {
	if m.n.flags&fDisposed != 0 {
		return
	}
	unlinkAllDeps(&m.n)
	m.n.flags |= fDirty
}

type effect struct {
	n     node
	owner Owner
	fn    func()
}

func newEffect(fn func(), extra flags) *effect {
	e := &effect{fn: fn}
	e.n.flags = fEffect | extra
	e.n.comp = e
	e.n.seq = nextSeq()
	e.owner.comp = &e.n
	e.owner.attach(currentOwner)
	return e
}

func (e *effect) update() bool {
	e.owner.clean()
	n := &e.n
	prevSub, prevOwner := activeSub, currentOwner
	activeSub, currentOwner = n, &e.owner
	startTracking(n)
	defer func() { activeSub, currentOwner = prevSub, prevOwner }()
	e.fn()
	endTracking(n)
	return true
}

func (e *effect) unwatched() {}

// Effect runs f now if no batch or flush is in progress, else at the end
// of it, and again whenever what f read changes. User effects run after
// render effects within a flush.
func Effect(f func()) {
	e := newEffect(f, fDirty)
	enqueue(&e.n)
	if batchDepth == 0 && !flushing {
		flush()
	}
}

// RenderEffect runs f immediately and again whenever what f read
// changes. ui uses it for props: within a flush, render effects run
// before user effects.
func RenderEffect(f func()) {
	e := newEffect(f, fRender)
	e.update()
}

// Untrack runs f without subscribing the current computation to what f
// reads.
func Untrack[T any](f func() T) T {
	prev := activeSub
	activeSub = nil
	defer func() { activeSub = prev }()
	return f()
}

// Batch runs f and runs the effects its writes dirtied once, after f
// returns.
func Batch(f func()) {
	batchDepth++
	func() {
		defer func() { batchDepth-- }()
		f()
	}()
	if batchDepth == 0 && !flushing {
		flush()
	}
}

// MaxPasses is how many times effects may re-dirty each other within one
// flush before it panics as an infinite loop.
const MaxPasses = 100

var queue, running []*node

func enqueue(n *node) {
	if n.flags&fQueued != 0 {
		return
	}
	n.flags |= fQueued
	queue = append(queue, n)
}

func cmpEffects(a, b *node) int {
	ra, rb := a.flags&fRender != 0, b.flags&fRender != 0
	if ra != rb {
		if ra {
			return -1
		}
		return 1
	}
	switch {
	case a.seq < b.seq:
		return -1
	case a.seq > b.seq:
		return 1
	}
	return 0
}

func flush() {
	flushing = true
	i := 0
	defer func() {
		flushing = false
		// After a panic, keep what didn't run so a later flush runs it.
		for ; i < len(running); i++ {
			if n := running[i]; n != nil {
				queue = append(queue, n)
				running[i] = nil
			}
		}
		running = running[:0]
	}()
	for pass := 0; len(queue) > 0; pass++ {
		if pass == MaxPasses {
			msg := describe(queue[0])
			for _, n := range queue {
				n.flags &^= fQueued
			}
			clear(queue)
			queue = queue[:0]
			panic(fmt.Sprintf("signal: effects still dirty after %d passes (an infinite loop?), e.g. %s", MaxPasses, msg))
		}
		running, queue = queue, running[:0]
		slices.SortFunc(running, cmpEffects)
		for i = 0; i < len(running); i++ {
			n := running[i]
			running[i] = nil
			n.flags &^= fQueued
			if n.flags&fDisposed != 0 {
				continue
			}
			if n.flags&fDirty != 0 || (n.flags&fPending != 0 && checkDirty(n)) {
				n.comp.update()
			} else {
				n.flags &^= fPending
			}
		}
		running = running[:0]
	}
}

func describe(n *node) string {
	e, ok := n.comp.(*effect)
	if !ok {
		return "a computation"
	}
	pc := reflect.ValueOf(e.fn).Pointer()
	f := runtime.FuncForPC(pc)
	if f == nil {
		return "an effect"
	}
	file, line := f.FileLine(pc)
	return fmt.Sprintf("effect %s (%s:%d)", f.Name(), file, line)
}
