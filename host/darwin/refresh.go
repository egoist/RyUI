//go:build darwin && !ios

package darwin

import (
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// Pull to refresh (OnRefresh) on a Mac: a trackpad's or a Magic Mouse's
// scroll pulls a Scroll or List past its top, as its elastic overscroll
// lets it, and let go of beyond refreshPull points it refreshes, as on
// iOS: a spinner shows above the content, which moves down to make room,
// until Refreshing goes false. The scroll views' live scroll notifications
// say how far it went (to an observer of its own, as a paging Scroll's
// observes the end of live scrolls too).

const (
	refreshPull   = 60 // points past the top that refresh
	refreshHeight = 40 // the room the spinner takes
)

type refresher struct {
	pulled     float64 // the farthest past the top in this live scroll
	refreshing bool
	spinner    ID
}

// refreshObserver is the observer of refreshable scroll views' live
// scrolls: a AsukaTarget of its own.
func (h *Host) refreshObserver() ID {
	if h.refreshTarget == 0 {
		h.refreshTarget = New("AsukaTarget")
	}
	return h.refreshTarget
}

// setRefreshable turns pulling to refresh on or off for n's scroll view.
func (h *Host) setRefreshable(n *node, on bool) {
	if (n.refresh != nil) == on {
		return
	}
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	obs := h.refreshObserver()
	if !on {
		Send(center, "removeObserver:name:object:", uintptr(obs), 0, uintptr(n.view))
		h.setRefreshing(n, false)
		n.refresh = nil
		return
	}
	n.refresh = &refresher{}
	Send(n.view, "setAutomaticallyAdjustsContentInsets:", 0)
	Send(center, "addObserver:selector:name:object:", uintptr(obs), uintptr(Sel("refreshScrolled:")),
		uintptr(String("NSScrollViewDidLiveScrollNotification")), uintptr(n.view))
	Send(center, "addObserver:selector:name:object:", uintptr(obs), uintptr(Sel("refreshScrollEnded:")),
		uintptr(String("NSScrollViewDidEndLiveScrollNotification")), uintptr(n.view))
}

// overTopOf is overTop; tests pull further than a clip view lets them
// (it constrains its bounds outside a live scroll's rubber band).
var overTopOf = overTop

// overTop is how far past its top a scroll view's content is pulled.
func overTop(sv ID) float64 {
	clip := Send(sv, "contentView")
	y := MsgRect(clip, Sel("bounds")).Origin.Y
	if !SendBool(clip, "isFlipped") {
		// Unflipped, the top is the document's height less the clip's.
		doc := Send(sv, "documentView")
		top := MsgRect(doc, Sel("frame")).Size.Height - MsgRect(clip, Sel("bounds")).Size.Height
		return y - top
	}
	inset := 0.0
	if r := theHost.byView[sv]; r != nil && r.refresh != nil && r.refresh.refreshing {
		inset = refreshHeight
	}
	return -(y + inset)
}

// refreshScrolled follows a live scroll past the top.
func (h *Host) refreshScrolled(sv ID) {
	n := h.byView[sv]
	if n == nil || n.refresh == nil {
		return
	}
	if p := overTopOf(sv); p > n.refresh.pulled {
		n.refresh.pulled = p
	}
}

// refreshScrollEnded refreshes if the live scroll that ended went far
// enough past the top.
func (h *Host) refreshScrollEnded(sv ID) {
	n := h.byView[sv]
	if n == nil || n.refresh == nil {
		return
	}
	pulled := n.refresh.pulled
	n.refresh.pulled = 0
	if pulled < refreshPull || n.refresh.refreshing || !n.listening(ops.EventRefresh) {
		return
	}
	h.setRefreshing(n, true) // the core's handler says when it's done
	h.emit(n, ops.Event{Kind: ops.EventRefresh})
}

// setRefreshing shows the spinner above the content, or takes it away.
func (h *Host) setRefreshing(n *node, on bool) {
	r := n.refresh
	if r == nil || r.refreshing == on {
		return
	}
	r.refreshing = on
	sv := n.view
	if on {
		if r.spinner == 0 {
			r.spinner = initView("NSProgressIndicator")
			Send(r.spinner, "setStyle:", 1) // spinning
			Send(r.spinner, "setIndeterminate:", 1)
			Send(r.spinner, "setControlSize:", 1) // small
			Send(r.spinner, "setDisplayedWhenStopped:", 0)
		}
		w := MsgRect(sv, Sel("bounds")).Size.Width
		MsgSetRect(r.spinner, selSetFrame, Rect{Origin: Point{X: (w - 16) / 2, Y: (refreshHeight - 16) / 2}, Size: Size{Width: 16, Height: 16}})
		Send(r.spinner, "setAutoresizingMask:", 1|4) // its margins stretch: it stays centered
		Send(sv, "addSubview:", uintptr(r.spinner))
		Send(r.spinner, "startAnimation:", 0)
		MsgSetEdges(sv, Sel("setContentInsets:"), EdgeInsets{Top: refreshHeight})
		return
	}
	if r.spinner != 0 {
		Send(r.spinner, "stopAnimation:", 0)
		Send(r.spinner, "removeFromSuperview")
	}
	MsgSetEdges(sv, Sel("setContentInsets:"), EdgeInsets{})
}

// releaseRefresh lets go of what pulling to refresh holds.
func (h *Host) releaseRefresh(n *node) {
	if n.refresh == nil {
		return
	}
	Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "removeObserver:name:object:", uintptr(h.refreshObserver()), 0, uintptr(n.view))
	if n.refresh.spinner != 0 {
		Release(n.refresh.spinner)
	}
	n.refresh = nil
}
