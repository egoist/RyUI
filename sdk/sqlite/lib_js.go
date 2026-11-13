//go:build js && wasm

package sqlite

import (
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"sync"
	"syscall/js"
	"unsafe"

	"github.com/egoist/asuka/internal/jsloop"
)

// In a browser, SQLite is the same C built to WebAssembly
// (internal/sqlitebuild's wasm target, platform/web/sqlite/asqlite.wasm),
// in a worker of the page's (platform/web/src/sqlite-worker.js) that
// keeps databases in the origin private file system. A call is a message
// to the worker: the goroutine that makes it waits for the answer, which
// the page writes into the call's buffer before it wakes the goroutine.
//
// The page's callbacks (the main thread) can't wait: the page waits for
// them to return before it hears from the worker. A call there fails
// with an error that says so instead (jsloop); main, before app.Run, isn't one.
//
// A database file has one connection (no shared memory between them, so
// no write-ahead logging), and SQLite never waits for a lock: a tab that
// finds another holding the database gets SQLITE_BUSY at once while the
// worker asks for it, and the statement runs again from Go (conn.retry).

// errMainThread is the error of a call that would wait on the page's
// main thread (an event's, a posted function's): in a browser, use
// ExecAsync, TxAsync and Watch there, or a goroutine.
var errMainThread = &Error{Code: codeMisuse, Message: "this call waits for the database, which the page's main thread can't do: use ExecAsync, TxAsync or Watch, or a goroutine"}

func init() {
	blocked = func() error {
		if jsloop.InCallback() {
			return errMainThread
		}
		return nil
	}
}

// webLib is the page's SQLite worker, through the host's sqlite.js.
type webLib struct {
	callFn js.Value
	done   js.Func // the page wakes a call's goroutine with it

	mu      sync.Mutex
	next    uint32
	pending map[uint32]chan int // the calls waiting, by id
}

// waiters are the channels calls wait on, reused.
var waiters = sync.Pool{New: func() any { return make(chan int, 1) }}

func loadLibrary() (library, error) {
	name := os.Getenv("ASUKA_WEB_HOST")
	if name == "" {
		name = "asukaHost"
	}
	h := js.Global().Get(name)
	if h.IsUndefined() {
		return nil, errors.New("no web host on this page")
	}
	api := h.Get("sqlite")
	if api.IsUndefined() || api.IsNull() {
		return nil, errors.New("the page didn't load SQLite (asuka build --platform web ships it with the apps that import asuka/sdk/sqlite; a page of your own passes start asqlite.wasm in its libs option)")
	}
	l := &webLib{pending: map[uint32]chan int{}}
	l.done = js.FuncOf(func(_ js.Value, args []js.Value) any {
		l.answer(uint32(args[0].Int()), args[1].Int())
		return nil
	})
	api.Call("connect", l.done)
	l.callFn = api.Get("call").Call("bind", api)
	return l, nil
}

func (l *webLib) answer(id uint32, n int) {
	l.mu.Lock()
	ch := l.pending[id]
	delete(l.pending, id)
	l.mu.Unlock()
	if ch != nil {
		ch <- n
	}
}

func (l *webLib) call(op, conn, arg uintptr, in, out []byte) int {
	if op != opTake && op != opInterrupt && jsloop.InCallback() {
		return errorAnswer(out, errMainThread)
	}
	ch := waiters.Get().(chan int)
	l.mu.Lock()
	l.next++
	id := l.next
	l.pending[id] = ch
	l.mu.Unlock()
	r := l.callFn.Invoke(op, conn, arg, addr(in), len(in), addr(out), len(out), id)
	var n int
	if r.Type() == js.TypeNumber {
		// Answered at once (TAKE: the page kept the answer).
		n = r.Int()
		l.mu.Lock()
		delete(l.pending, id)
		l.mu.Unlock()
	} else {
		n = <-ch
	}
	waiters.Put(ch)
	// The page wrote the answer into out (Go's heap doesn't move).
	runtime.KeepAlive(in)
	runtime.KeepAlive(out)
	return n
}

func (l *webLib) conns() int  { return 1 }
func (l *webLib) wal() bool   { return false }
func (l *webLib) waits() bool { return false }

func addr(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return int(uintptr(unsafe.Pointer(unsafe.SliceData(b))))
}

// errorAnswer writes an answer that says e (asq.h's), as the shim would.
func errorAnswer(out []byte, e *Error) int {
	b := append(out[:0], 0, 0, 0, 0, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(e.Code))
	b = append(b, 1)
	b = appendString(b, e.Message)
	binary.LittleEndian.PutUint32(b[1:], uint32(len(b)))
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0)
	if len(b) > len(out) {
		return 0
	}
	return len(b)
}
