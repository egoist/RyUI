//go:build linux && !android

package linux

import (
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Style.Match, as on macOS and Windows: when a view with a key is
// destroyed during an animation and another with the key is inserted,
// the new one grows from the old one's place and size, and the old one
// moves along to the new one's, fading. The views with keys are noted
// where they are as the animated batch starts destroying (in their "top":
// the outermost view their views are placed in by the host, a screen's
// or the window's root); the old view's widget moves into its top, where
// it was, on top, until the batch's frames are applied (when the frames
// batch ends its animation); then they're matched, their frames stepped
// with the animation's other tweens (anim.go). A view going with no
// match fades where it is.

type matchPlace struct {
	top *node
	r   [4]float32 // in top
}

type matchOutView struct {
	n *node
	matchPlace
}

var (
	matchKeys   = map[*node]string{}        // the views with keys
	matchIns    = map[*node]bool{}          // views given a key during an animation: maybe coming
	matchNoted  map[string]matchPlace       // where the keyed views were, as the batch started destroying
	matchOuts   = map[string]matchOutView{} // views going, waiting for their match
	matchQueued bool                        // the fallback fade is queued
)

func init() {
	commonHooks = append(commonHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if p != ops.PropMatch {
			return false
		}
		if v.Str == "" {
			delete(matchKeys, n)
			delete(matchIns, n)
			return true
		}
		matchKeys[n] = v.Str
		if h.animating {
			if len(matchIns) == 0 {
				// The batch's views that may be coming: matched as the
				// frames batch ends, forgotten once the core returns.
				h.afterCore(func() { clear(matchIns) })
			}
			matchIns[n] = true
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		delete(matchKeys, n)
		delete(matchIns, n)
	})
}

// matchTop is the outermost view n's view is placed in by the host,
// through views the host places (and scrolled content), and n's origin
// in it.
func (h *Host) matchTop(n *node) (top *node, x, y float32) {
	c := n
	for {
		p := c.parent
		switch {
		case p == nil || c.widget == 0:
			return c, x, y
		case p.scroll != nil && p.scroll.content == c:
			x += c.frame[0] - float32(AdjustmentGetValue(p.scroll.hadj))
			y += c.frame[1] - float32(AdjustmentGetValue(p.scroll.vadj))
		case WidgetGetParent(c.widget) == p.widget:
			f := c.frame
			if a := h.frameNow(c); a != nil {
				f = *a
			}
			x += f[0]
			y += f[1]
		default:
			return c, x, y
		}
		c = p
	}
}

// noteMatches notes where the views with keys are, as an animated batch
// starts destroying.
func (h *Host) noteMatches() {
	if matchNoted != nil {
		return
	}
	matchNoted = map[string]matchPlace{}
	for n, key := range matchKeys {
		if !n.placed || n.widget == 0 || WidgetGetParent(n.widget) == 0 || !WidgetGetMapped(n.widget) {
			continue
		}
		top, x, y := h.matchTop(n)
		if top == n {
			continue
		}
		f := n.frame
		if a := h.frameNow(n); a != nil {
			f = *a
		}
		matchNoted[key] = matchPlace{top: top, r: [4]float32{x, y, f[2], f[3]}}
	}
}

// matchOut keeps a view with a key destroyed during an animation in its
// top, where it was, until its match comes. It reports whether it did.
func (h *Host) matchOut(n *node) bool {
	if !h.animating || len(matchKeys) == 0 {
		return false
	}
	h.noteMatches() // the first destroy of the batch: the tree is still whole
	key := matchKeys[n]
	if key == "" {
		return false
	}
	delete(matchKeys, n)
	delete(matchIns, n)
	o, ok := matchNoted[key]
	if !ok || o.r[2] <= 0 || o.r[3] <= 0 || n.widget == 0 || o.top.widget == 0 || h.nodes[o.top.id] != o.top {
		return false
	}
	if old, ok := matchOuts[key]; ok {
		// Two going with one key: the first goes now.
		if t := h.tweens[old.n]; t != nil {
			delete(h.tweens, old.n)
			h.exited(old.n, t)
		}
	}
	// Into its top, on top of what's there, where it was.
	h.detach(n.widget)
	WidgetSetParent(n.widget, o.top.widget)
	n.frame = o.r
	t := h.tweenOf(n)
	t.exit, t.widget = true, n.widget
	t.from, t.to, t.frame = o.r, o.r, true
	Call(FnQueueAllocate, o.top.widget)
	matchOuts[key] = matchOutView{n: n, matchPlace: o}
	if !matchQueued {
		// If no frames come to match it (nothing was laid out), it fades
		// once the core has returned.
		matchQueued = true
		h.afterCore(func() {
			matchQueued = false
			for k, o := range matchOuts {
				delete(matchOuts, k)
				h.fadeOut(o.n)
			}
		})
	}
	return true
}

// runMatches matches the views going and coming, as an animation's batch
// ends: once the frames batch's has, the new views are placed.
func (h *Host) runMatches() {
	matchNoted = nil
	if len(matchOuts) == 0 {
		return
	}
	for in := range matchIns {
		key := matchKeys[in]
		o, ok := matchOuts[key]
		if !ok || !in.placed || in.widget == 0 || in.parent == nil {
			continue
		}
		top, x, y := h.matchTop(in)
		if top != o.top {
			continue
		}
		delete(matchOuts, key)
		delete(matchIns, in)
		// The new one, from the old one's place, in its parent.
		px, py := x-in.frame[0], y-in.frame[1]
		t := h.tweenOf(in)
		t.from = [4]float32{o.r[0] - px, o.r[1] - py, o.r[2], o.r[3]}
		t.to, t.frame = in.frame, true
		h.relayout(in)
		// The old one, to the new one's place, fading.
		ot := h.tweens[o.n]
		if ot == nil {
			continue
		}
		ot.a, ot.start = h.anim, time.Now()
		ot.from, ot.to, ot.frame = o.r, [4]float32{x, y, in.frame[2], in.frame[3]}, true
		ot.fromOp, ot.toOp, ot.op = float32(WidgetGetOpacity(o.n.widget)), 0, true
		h.startTicking()
	}
}

// fadeOut fades a view that went out without a match where it is, and
// lets it go.
func (h *Host) fadeOut(n *node) {
	t := h.tweens[n]
	if t == nil || n.widget == 0 {
		if t != nil {
			delete(h.tweens, n)
			h.exited(n, t)
		}
		return
	}
	t.start = time.Now()
	t.fromOp, t.toOp, t.op = float32(WidgetGetOpacity(n.widget)), 0, true
	h.startTicking()
}
