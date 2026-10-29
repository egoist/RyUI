package signal

import (
	"strings"
	"testing"
)

func TestEffectRunsOnChange(t *testing.T) {
	s := New(1)
	var got []int
	Root(func(dispose func()) {
		Effect(func() { got = append(got, s.Get()) })
	})
	s.Set(2)
	s.Set(2) // equal: skipped
	s.Set(3)
	want := []int{1, 2, 3}
	if !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiamondIsGlitchFree(t *testing.T) {
	// a -> b, a -> c, (b, c) -> d: d must see consistent b and c, once.
	a := New(1)
	var bRuns, cRuns, dRuns int
	var seen []string
	Root(func(func()) {
		b := NewMemo(func() int { bRuns++; return a.Get() * 2 })
		c := NewMemo(func() int { cRuns++; return a.Get() * 3 })
		Effect(func() {
			dRuns++
			x, y := b.Get(), c.Get()
			if y*2 != x*3 {
				seen = append(seen, "glitch")
			}
		})
	})
	a.Set(2)
	a.Set(5)
	if len(seen) != 0 {
		t.Fatalf("saw inconsistent state: %v", seen)
	}
	if bRuns != 3 || cRuns != 3 || dRuns != 3 {
		t.Fatalf("runs b=%d c=%d d=%d, want 3 each", bRuns, cRuns, dRuns)
	}
}

func TestMemoIsLazyAndCached(t *testing.T) {
	a := New(1)
	runs := 0
	m := NewMemo(func() int { runs++; return a.Get() + 1 })
	if runs != 0 {
		t.Fatal("memo computed before Get")
	}
	if m.Get() != 2 || m.Get() != 2 || runs != 1 {
		t.Fatalf("runs=%d after two Gets", runs)
	}
	a.Set(5)
	if runs != 1 {
		t.Fatal("memo recomputed eagerly")
	}
	if m.Get() != 6 || runs != 2 {
		t.Fatalf("runs=%d", runs)
	}
}

func TestMemoEqualityCutsOff(t *testing.T) {
	a := New(1)
	effectRuns := 0
	Root(func(func()) {
		parity := NewMemo(func() bool { return a.Get()%2 == 0 })
		Effect(func() { parity.Get(); effectRuns++ })
	})
	a.Set(3) // parity unchanged
	a.Set(5)
	if effectRuns != 1 {
		t.Fatalf("effect ran %d times, want 1", effectRuns)
	}
	a.Set(6)
	if effectRuns != 2 {
		t.Fatalf("effect ran %d times, want 2", effectRuns)
	}
}

func TestDeepChainPropagates(t *testing.T) {
	a := New(0)
	var last Accessor[int] = a
	for i := 0; i < 50; i++ {
		prev := last
		last = NewMemo(func() int { return prev.Get() + 1 })
	}
	got := 0
	Root(func(func()) {
		Effect(func() { got = last.Get() })
	})
	a.Set(10)
	if got != 60 {
		t.Fatalf("got %d", got)
	}
}

func TestDynamicDependencies(t *testing.T) {
	cond, x, y := New(true), New("x"), New("y")
	runs := 0
	var got string
	Root(func(func()) {
		Effect(func() {
			runs++
			if cond.Get() {
				got = x.Get()
			} else {
				got = y.Get()
			}
		})
	})
	y.Set("y2") // not a dep yet
	if runs != 1 {
		t.Fatalf("runs=%d", runs)
	}
	cond.Set(false)
	x.Set("x2") // no longer a dep
	if runs != 2 || got != "y2" {
		t.Fatalf("runs=%d got=%q", runs, got)
	}
	if x.n.subs != nil {
		t.Fatal("x still has a subscriber")
	}
}

func TestBatch(t *testing.T) {
	a, b := New(1), New(2)
	runs := 0
	Root(func(func()) {
		Effect(func() { a.Get(); b.Get(); runs++ })
	})
	Batch(func() {
		a.Set(10)
		b.Set(20)
		if runs != 1 {
			t.Fatal("effect ran inside batch")
		}
	})
	if runs != 2 {
		t.Fatalf("runs=%d, want 2", runs)
	}
}

func TestDisposeStopsEffects(t *testing.T) {
	a := New(0)
	runs, cleanups := 0, 0
	var dispose func()
	Root(func(d func()) {
		dispose = d
		Effect(func() {
			a.Get()
			runs++
			OnCleanup(func() { cleanups++ })
		})
		m := NewMemo(func() int { return a.Get() })
		Effect(func() { m.Get() })
	})
	a.Set(1)
	if runs != 2 || cleanups != 1 {
		t.Fatalf("runs=%d cleanups=%d", runs, cleanups)
	}
	dispose()
	if cleanups != 2 {
		t.Fatalf("cleanups=%d after dispose", cleanups)
	}
	a.Set(2)
	if runs != 2 {
		t.Fatal("effect ran after dispose")
	}
	if a.n.subs != nil {
		t.Fatal("signal still has subscribers after dispose")
	}
}

func TestNestedEffectsAreDisposedOnRerun(t *testing.T) {
	outer, inner := New(0), New(0)
	innerRuns := 0
	Root(func(func()) {
		Effect(func() {
			outer.Get()
			Effect(func() { inner.Get(); innerRuns++ })
		})
	})
	outer.Set(1) // old inner effect disposed, new one made
	innerRuns = 0
	inner.Set(1)
	if innerRuns != 1 {
		t.Fatalf("inner ran %d times, want 1 (old inner leaked?)", innerRuns)
	}
}

func TestRootIsNotDisposedWithParent(t *testing.T) {
	a := New(0)
	runs := 0
	var disposeOuter func()
	Root(func(d func()) {
		disposeOuter = d
		Root(func(func()) {
			Effect(func() { a.Get(); runs++ })
		})
	})
	disposeOuter()
	a.Set(1)
	if runs != 2 {
		t.Fatalf("runs=%d: a detached root was disposed with its parent", runs)
	}
}

func TestRenderEffectsRunBeforeUserEffects(t *testing.T) {
	a := New(0)
	var order []string
	Root(func(func()) {
		Effect(func() { a.Get(); order = append(order, "user1") })
		RenderEffect(func() { a.Get(); order = append(order, "render") })
		Effect(func() { a.Get(); order = append(order, "user2") })
	})
	order = nil
	a.Set(1)
	want := "render,user1,user2"
	if got := strings.Join(order, ","); got != want {
		t.Fatalf("order %s, want %s", got, want)
	}
}

func TestEffectWriteSchedulesAnotherPass(t *testing.T) {
	a, b := New(0), New(0)
	var got int
	Root(func(func()) {
		Effect(func() { b.Set(a.Get() * 2) })
		Effect(func() { got = b.Get() })
	})
	a.Set(4)
	if got != 8 {
		t.Fatalf("got %d", got)
	}
}

func TestInfiniteLoopPanics(t *testing.T) {
	a := New(0)
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(r.(string), "passes") {
			t.Fatalf("recovered %v", r)
		}
	}()
	Root(func(func()) {
		Effect(func() { a.Set(a.Get() + 1) })
	})
}

func TestSetInsideMemoPanics(t *testing.T) {
	a := New(0)
	m := NewMemo(func() int { a.Set(1); return 0 })
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	m.Get()
}

func TestUntrack(t *testing.T) {
	a, b := New(0), New(0)
	runs := 0
	Root(func(func()) {
		Effect(func() {
			a.Get()
			Untrack(func() int { return b.Get() })
			runs++
		})
	})
	b.Set(1)
	if runs != 1 {
		t.Fatal("untracked read subscribed")
	}
}

func TestContext(t *testing.T) {
	ctx := NewContext("default")
	var got []string
	Root(func(func()) {
		got = append(got, ctx.Use())
		ctx.Provide("outer", func() {
			got = append(got, ctx.Use())
			ctx.Provide("inner", func() {
				Effect(func() { got = append(got, ctx.Use()) })
			})
			Root(func(func()) { got = append(got, ctx.Use()) })
		})
	})
	want := "default,outer,outer,inner"
	if g := strings.Join(got, ","); g != want {
		t.Fatalf("got %s, want %s", g, want)
	}
}

func TestStatic(t *testing.T) {
	a := Static(3)
	if v, ok := StaticValue(a); !ok || v != 3 {
		t.Fatal("StaticValue")
	}
	if _, ok := StaticValue[int](New(1)); ok {
		t.Fatal("signal reported static")
	}
}

func TestSteadyStateAllocatesNothing(t *testing.T) {
	a := New(0)
	sum := 0
	Root(func(func()) {
		m := NewMemo(func() int { return a.Get() * 2 })
		RenderEffect(func() { sum += m.Get() + a.Get() })
		Effect(func() { sum += a.Get() })
	})
	i := 0
	allocs := testing.AllocsPerRun(1000, func() {
		i++
		a.Set(i)
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per write", allocs)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
