//go:build js

package watch

import (
	"fmt"
	"strings"
	"sync"
	"syscall/js"
)

// The web: Go's os package on js/wasm calls the fs object of the page
// (globalThis.fs), which the web host makes a file system in memory
// (platform/web/src/fs.js) and which, under Node (go test), is Node's. Both
// have Node's fs.watch(path, {recursive}, listener): the listener is
// told "rename" or "change" and the name of what changed, relative to the
// directory. The web host's tells, a moment later, what the app's Go
// changed in it; under Node it is the system's watch.

type jsTarget struct {
	watcher  js.Value
	listener js.Func
	onError  js.Func
}

type jsWatch struct {
	// notices are what the listeners were told, up to jsQueueSize;
	// overflow says there were more, so the scan must look at everything.
	mu       sync.Mutex
	notices  []notice
	overflow bool
	signal   chan struct{}
	// targets by ID; only the driver's goroutine uses them.
	targets map[uint64]*jsTarget
}

func newPlatform(notify func(notice), opts Options) (native, error) {
	fs := js.Global().Get("fs")
	if fs.Type() != js.TypeObject || fs.Get("watch").Type() != js.TypeFunction {
		return nil, ErrUnsupported
	}
	d := &jsWatch{signal: make(chan struct{}, 1), targets: map[uint64]*jsTarget{}}
	return runDriver(d, notify), nil
}

func (d *jsWatch) add(t target) (err error) {
	if !t.dir {
		return ErrUnsupported // watched through its directory
	}
	id := t.id
	j := &jsTarget{}
	j.listener = js.FuncOf(func(this js.Value, args []js.Value) any {
		n := notice{target: id}
		if len(args) > 0 && args[0].String() == "change" {
			n.kind = kindWrite
		}
		if len(args) > 1 && args[1].Type() == js.TypeString {
			n.name = strings.ReplaceAll(args[1].String(), `\`, "/")
		}
		d.post(n)
		return nil
	})
	j.onError = js.FuncOf(func(this js.Value, args []js.Value) any {
		d.post(notice{target: id, kind: kindLost})
		return nil
	})
	defer func() {
		if r := recover(); r != nil {
			j.listener.Release()
			j.onError.Release()
			if t.subtree {
				err = ErrUnsupported // the scan watches each directory instead
				return
			}
			err = fmt.Errorf("fs.watch %s: %v", t.path, r)
		}
	}()
	options := js.Global().Get("Object").New()
	options.Set("recursive", t.subtree)
	options.Set("persistent", false)
	j.watcher = js.Global().Get("fs").Call("watch", t.path, options, j.listener)
	if on := j.watcher.Get("on"); on.Type() == js.TypeFunction {
		j.watcher.Call("on", "error", j.onError)
	}
	if old := d.targets[id]; old != nil {
		old.close()
	}
	d.targets[id] = j
	return nil
}

func (j *jsTarget) close() {
	func() {
		defer func() { recover() }()
		j.watcher.Call("close")
	}()
	j.listener.Release()
	j.onError.Release()
}

func (d *jsWatch) remove(id uint64) {
	if j := d.targets[id]; j != nil {
		j.close()
		delete(d.targets, id)
	}
}

// jsQueueSize is how many notices a watch holds before it asks the scan
// to look at everything again.
const jsQueueSize = 4096

// post queues what a listener was told, for wait.
func (d *jsWatch) post(n notice) {
	d.mu.Lock()
	if len(d.notices) < jsQueueSize {
		d.notices = append(d.notices, n)
	} else {
		d.overflow = true
	}
	d.mu.Unlock()
	d.wake()
}

func (d *jsWatch) wait() ([]notice, error) {
	<-d.signal
	d.mu.Lock()
	defer d.mu.Unlock()
	notices := d.notices
	d.notices = nil
	if d.overflow {
		d.overflow = false
		return []notice{{err: errOverflow}}, nil
	}
	return notices, nil
}

func (d *jsWatch) wake() {
	select {
	case d.signal <- struct{}{}:
	default:
	}
}

func (d *jsWatch) close() {
	for id, j := range d.targets {
		j.close()
		delete(d.targets, id)
	}
}
