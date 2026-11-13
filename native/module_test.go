package native

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeBackend is a platform's side for tests: method 1 echoes its
// string; 2 answers with the status its argument says; 3 calls back the
// callback it gets (a question) and answers with what it said; a started
// call answers when canceled, or at once with its string.
type fakeBackend struct {
	released []uint64
	canceled chan uint64
}

func (f *fakeBackend) New(m *Module, iface uint32, out []byte) []byte {
	w := &Writer{b: out[:0]}
	w.Uint8(0)
	w.Uint64(uint64(40 + iface))
	return w.b
}

func (f *fakeBackend) Call(h uint64, method uint32, in, out []byte) []byte {
	r := Reader{b: in}
	w := Writer{b: out[:0]}
	switch method {
	case 1:
		w.Uint8(0)
		w.String(r.String() + "!")
	case 2:
		switch s := r.Uint8(); s {
		case 1:
			w.Uint8(1)
			w.Uint32(r.Uint32())
		case 2:
			w.Uint8(2)
			w.String("Oven")
			w.Int64(7)
			w.String("no oven")
		case 3:
			w.Uint8(3)
			w.String("no ounces")
		default:
			w.Uint8(s)
		}
	case 3:
		cb := r.Uint64()
		q := NewWriter()
		q.String("ok?")
		a := NewReader(Invoke(cb, q.Take()))
		w.Uint8(0)
		w.Bool(a.Bool())
	}
	return w.b
}

func (f *fakeBackend) Start(h uint64, method uint32, in []byte, call uint64) {
	s := NewReader(in).String()
	go func() {
		if s == "slow" {
			<-f.canceled
			Done(call, []byte{4})
			return
		}
		w := NewWriter()
		w.Uint8(0)
		w.String(s + " done")
		Done(call, w.Take())
	}()
}

func (f *fakeBackend) Cancel(call uint64) { f.canceled <- call }
func (f *fakeBackend) Release(h uint64)   { f.released = append(f.released, h) }

var errBusy = NewError("busy")

func withFake(t testing.TB) (*fakeBackend, *Module) {
	f := &fakeBackend{canceled: make(chan uint64, 1)}
	old := backend.Load()
	SetBackend(f)
	t.Cleanup(func() { backend.Store(old) })
	return f, &Module{ID: "example.com/kitchen", Errors: []error{errBusy}}
}

func call(t *testing.T, o *Object, method uint32, write func(w *Writer)) (*Reader, error) {
	t.Helper()
	w := NewWriter()
	write(w)
	return o.Call(method, w)
}

// Calls reach the backend and come back decoded; each answer's status is
// its error.
func TestModuleCalls(t *testing.T) {
	f, m := withFake(t)
	o, err := m.New(2, "Kitchen")
	if err != nil || o.handle != 42 {
		t.Fatal(o, err)
	}
	r, err := call(t, o, 1, func(w *Writer) { w.String("hi") })
	if err != nil {
		t.Fatal(err)
	}
	if s := r.String(); s != "hi!" || r.Done() != nil {
		t.Errorf("echo %q", s)
	}
	status := func(s uint8, extra func(w *Writer)) error {
		_, err := call(t, o, 2, func(w *Writer) {
			w.Uint8(s)
			if extra != nil {
				extra(w)
			}
		})
		return err
	}
	if err := status(1, func(w *Writer) { w.Uint32(0) }); err != errBusy {
		t.Errorf("declared: %v", err)
	}
	var ne *Error
	if err := status(2, nil); !errors.As(err, &ne) || ne.Domain != "Oven" || ne.Code != 7 || ne.Message != "no oven" || err.Error() != "no oven (Oven 7)" {
		t.Errorf("error: %#v", err)
	}
	if err := status(3, nil); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("unsupported: %v", err)
	}
	if err := status(1, func(w *Writer) { w.Uint32(9) }); err == nil {
		t.Error("an undeclared index: no error")
	}

	// A question callback answers on the backend's thread.
	var asked string
	cb := NewCallback(func(r *Reader, w *Writer) { asked = r.String(); w.Bool(true) }, false)
	r, err = call(t, o, 3, func(w *Writer) { w.Func(cb) })
	cb.Release()
	if err != nil || !r.Bool() || asked != "ok?" {
		t.Errorf("question: %v %q", err, asked)
	}
	r.Done()

	o.Release()
	if _, err := call(t, o, 1, func(w *Writer) {}); err != ErrClosed {
		t.Errorf("after Release: %v", err)
	}
	if len(f.released) != 1 || f.released[0] != 42 {
		t.Errorf("released %v", f.released)
	}
}

// A call that takes a context answers when its work does, or is
// canceled with it.
func TestModuleCallContext(t *testing.T) {
	_, m := withFake(t)
	o, _ := m.New(0, "Kitchen")
	w := NewWriter()
	w.String("soup")
	r, err := o.CallContext(context.Background(), 4, w)
	if err != nil || r.String() != "soup done" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	w = NewWriter()
	w.String("slow")
	if _, err := o.CallContext(ctx, 4, w); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("canceled: %v", err)
	}
	done, stop := context.WithCancel(context.Background())
	stop()
	if _, err := o.CallContext(done, 4, NewWriter()); !errors.Is(err, context.Canceled) {
		t.Errorf("done before: %v", err)
	}
}

// Every kind of value, there and back; a short read is an error, its
// reads zero.
func TestCodec(t *testing.T) {
	at := time.Unix(1_700_000_000, 250)
	w := NewWriter()
	w.Bool(true)
	w.Int8(-3)
	w.Uint16(65000)
	w.Int32(-70000)
	w.Int(-1 << 40)
	w.Uint(1 << 63)
	w.Float32(0.5)
	w.Float64(-2.25)
	w.String("héllo")
	w.Data([]byte{1, 2})
	w.Data(nil)
	w.Time(at)
	w.Duration(-time.Second)
	w.Len(2)
	r := NewReader(w.Take())
	if !r.Bool() || r.Int8() != -3 || r.Uint16() != 65000 || r.Int32() != -70000 || r.Int() != -1<<40 || r.Uint() != 1<<63 ||
		r.Float32() != 0.5 || r.Float64() != -2.25 || r.String() != "héllo" || string(r.Data()) != "\x01\x02" || r.Data() != nil ||
		!r.Time().Equal(at) || r.Duration() != -time.Second {
		t.Fatal("round trip")
	}
	if r.Len() != 0 || r.Err() == nil {
		t.Error("a length past what's left: no error")
	}
	if r.String() != "" || r.Uint64() != 0 {
		t.Error("reads after a short one aren't zero")
	}
}

// A call's steady state: its writer and its answer's buffer are pooled.
func BenchmarkModuleCall(b *testing.B) {
	_, m := withFake(b)
	o, _ := m.New(0, "Kitchen")
	b.ReportAllocs()
	for b.Loop() {
		w := NewWriter()
		w.String("hi")
		r, err := o.Call(1, w)
		if err != nil {
			b.Fatal(err)
		}
		_ = r.String()
		r.Done()
	}
}
