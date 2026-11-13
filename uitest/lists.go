package uitest

import (
	"slices"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
)

// A List's rows are cells the host makes and asks the core to bind, as
// a collection view does: the fake host binds the rows that fit the
// list's height, from where it's scrolled to, and one more line.

type listState struct {
	cells map[int]*fake.View // bound, by row index
	free  []*fake.View       // unbound, to reuse
	first int                // the first row shown
	end   bool               // anchored to its end (a chat), not scrolled
	items []int64            // the list's items as last bound
	count int
}

// maxBound caps the rows a list binds at once: rows of no height would
// otherwise bind them all.
const maxBound = 200

func (st *listState) indices() []int {
	out := make([]int, 0, len(st.cells))
	for i := range st.cells {
		out = append(out, i)
	}
	slices.Sort(out)
	return out
}

// syncLists binds what each visible list shows, and lets go of the
// rows and lists that went; it reports whether it bound anything.
func (a *App) syncLists() bool {
	changed := false
	seen := map[ops.NodeID]bool{}
	for _, l := range a.layers() {
		a.walk(l, func(v *fake.View) bool {
			if v.Kind == ops.KindList && !seen[v.ID] {
				seen[v.ID] = true
				if a.syncList(v) {
					changed = true
				}
			}
			return true
		})
	}
	for id, st := range a.lists {
		if seen[id] {
			continue
		}
		if list := a.Host.Views[id]; list != nil { // hidden, or under another screen: kept, as a collection view keeps its cells
			continue
		}
		for _, c := range st.cells {
			delete(a.Host.Views, c.ID)
		}
		for _, c := range st.free {
			delete(a.Host.Views, c.ID)
		}
		delete(a.lists, id)
	}
	return changed
}

func (a *App) syncList(list *fake.View) bool {
	st := a.lists[list.ID]
	if st == nil {
		st = &listState{cells: map[int]*fake.View{}}
		st.end = ops.ScrollAnchor(list.Int(ops.PropListAnchor)) == ops.ScrollAnchorBottom
		a.lists[list.ID] = st
	}
	count := a.Host.ListCount(list)
	stale := count != st.count || !slices.Equal(st.items, list.Items)
	st.count = count
	st.items = slices.Clone(list.Items)
	height := list.Frame[3]
	if height <= 0 {
		height = a.Host.Env.Height
	}
	cols := max(1, int(list.Int(ops.PropListColumns)))
	changed := false
	bind := func(i int) float32 {
		c := st.cells[i]
		if c == nil {
			if n := len(st.free); n > 0 {
				c, st.free = st.free[n-1], st.free[:n-1]
			} else {
				c = a.Host.NewCell()
			}
			st.cells[i] = c
			a.Host.Bind(list, i, c)
			changed = true
		} else if stale {
			a.Host.Bind(list, i, c)
		}
		return c.Frame[3]
	}
	// The rows that show: from the first, or back from the last for a
	// list at its end, until they fill the height, and a line more.
	lo, hi := 0, 0
	if st.end {
		hi = count
		lo = hi
		for filled := float32(0); lo > 0 && filled <= height && hi-lo < maxBound; {
			line := lo - 1 - (lo-1)%cols
			var h float32
			for i := line; i < lo; i++ {
				h = max(h, bind(i))
			}
			filled += h
			lo = line
		}
	} else {
		lo = min(st.first, max(0, count-1))
		lo -= lo % cols
		hi = lo
		for filled := float32(0); hi < count && filled <= height && hi-lo < maxBound; {
			var h float32
			for i := hi; i < min(hi+cols, count); i++ {
				h = max(h, bind(i))
			}
			filled += h
			hi = min(hi+cols, count)
		}
	}
	for i, c := range st.cells {
		if i < lo || i >= hi {
			a.Host.Unbind(list, c)
			delete(st.cells, i)
			st.free = append(st.free, c)
			changed = true
		}
	}
	return changed
}

// cellOf returns the list cell v is in, and its list, or nils.
func (a *App) cellOf(v *fake.View) (cell, list *fake.View) {
	for v.Parent != nil {
		v = v.Parent
	}
	if v.Kind != fake.KindCell {
		return nil, nil
	}
	for id, st := range a.lists {
		for _, c := range st.cells {
			if c == v {
				return c, a.Host.Views[id]
			}
		}
	}
	return nil, nil
}

// Row returns the list row (its cell) that shows text, failing unless
// exactly one shows it: a.In(uitest.Under(a.Row("Ada"))) finds and taps
// in it.
func (a *App) Row(text string) *fake.View {
	a.t.Helper()
	var rows []*fake.View
	for _, t := range a.targets(a.layers(), text) {
		if cell, _ := a.cellOf(t.v); cell != nil && !slices.Contains(rows, cell) {
			rows = append(rows, cell)
		}
	}
	if len(rows) != 1 {
		a.t.Fatalf("uitest: %d list rows show %q\n%s", len(rows), text, a.screenFor())
	}
	return rows[0]
}

// ScrollTo scrolls the lists on the screen until one shows text, as a
// user would scroll looking for it, and fails if none does.
func (a *App) ScrollTo(text string) {
	a.t.Helper()
	if a.Has(text) {
		return
	}
	for _, list := range a.All(ops.KindList) {
		st := a.lists[list.ID]
		if st == nil {
			continue
		}
		st.end = false
		for first := 0; first < st.count; {
			st.first = first
			a.Settle()
			if a.Has(text) {
				return
			}
			n := len(st.cells)
			if n == 0 {
				break
			}
			first += n
		}
	}
	a.t.Fatalf("uitest: no list shows %q, scrolled through\n%s", text, a.screenFor())
}
