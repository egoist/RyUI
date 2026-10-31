package ui

import (
	"reflect"

	"github.com/egoist/ryui/signal"
)

// Show shows then() while when is true, else the first of otherwise if
// given. Each branch has its own owner: switching disposes the old
// branch (its effects stop, its views are destroyed) and builds the new
// one.
func Show(when signal.Accessor[bool], then func() Node, otherwise ...func() Node) Node {
	f := newFragment()
	r := f.r
	var dispose func()
	var shown *element
	state := -1
	signal.RenderEffect(func() {
		v := when.Get()
		s := 0
		if v {
			s = 1
		}
		if s == state {
			return
		}
		state = s
		if shown != nil {
			f.removeKid(shown, true)
			shown = nil
		}
		if dispose != nil {
			dispose()
			dispose = nil
		}
		build := then
		if !v {
			build = nil
			if len(otherwise) > 0 {
				build = otherwise[0]
			}
		}
		if build == nil {
			return
		}
		signal.Root(func(d func()) {
			dispose = d
			if n := withRoot(r, build); n.e != nil {
				shown = n.e
				f.appendKid(shown)
			}
		})
	})
	signal.OnCleanup(func() {
		if dispose != nil {
			dispose()
		}
	})
	return Node{f}
}

// Dynamic shows what f returns, rebuilding it when what f reads changes.
// Everything f reads is tracked, including reads in the components it
// calls; prefer Show and For.
func Dynamic(f func() Node) Node {
	frag := newFragment()
	r := frag.r
	var shown *element
	signal.RenderEffect(func() {
		if shown != nil {
			frag.removeKid(shown, true)
			shown = nil
		}
		if n := withRoot(r, f); n.e != nil {
			shown = n.e
			frag.appendKid(shown)
		}
	})
	return Node{frag}
}

// equality returns == for comparable T, else nil (every write counts as
// a change; the row's prop effects still send only real changes).
func equality[T any]() func(a, b T) bool {
	if reflect.TypeFor[T]().Comparable() {
		return func(a, b T) bool { return any(a) == any(b) }
	}
	return nil
}

type forRow[T any, K comparable] struct {
	key     K
	item    *signal.Signal[T]
	index   *signal.Signal[int]
	el      *element
	dispose func()
	old     int // position before this update
}

// For shows a row per item, keyed: a row lives as long as its key. A
// moved item moves its row's views; a changed item updates that row's
// item signal and nothing else.
func For[T any, K comparable](items signal.Accessor[[]T], key func(T) K, row func(item signal.Accessor[T], index signal.Accessor[int]) Node) Node {
	f := newFragment()
	r := f.r
	eq := equality[T]()
	var rows []*forRow[T, K]
	signal.RenderEffect(func() {
		list := items.Get()
		signal.Untrack(func() struct{} {
			rows = reconcile(f, r, rows, list, key, row, eq)
			return struct{}{}
		})
	})
	signal.OnCleanup(func() {
		for _, rw := range rows {
			rw.dispose()
		}
	})
	return Node{f}
}

func reconcile[T any, K comparable](f *element, r *Root, rows []*forRow[T, K], list []T, key func(T) K,
	row func(signal.Accessor[T], signal.Accessor[int]) Node, eq func(a, b T) bool) []*forRow[T, K] {
	old := make(map[K]*forRow[T, K], len(rows))
	for i, rw := range rows {
		rw.old = i
		old[rw.key] = rw
	}
	next := make([]*forRow[T, K], len(list))
	for i, it := range list {
		k := key(it)
		if rw := old[k]; rw != nil {
			delete(old, k)
			rw.item.Set(it)
			rw.index.Set(i)
			next[i] = rw
			continue
		}
		rw := &forRow[T, K]{key: k, old: -1}
		signal.Root(func(d func()) {
			rw.dispose = d
			rw.item = signal.NewFunc(it, eq)
			rw.index = signal.New(i)
			rw.el = withRoot(r, func() Node { return row(rw.item, rw.index) }).e
		})
		next[i] = rw
	}
	// Removed rows.
	for _, rw := range old {
		if rw.el != nil {
			f.removeKid(rw.el, true)
		}
		rw.dispose()
	}
	// Kept rows on the longest increasing run of old positions stay put;
	// the others move.
	stay := lis(next)
	for i, rw := range next {
		if rw.old >= 0 && !stay[i] && rw.el != nil {
			rw.el.detachNative()
		}
	}
	f.kids = f.kids[:0]
	for _, rw := range next {
		if rw.el != nil {
			rw.el.parent = f
			f.kids = append(f.kids, rw.el)
		}
	}
	f.markKids()
	for i, rw := range next {
		if rw.el != nil && (rw.old < 0 || !stay[i]) {
			rw.el.attachNative()
		}
	}
	return next
}

// lis marks the rows (with old >= 0) on a longest increasing subsequence
// of old positions.
func lis[T any, K comparable](rows []*forRow[T, K]) []bool {
	stay := make([]bool, len(rows))
	var tails []int // index into rows of the smallest tail per length
	prev := make([]int, len(rows))
	for i, rw := range rows {
		if rw.old < 0 {
			continue
		}
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if rows[tails[mid]].old < rw.old {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > 0 {
			prev[i] = tails[lo-1]
		} else {
			prev[i] = -1
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}
	if len(tails) > 0 {
		for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
			stay[i] = true
		}
	}
	return stay
}
