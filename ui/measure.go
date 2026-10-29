package ui

import (
	"math"

	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

// measureKey is what a measurement depends on, for the kinds whose size
// is a function of their content: Text, Button, Toggle, Activity.
// Constraints are kept as bits, so NaN (unknown) keys compare equal.
type measureKey struct {
	kind      ops.Kind
	text      string
	variant   int32
	font      Font
	lines     int
	fontScale float32
	c         [4]uint32
}

const measureCacheMax = 1024

// measureCache answers repeated measurements of identical content
// without asking the host. Two generations bound it: when the current
// one is full it becomes the old one, and hits in the old one move back.
type measureCache struct {
	cur, old map[measureKey]layout.Size
	hits     int
}

func (m *measureCache) get(k *measureKey) (layout.Size, bool) {
	if s, ok := m.cur[*k]; ok {
		m.hits++
		return s, true
	}
	if s, ok := m.old[*k]; ok {
		m.hits++
		m.put(k, s)
		return s, true
	}
	return layout.Size{}, false
}

func (m *measureCache) put(k *measureKey, s layout.Size) {
	if m.cur == nil || len(m.cur) >= measureCacheMax {
		m.old = m.cur
		m.cur = make(map[measureKey]layout.Size, 64)
	}
	m.cur[*k] = s
}

func bits(f float32) uint32 { return math.Float32bits(f) }

func (e *element) cacheable() bool {
	switch e.kind {
	case ops.KindText, ops.KindButton, ops.KindToggle, ops.KindActivity, ops.KindSymbol:
		return true
	}
	return false
}

func (e *element) measure() {
	e.measured = true
	e.box.SetMeasure(func(c layout.Constraints) layout.Size {
		r := e.r
		if !e.cacheable() {
			return r.host.Measure(e.id, c)
		}
		k := measureKey{
			kind: e.kind, text: e.text, variant: e.variant,
			font: e.style.Font, lines: e.style.Lines, fontScale: r.envV.FontScale,
			c: [4]uint32{bits(c.Width), bits(c.Height), bits(c.AvailW), bits(c.AvailH)},
		}
		if s, ok := r.measures.get(&k); ok {
			return s
		}
		s := r.host.Measure(e.id, c)
		r.measures.put(&k, s)
		return s
	})
}
