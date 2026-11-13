package native

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/egoist/asuka/ui"
)

// Typed native modules: a Go package declares an interface with
// //asuka:native, and asuka native generates the rest (its _native.go:
// NewT, a client that encodes each call; and the Swift, Kotlin and
// JavaScript that decode it and call the implementation). What's here is
// what that generated code calls, and what each platform's host gives it
// (SetBackend).
//
// A call goes from the calling goroutine straight to the platform's
// runtime and back, on the same thread: through the iOS host's C
// functions, JNI on Android, the Mac's Swift library through purego, and
// JavaScript on the web. A method that takes a context runs
// asynchronously on the platform's side and answers when it's done, or
// when the context is done (canceled). On Windows and Linux an
// implementation is Go, which the generated NewT returns as it is.

// Module is a package's typed native code: what the hosts find it by.
type Module struct {
	ID     string  // the Go package's import path
	Swift  string  // the Objective-C name of its Swift side's class (iOS, macOS)
	Kotlin string  // the name of its Kotlin side's class (Android)
	Errors []error // its declared errors (NewError), by index
}

// Backend is a platform's side of typed native modules, which its host
// sets. Calls may come from any goroutine. An answer is a status (u8: 0
// ok, then the result; 1 a declared error, u32 its index; 2 an error,
// its domain, i64 code and message; 3 unsupported, its message; 4
// canceled), written into out when it fits, else a slice of the
// backend's (Go memory either way).
type Backend interface {
	// New makes an object of the module's interface (its index): its
	// answer is a u64 handle.
	New(m *Module, iface uint32, out []byte) []byte
	// Call calls the object's method (its index, from 1).
	Call(handle uint64, method uint32, in, out []byte) []byte
	// Start starts a method that takes a context; Done gets its answer.
	// The backend copies in before it returns.
	Start(handle uint64, method uint32, in []byte, call uint64)
	// Cancel cancels a started call, which still answers (canceled, or
	// what it had done).
	Cancel(call uint64)
	// Release lets the object go.
	Release(handle uint64)
}

var backend atomic.Pointer[Backend]

// SetBackend sets the platform's side of typed native modules (hosts).
func SetBackend(b Backend) { backend.Store(&b) }

func current() Backend {
	if b := backend.Load(); b != nil {
		return *b
	}
	return nil
}

// ErrClosed is a call to an object after its Close.
var ErrClosed = errors.New("native: the object is closed")

// Error is an error a platform's side threw that the package didn't
// declare: its domain (an NSError's, an exception's class), code and
// message.
type Error struct {
	Domain  string
	Code    int64
	Message string
}

func (e *Error) Error() string {
	switch {
	case e.Domain == "":
		return e.Message
	case e.Message == "":
		return fmt.Sprintf("%s %d", e.Domain, e.Code)
	}
	return fmt.Sprintf("%s (%s %d)", e.Message, e.Domain, e.Code)
}

// declared is an error a package declares with NewError.
type declared struct{ message string }

func (e *declared) Error() string { return e.message }

// NewError declares an error the package's native code throws: each
// platform's side has it (a case of the Swift error enum, an exception
// in Kotlin, in JavaScript), and Go gets this value, for errors.Is.
//
//	var ErrLowPower = native.NewError("battery: low power mode")
func NewError(message string) error { return &declared{message} }

// answers are the buffers calls are answered in.
var answers = sync.Pool{New: func() any { b := make([]byte, 0, 512); return &b }}

func answerBuffer() *[]byte {
	b := answers.Get().(*[]byte)
	*b = (*b)[:cap(*b)]
	return b
}

// status reads an answer's status: nil for ok, with r at its result.
func (m *Module) status(r *Reader) error {
	switch s := r.Uint8(); {
	case r.err != nil:
		return r.err
	case s == 0:
		return nil
	case s == 1:
		i := r.Uint32()
		if int(i) < len(m.Errors) {
			return m.Errors[i]
		}
		return &Error{Message: fmt.Sprintf("native: %s's error %d, which it doesn't declare", m.ID, i)}
	case s == 2:
		e := &Error{Domain: r.String(), Code: r.Int64()}
		e.Message = r.String()
		return e
	case s == 3:
		return fmt.Errorf("%w: %s", errors.ErrUnsupported, r.String())
	case s == 4:
		return context.Canceled
	}
	return errShort
}

// New makes an object of the module's interface iface (named name), on
// the platform's side.
func (m *Module) New(iface uint32, name string) (*Object, error) {
	b := current()
	if b == nil {
		return nil, fmt.Errorf("%w: %s has no %s on %s", errors.ErrUnsupported, m.ID, name, runtime.GOOS)
	}
	buf := answerBuffer()
	r := answerReader(b.New(m, iface, *buf), buf)
	if err := m.status(r); err != nil {
		r.Done()
		return nil, err
	}
	h := r.Handle()
	if err := r.Done(); err != nil {
		return nil, err
	}
	return m.object(h), nil
}

// Object reads a native object of the module from an answer (nil for
// none).
func (m *Module) Object(r *Reader) *Object {
	h := r.Handle()
	if h == 0 {
		return nil
	}
	return m.object(h)
}

func (m *Module) object(h uint64) *Object {
	o := &Object{mod: m, handle: h}
	o.cleanup = runtime.AddCleanup(o, release, h)
	return o
}

func release(h uint64) {
	if b := current(); b != nil {
		b.Release(h)
	}
}

// Object is an object on the platform's side, which generated code
// calls. It's let go when Go no longer has it, or at Close.
type Object struct {
	mod     *Module
	handle  uint64
	closed  atomic.Bool
	cleanup runtime.Cleanup
}

// Release lets the object go now; later calls return ErrClosed.
func (o *Object) Release() {
	if o == nil || o.closed.Swap(true) {
		return
	}
	o.cleanup.Stop()
	release(o.handle)
}

// Call calls method with the arguments w holds (taking w), and returns
// the answer's reader, at the result, which the caller ends with Done.
func (o *Object) Call(method uint32, w *Writer) (*Reader, error) {
	defer w.free()
	if o.closed.Load() {
		return nil, ErrClosed
	}
	b := current()
	if b == nil {
		return nil, fmt.Errorf("%w: no native code on %s", errors.ErrUnsupported, runtime.GOOS)
	}
	buf := answerBuffer()
	r := answerReader(b.Call(o.handle, method, w.b, *buf), buf)
	runtime.KeepAlive(o)
	if err := o.mod.status(r); err != nil {
		r.Done()
		return nil, err
	}
	return r, nil
}

// pending are the started calls' answers, by call.
var (
	pendingMu sync.Mutex
	pending   = map[uint64]chan []byte{}
	nextCall  atomic.Uint64
)

// CallContext calls a method that takes a context: it runs on the
// platform's side until it answers, or ctx is done, which cancels it.
func (o *Object) CallContext(ctx context.Context, method uint32, w *Writer) (*Reader, error) {
	if err := ctx.Err(); err != nil {
		w.free()
		return nil, err
	}
	if o.closed.Load() {
		w.free()
		return nil, ErrClosed
	}
	b := current()
	if b == nil {
		w.free()
		return nil, fmt.Errorf("%w: no native code on %s", errors.ErrUnsupported, runtime.GOOS)
	}
	id := nextCall.Add(1)
	ch := make(chan []byte, 1)
	pendingMu.Lock()
	pending[id] = ch
	pendingMu.Unlock()
	b.Start(o.handle, method, w.b, id)
	w.free()
	var ans []byte
	select {
	case ans = <-ch:
	case <-ctx.Done():
		b.Cancel(id)
		ans = <-ch
	}
	runtime.KeepAlive(o)
	r := &Reader{b: ans}
	if err := o.mod.status(r); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return r, nil
}

// Done answers a started call (hosts, from any thread). The answer is
// copied.
func Done(call uint64, answer []byte) {
	pendingMu.Lock()
	ch := pending[call]
	delete(pending, call)
	pendingMu.Unlock()
	if ch != nil {
		ch <- append([]byte(nil), answer...)
	}
}

// Callback is a Go func passed to a native method, which the platform's
// side calls back. It lives until the call it's passed to returns (or,
// for a method that takes a context, until it answers); a notification
// the native side sent before then still runs, on the main thread.
type Callback struct{ id uint64 }

type callback struct {
	fn     func(r *Reader, w *Writer)
	notify bool
}

var (
	callbacks    sync.Map // id → *callback
	nextCallback atomic.Uint64
)

// NewCallback registers fn: a notification (notify: a func with no
// results) runs on the main thread, after the native side calls it; a
// question runs at once, on the native side's thread, and answers with
// what it writes.
func NewCallback(fn func(r *Reader, w *Writer), notify bool) *Callback {
	c := &Callback{id: nextCallback.Add(1)}
	callbacks.Store(c.id, &callback{fn, notify})
	return c
}

// Release ends the callback: the native side's later calls do nothing.
func (c *Callback) Release() { callbacks.Delete(c.id) }

// Invoke calls a callback with its arguments (hosts, from any thread):
// a notification is posted to the main thread and answers nil; a
// question answers with its result.
func Invoke(id uint64, args []byte) []byte {
	v, ok := callbacks.Load(id)
	if !ok {
		return nil
	}
	c := v.(*callback)
	if c.notify {
		// Delivered even if the call returns first: the native side sent
		// it while the callback was the call's.
		args = append([]byte(nil), args...)
		ui.Post(func() { c.fn(&Reader{b: args}, nil) })
		return nil
	}
	w := NewWriter()
	c.fn(&Reader{b: args}, w)
	out := append([]byte(nil), w.b...)
	w.free()
	return out
}
