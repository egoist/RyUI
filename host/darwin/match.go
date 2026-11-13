//go:build darwin && !ios

package darwin

import (
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// Style.Match, as on iOS: when a view with a key is destroyed during an
// animation and another with the key is inserted, the new one grows from
// the old one's place and size, and the old one moves along to the new
// one's, fading. Where the views with keys are is noted as the animated
// batch starts destroying; the old view goes into its window's content
// view, where it was, until the batch's frames are applied; then they're
// matched. A view going with no match fades.
//
// AppKit's views take no transform that holds (AppKit sets their layers'
// own), so the new view's frame carries the motion, as a transition's
// does: its subviews keep their places in it.

type matchOut struct {
	view    ID
	frame   Rect // in its window's content view
	content ID
}

type matching struct {
	count    int             // nodes with a key
	frames   map[string]Rect // where they were as the batch began destroying (content coordinates)
	contents map[string]ID   // each one's window's content view
	outs     map[string]matchOut
	ins      []*node
	duration float64
}

// setMatch sets n's key.
func (h *Host) setMatch(n *node, key string) {
	if n.match != "" {
		h.match.count--
	}
	n.match = key
	if key != "" {
		h.match.count++
	}
}

// matchOut takes a view with a key destroyed during an animation into
// its window, where it was, until its match comes. True if it did.
func (h *Host) matchOut(n *node) bool {
	m := &h.match
	if !h.animating || m.count == 0 && n.match == "" {
		if n.match != "" {
			m.count--
			n.match = ""
		}
		return false
	}
	if m.frames == nil {
		// Where every placed view with a key is, before the batch takes any
		// away (one may go with an ancestor, destroyed first).
		m.frames, m.contents = map[string]Rect{}, map[string]ID{}
		note := func(x *node) {
			if x.match == "" || !x.placed || x.view == 0 {
				return
			}
			w := Send(x.view, "window")
			if w == 0 {
				return
			}
			content := Send(w, "contentView")
			m.frames[x.match] = MsgRectIDToRect(x.view, Sel("convertRect:toView:"), MsgRect(x.view, Sel("bounds")), content)
			m.contents[x.match] = content
		}
		note(n) // already gone from the nodes
		for _, x := range h.nodes {
			note(x)
		}
	}
	key := n.match
	if key == "" {
		return false
	}
	m.count--
	n.match = ""
	f, ok := m.frames[key]
	if !ok || n.view == 0 || f.Size.Width <= 0 || f.Size.Height <= 0 {
		return false
	}
	if old, ok := m.outs[key]; ok {
		h.fadeOut(old.view, 0) // two going with one key: the last waits
	}
	content := m.contents[key]
	v := n.view
	Retain(v)
	withoutAnimation(func() {
		Send(v, "removeFromSuperview")
		MsgSetRect(v, selSetFrame, f)
		Send(content, "addSubview:", uintptr(v))
	})
	h.exiting[v] = true // forget leaves it to the match
	if m.outs == nil {
		m.outs = map[string]matchOut{}
	}
	m.outs[key] = matchOut{view: v, frame: f, content: content}
	m.duration = h.anim.duration
	// If no frames ever come, it goes anyway.
	time.AfterFunc(time.Duration((m.duration+1)*float64(time.Second)), func() {
		h.Post(func() {
			if o, ok := h.match.outs[key]; ok && o.view == v {
				delete(h.match.outs, key)
				h.fadeOut(v, 0)
			}
		})
	})
	return true
}

// matchIn notes a view inserted during an animation: it, or a view in it,
// may be one an old one's going to.
func (h *Host) matchIn(n *node) {
	if h.animating && (h.match.count > 0 || len(h.match.outs) > 0) {
		h.match.ins = append(h.match.ins, n)
	}
}

// batchApplied runs the matches once a batch's frames are applied; a
// props batch only ends what the destroying noted.
func (h *Host) batchApplied(frames bool) {
	m := &h.match
	m.frames, m.contents = nil, nil
	if !frames {
		return
	}
	ins := m.ins
	m.ins = nil
	if len(m.outs) == 0 {
		return
	}
	var coming []*node
	var collect func(n *node)
	collect = func(n *node) {
		if _, ok := m.outs[n.match]; ok && n.match != "" {
			coming = append(coming, n)
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	for _, n := range ins {
		collect(n)
	}
	d := m.duration
	for _, n := range coming {
		out, ok := m.outs[n.match]
		if !ok || n.view == 0 || Send(n.view, "window") == 0 || Send(Send(n.view, "window"), "contentView") != out.content {
			continue
		}
		delete(m.outs, n.match)
		v, sup := n.view, Send(n.view, "superview")
		if sup == 0 {
			h.fadeOut(out.view, d)
			continue
		}
		final := MsgRect(v, Sel("frame"))
		to := MsgRectIDToRect(v, Sel("convertRect:toView:"), MsgRect(v, Sel("bounds")), out.content)
		if to.Size.Width <= 0 || to.Size.Height <= 0 {
			h.fadeOut(out.view, d)
			continue
		}
		from := MsgRectIDToRect(sup, Sel("convertRect:fromView:"), out.frame, out.content)
		withoutAnimation(func() { MsgSetRect(v, selSetFrame, from) })
		old := out.view
		animate(d, func() {
			MsgSetRect(Send(v, "animator"), selSetFrame, final)
			a := Send(old, "animator")
			MsgSetRect(a, selSetFrame, to)
			MsgSetFloat(a, Sel("setAlphaValue:"), 0)
		}, func() { h.dropOut(old) })
	}
	for k, out := range m.outs {
		delete(m.outs, k)
		h.fadeOut(out.view, d)
	}
}

// fadeOut fades an old view out and lets it go.
func (h *Host) fadeOut(v ID, d float64) {
	if d <= 0 {
		h.dropOut(v)
		return
	}
	animate(d, func() { MsgSetFloat(Send(v, "animator"), Sel("setAlphaValue:"), 0) }, func() { h.dropOut(v) })
}

func (h *Host) dropOut(v ID) {
	if !h.exiting[v] {
		return
	}
	delete(h.exiting, v)
	Send(v, "removeFromSuperview")
	Release(v) // forget's
	Release(v) // matchOut's
}

// animate runs f in an animation group of d seconds, then done.
func animate(d float64, f, done func()) {
	var anim, end objc.Block
	anim = NewBlock(func(_ objc.Block, ctx ID) {
		MsgSetFloat(ctx, Sel("setDuration:"), d)
		Send(ctx, "setAllowsImplicitAnimation:", 1)
		f()
	})
	end = NewBlock(func(_ objc.Block) {
		done()
		anim.Release()
		end.Release()
	})
	Send(Class("NSAnimationContext"), "runAnimationGroup:completionHandler:", uintptr(anim), uintptr(end))
}
