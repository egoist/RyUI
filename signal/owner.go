package signal

// Owner owns effects, memos, child owners and cleanups. Disposing an
// owner disposes everything it owns, runs its cleanups (last registered
// first) and unsubscribes its computations. The GC frees memory but never
// stops a subscribed effect: disposal is explicit.
type Owner struct {
	parent      *Owner // for context lookup; also disposes us if attached
	first, last *Owner // attached children
	prev, next  *Owner // siblings in parent's list
	attached    bool
	disposed    bool
	cleanups    []func()
	ctx         *ctxEntry
	comp        *node // the computation this owner belongs to, if any
	// catcher is the nearest owner, this one or above, that Catch made:
	// a panic in an effect it owns goes to its handler. Nil for most, so
	// effects outside a Catch run with no recover.
	catcher *Owner
	onError func(err any)
}

type ctxEntry struct {
	key  any
	val  any
	next *ctxEntry
}

var currentOwner *Owner

func (o *Owner) attach(parent *Owner) {
	o.parent = parent
	if parent == nil {
		return
	}
	o.catcher = parent.catcher
	o.attached = true
	o.prev = parent.last
	if parent.last != nil {
		parent.last.next = o
	} else {
		parent.first = o
	}
	parent.last = o
}

func (o *Owner) detach() {
	if !o.attached {
		return
	}
	p := o.parent
	if o.prev != nil {
		o.prev.next = o.next
	} else {
		p.first = o.next
	}
	if o.next != nil {
		o.next.prev = o.prev
	} else {
		p.last = o.prev
	}
	o.prev, o.next, o.attached = nil, nil, false
}

// clean disposes the children and runs the cleanups, leaving the owner
// usable (an effect cleans itself before each re-run).
func (o *Owner) clean() {
	if o.first == nil && len(o.cleanups) == 0 {
		return
	}
	settleAmbient()
	prevSub := activeSub
	activeSub = nil
	for c := o.last; c != nil; {
		prev := c.prev
		c.Dispose()
		c = prev
	}
	for i := len(o.cleanups) - 1; i >= 0; i-- {
		f := o.cleanups[i]
		o.cleanups[i] = nil
		f()
	}
	o.cleanups = o.cleanups[:0]
	settleAmbient()
	activeSub = prevSub
}

// Dispose disposes o and everything it owns. Disposing twice is a no-op.
func (o *Owner) Dispose() {
	if o.disposed {
		return
	}
	o.disposed = true
	o.clean()
	if o.comp != nil {
		o.comp.flags |= fDisposed
		unlinkAllDeps(o.comp)
	}
	o.detach()
}

// Disposed reports whether o was disposed.
func (o *Owner) Disposed() bool { return o.disposed }

// Root runs f with a new owner that is not disposed with the current one
// (but still sees its contexts). f receives the function that disposes
// it. f runs untracked, and in a batch: effects it creates run once f
// returns.
func Root(f func(dispose func())) {
	o := &Owner{parent: currentOwner}
	if currentOwner != nil {
		o.catcher = currentOwner.catcher
	}
	settleAmbient()
	prevOwner, prevSub := currentOwner, activeSub
	currentOwner, activeSub = o, nil
	defer func() {
		settleAmbient()
		currentOwner, activeSub = prevOwner, prevSub
	}()
	Batch(func() { f(o.Dispose) })
}

// NewOwner makes an owner under the current one, disposed with it.
func NewOwner() *Owner {
	o := &Owner{}
	o.attach(currentOwner)
	return o
}

// CurrentOwner returns the owner new effects would belong to, or nil.
func CurrentOwner() *Owner { return currentOwner }

// RunWithOwner runs f with o as the current owner, untracked.
func RunWithOwner(o *Owner, f func()) {
	settleAmbient()
	prevOwner, prevSub := currentOwner, activeSub
	currentOwner, activeSub = o, nil
	defer func() {
		settleAmbient()
		currentOwner, activeSub = prevOwner, prevSub
	}()
	f()
}

// OnCleanup registers f to run when the current owner is disposed, or
// before the current effect re-runs. Without an owner it does nothing.
func OnCleanup(f func()) {
	if currentOwner != nil {
		currentOwner.cleanups = append(currentOwner.cleanups, f)
	}
}

// Context passes a value down the owner tree.
type Context[T any] struct{ def T }

// NewContext makes a context whose Use returns def when nothing is
// provided.
func NewContext[T any](def T) *Context[T] { return &Context[T]{def: def} }

// Provide runs f with v provided for c, under a new owner that belongs to
// the current one.
func (c *Context[T]) Provide(v T, f func()) {
	o := NewOwner()
	o.ctx = &ctxEntry{key: c, val: v}
	prevOwner := currentOwner
	currentOwner = o
	defer func() { currentOwner = prevOwner }()
	f()
}

// Use returns the nearest provided value, or the default.
func (c *Context[T]) Use() T {
	for o := currentOwner; o != nil; o = o.parent {
		for e := o.ctx; e != nil; e = e.next {
			if e.key == any(c) {
				return e.val.(T)
			}
		}
	}
	return c.def
}

// Catch runs f under a new owner, disposed with the current one, that
// catches panics: f's own, and those of the effects f's owner owns, as
// they run then or later. A panic goes to handler (on the main thread, the
// effect's or f's state restored), not up the stack, and the panicking
// effect keeps what it read so far. A panic in handler goes to the Catch
// around this one, if any. Panics with no Catch around them aren't
// recovered: the program crashes as before, with its stack.
//
// Panics in computations a Catch doesn't own (a memo read from outside,
// event handlers) aren't caught. Effects outside every Catch pay nothing
// for it.
func Catch(f func(), handler func(err any)) {
	o := NewOwner()
	o.catcher = o
	o.onError = handler
	prevOwner, prevSub := currentOwner, activeSub
	currentOwner = o
	defer func() {
		currentOwner, activeSub = prevOwner, prevSub
		if err := recover(); err != nil {
			handleError(o, err)
		}
	}()
	f()
}

// handleError gives err to the catcher of owner o, or panics with it when
// there is none.
func handleError(o *Owner, err any) {
	c := o.catcher
	if c == nil {
		panic(err)
	}
	var up *Owner
	if c.parent != nil && c.parent.catcher != nil {
		up = c.parent
	}
	if up == nil {
		c.onError(err)
		return
	}
	defer func() {
		if err := recover(); err != nil {
			handleError(up, err)
		}
	}()
	c.onError(err)
}
