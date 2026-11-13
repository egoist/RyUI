package hotkey_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/hotkey"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// desktop is a fake host's "hotkey" module, as a desktop host's: it
// keeps the registrations, answers "register" with answer's (nil: at
// once; hold: later, with reply), holds "next" until a press, and
// fails it at "stop".
type desktop struct {
	calls   []string
	regs    map[uint32]reg
	answer  func(r reg) error
	held    map[uint32]func([]byte, error) // registers answered later
	hold    bool
	waiting func([]byte, error)
	presses []byte
}

type reg struct {
	id   uint32
	mods ui.Modifiers
	key  string
	desc string
}

var errHold = errors.New("hold")

func (d *desktop) module(method string, args []byte, reply func([]byte, error)) {
	d.calls = append(d.calls, method)
	le := binary.LittleEndian
	switch method {
	case "register":
		n := le.Uint32(args[5:])
		r := reg{id: le.Uint32(args), mods: ui.Modifiers(args[4]), key: string(args[9 : 9+n]), desc: string(args[9+n:])}
		if d.hold {
			d.held[r.id] = reply
			d.regs[r.id] = r
			return
		}
		if d.answer != nil {
			if err := d.answer(r); err != nil {
				reply(nil, err)
				return
			}
		}
		d.regs[r.id] = r
		reply(nil, nil)
	case "unregister":
		delete(d.regs, le.Uint32(args))
		reply(nil, nil)
	case "next":
		if d.waiting != nil {
			panic("two polls")
		}
		d.waiting = reply
		d.flush()
	case "stop":
		if w := d.waiting; w != nil {
			d.waiting = nil
			w(nil, errors.New("stopped"))
		}
		reply(nil, nil)
	}
}

// press presses the registration with these keys, as the system would.
func (d *desktop) press(key string) {
	for id, r := range d.regs {
		if r.key == key {
			d.presses = binary.LittleEndian.AppendUint32(d.presses, id)
		}
	}
	d.flush()
}

func (d *desktop) flush() {
	if d.waiting != nil && len(d.presses) > 0 {
		w, b := d.waiting, d.presses
		d.waiting, d.presses = nil, nil
		w(b, nil)
	}
}

func (d *desktop) count(method string) int {
	n := 0
	for _, c := range d.calls {
		if c == method {
			n++
		}
	}
	return n
}

func mount(t *testing.T, root func() ui.Node) (*desktop, *ui.Root, func()) {
	t.Helper()
	h := fake.New()
	d := &desktop{regs: map[uint32]reg{}, held: map[uint32]func([]byte, error){}}
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"hotkey": d.module}
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	settle := func() {
		for range 100 {
			h.RunPosted()
			r.Flush()
		}
	}
	settle()
	return d, r, settle
}

// Registered in a component: each press runs OnPress on the main
// thread; disposing the component lets the keys go and ends the poll.
func TestRegisterPressDispose(t *testing.T) {
	shown := signal.New(true)
	presses := 0
	var k *hotkey.Hotkey
	d, r, settle := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			k = hotkey.Register(hotkey.Props{
				Shortcut:    ui.Val(ui.Cmd("Space").With(ui.Shift)),
				Description: ui.Val("Show the launcher"),
				OnPress:     func() { presses++ },
			})
			return ui.StaticText("x")
		})
	})
	if k.Status() != hotkey.Active || k.Err() != nil {
		t.Fatalf("status %v, %v", k.Status(), k.Err())
	}
	if len(d.regs) != 1 {
		t.Fatalf("registrations %+v", d.regs)
	}
	for _, g := range d.regs {
		if g.key != "space" || g.mods != ui.Primary|ui.Shift || g.desc != "Show the launcher" {
			t.Fatalf("registered %+v", g)
		}
	}
	d.press("space")
	settle()
	d.press("space") // answers the waiting poll
	d.press("space") // kept until the poll asks again
	d.press("space")
	settle()
	if presses != 4 {
		t.Fatalf("%d presses", presses)
	}
	if d.count("next") != 4 {
		t.Fatalf("%d polls", d.count("next"))
	}
	shown.Set(false)
	r.Flush()
	settle()
	if len(d.regs) != 0 || d.count("stop") != 1 || d.waiting != nil {
		t.Fatalf("after dispose: %+v, calls %v", d.regs, d.calls)
	}
	if k.Status() != hotkey.Off {
		t.Fatalf("status %v", k.Status())
	}
	polls := d.count("next")
	d.press("space")
	settle()
	if presses != 4 || d.count("next") != polls {
		t.Fatalf("after dispose: %d presses, %d polls", presses, d.count("next"))
	}
	// Registered again: the poll starts again.
	shown.Set(true)
	r.Flush()
	settle()
	d.press("space")
	settle()
	if presses != 5 || k.Status() != hotkey.Active {
		t.Fatalf("again: %d presses, %v", presses, k.Status())
	}
}

// The shortcut follows its accessor: the old keys go, the new ones are
// registered; no key registers nothing.
func TestShortcutFollows(t *testing.T) {
	keys := signal.New(ui.Cmd("k"))
	got := ""
	var k *hotkey.Hotkey
	d, r, settle := mount(t, func() ui.Node {
		k = hotkey.Register(hotkey.Props{Shortcut: keys, OnPress: func() { got += "!" }})
		return ui.StaticText("x")
	})
	keys.Set(ui.Cmd("j").With(ui.Alt))
	r.Flush()
	settle()
	if len(d.regs) != 1 || d.count("unregister") != 1 {
		t.Fatalf("after the change: %+v, calls %v", d.regs, d.calls)
	}
	for _, g := range d.regs {
		if g.key != "j" || g.mods != ui.Primary|ui.Alt {
			t.Fatalf("registered %+v", g)
		}
	}
	d.press("k")
	d.press("j")
	settle()
	if got != "!" {
		t.Fatalf("presses %q, calls %v, waiting %v, regs %+v", got, d.calls, d.waiting != nil, d.regs)
	}
	keys.Set(ui.Shortcut{})
	r.Flush()
	settle()
	if len(d.regs) != 0 || k.Status() != hotkey.Off || k.Err() != nil {
		t.Fatalf("cleared: %+v, %v %v", d.regs, k.Status(), k.Err())
	}
}

// The host's refusals are Err's errors; the same keys twice in the app
// are taken without asking the host.
func TestErrors(t *testing.T) {
	answers := map[string]error{
		"b": errors.New("taken"),
		"c": errors.New("not allowed"),
		"d": errors.New("no key"),
		"e": errors.New("not available"),
		"f": errors.New("asuka: hotkey.register isn't on the web: a page gets the keyboard only while it has the focus"),
		"g": errors.New("no module method hotkey.register"),
	}
	h := fake.New()
	d := &desktop{regs: map[uint32]reg{}, held: map[uint32]func([]byte, error){}, answer: func(r reg) error { return answers[r.key] }}
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"hotkey": d.module}
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "a"}
	var ks []*hotkey.Hotkey
	r := ui.Mount(h, func() ui.Node {
		for _, key := range keys {
			ks = append(ks, hotkey.Register(hotkey.Props{Shortcut: ui.Val(ui.Cmd(key))}))
		}
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	for range 100 {
		h.RunPosted()
		r.Flush()
	}
	want := []error{nil, hotkey.ErrTaken, hotkey.ErrNotAllowed, hotkey.ErrKey, hotkey.ErrNotAvailable, hotkey.ErrNotAvailable, hotkey.ErrNotAvailable, hotkey.ErrTaken}
	for i, k := range ks {
		if k.Err() != want[i] {
			t.Errorf("%s: %v, want %v", keys[i], k.Err(), want[i])
		}
		if st := k.Status(); (want[i] == nil) != (st == hotkey.Active) || want[i] != nil && st != hotkey.Failed {
			t.Errorf("%s: status %v", keys[i], st)
		}
	}
	if d.count("register") != 7 {
		t.Fatalf("%d registers: the app's own duplicate needn't ask", d.count("register"))
	}
	// A phone: no module at all.
	m := fake.New()
	r2 := ui.Mount(m, func() ui.Node {
		ks = append(ks[:0], hotkey.Register(hotkey.Props{Shortcut: ui.Val(ui.Cmd("x"))}))
		return ui.StaticText("x")
	})
	t.Cleanup(r2.Dispose)
	for range 10 {
		m.RunPosted()
		r2.Flush()
	}
	if ks[0].Err() != hotkey.ErrNotAvailable {
		t.Fatalf("no module: %v", ks[0].Err())
	}
}

// Pending until the host answers (Linux's portal may ask the user);
// let go meanwhile, the late answer is ignored. Unregister lets go
// before the owner is disposed.
func TestPendingAndUnregister(t *testing.T) {
	h := fake.New()
	d := &desktop{regs: map[uint32]reg{}, held: map[uint32]func([]byte, error){}, hold: true}
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"hotkey": d.module}
	var a, b *hotkey.Hotkey
	pressed := 0
	r := ui.Mount(h, func() ui.Node {
		a = hotkey.Register(hotkey.Props{Shortcut: ui.Val(ui.Cmd("a")), OnPress: func() { pressed++ }})
		b = hotkey.Register(hotkey.Props{Shortcut: ui.Val(ui.Cmd("b")), OnPress: func() { pressed++ }})
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	settle := func() {
		for range 50 {
			h.RunPosted()
			r.Flush()
		}
	}
	settle()
	if a.Status() != hotkey.Pending || b.Status() != hotkey.Pending || d.count("next") != 0 {
		t.Fatalf("before answers: %v %v, %d polls", a.Status(), b.Status(), d.count("next"))
	}
	b.Unregister()
	settle()
	if b.Status() != hotkey.Off || d.count("unregister") != 1 {
		t.Fatalf("let go while pending: %v, calls %v", b.Status(), d.calls)
	}
	for id, reply := range d.held {
		reply(nil, nil)
		delete(d.held, id)
	}
	settle()
	if a.Status() != hotkey.Active || b.Status() != hotkey.Off {
		t.Fatalf("after answers: %v %v", a.Status(), b.Status())
	}
	d.press("a")
	d.press("b") // let go: nothing
	settle()
	if pressed != 1 {
		t.Fatalf("%d presses", pressed)
	}
	a.Unregister()
	settle()
	if d.count("stop") != 1 || d.waiting != nil {
		t.Fatalf("the poll didn't stop: %v", d.calls)
	}
}
