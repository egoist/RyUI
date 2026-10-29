package signal

import "testing"

// Cases modeled on js-reactivity-benchmark's kairo suite.

func BenchmarkDeepPropagation(b *testing.B) {
	head := New(0)
	var cur Accessor[int] = head
	for i := 0; i < 50; i++ {
		prev := cur
		cur = NewMemo(func() int { return prev.Get() + 1 })
	}
	sum := 0
	Root(func(func()) { Effect(func() { sum += cur.Get() }) })
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		head.Set(i + 1)
	}
}

func BenchmarkBroadPropagation(b *testing.B) {
	head := New(0)
	sum := 0
	Root(func(func()) {
		for i := 0; i < 50; i++ {
			m := NewMemo(func() int { return head.Get() + i })
			m2 := NewMemo(func() int { return m.Get() + 1 })
			Effect(func() { sum += m2.Get() })
		}
	})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		head.Set(i + 1)
	}
}

func BenchmarkDiamond(b *testing.B) {
	head := New(0)
	sum := 0
	Root(func(func()) {
		var branches []*Memo[int]
		for i := 0; i < 5; i++ {
			branches = append(branches, NewMemo(func() int { return head.Get() + 1 }))
		}
		total := NewMemo(func() int {
			t := 0
			for _, m := range branches {
				t += m.Get()
			}
			return t
		})
		Effect(func() { sum += total.Get() })
	})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		head.Set(i + 1)
	}
}

func BenchmarkUnstable(b *testing.B) {
	head := New(0)
	sum := 0
	Root(func(func()) {
		double := NewMemo(func() int { return head.Get() * 2 })
		inverse := NewMemo(func() int { return -head.Get() })
		cur := NewMemo(func() int {
			r := 0
			for i := 0; i < 20; i++ {
				if head.Get()%2 == 1 {
					r += double.Get()
				} else {
					r += inverse.Get()
				}
			}
			return r
		})
		Effect(func() { sum += cur.Get() })
	})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		head.Set(i + 1)
	}
}
