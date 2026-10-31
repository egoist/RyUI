// Package signal is RyUI's fine-grained reactivity: signals, memos and
// effects, with Solid's ownership model.
//
// Writes push dirty flags through the graph; reads pull values. A memo
// recomputes only when one of its sources really changed, and an effect
// runs at most once per flush, so updates are glitch-free (diamonds see
// one consistent state) and do no unnecessary work.
//
// Everything here runs on one thread (the platform's main thread). It is
// not safe for concurrent use; goroutines reach the main thread with
// ui.Post.
package signal

// flags is the state of a node in the graph.
type flags uint16

const (
	fMemo     flags = 1 << iota // a memo: has deps and subs
	fEffect                     // an effect: has deps, no subs
	fRender                     // an effect that ui makes for props; runs first
	fDirty                      // a dep changed: must recompute
	fPending                    // a dep might have changed: check deps first
	fQueued                     // effect is in the queue
	fDisposed                   // owner disposed; never runs again
)

// link is an edge from dep to sub. It sits in two doubly linked lists:
// dep's subscribers and sub's dependencies. Links are reused across runs
// when a computation reads the same deps in the same order, so a steady
// state re-run allocates nothing.
type link struct {
	dep, sub         *node
	prevSub, nextSub *link
	prevDep, nextDep *link
	version          uint32 // sub.run when this link was last confirmed
}

// node is the part of a signal, memo or effect that takes part in the
// graph.
type node struct {
	deps, depsTail *link
	subs, subsTail *link
	flags          flags
	run            uint32      // increments each time the node starts tracking
	seq            uint64      // creation order, for effects
	comp           computation // nil for signals
}

// computation is a memo or an effect.
type computation interface {
	// update recomputes. A memo reports whether its value changed.
	update() bool
	// unwatched is called when the last subscriber goes away.
	unwatched()
}

var (
	activeSub  *node // computation tracking reads, nil when untracked
	inMemo     int   // >0 while a memo computes
	batchDepth int
	flushing   bool
	freeLinks  *link // free list, chained through nextDep
	seqCounter uint64
)

func newLink() *link {
	if l := freeLinks; l != nil {
		freeLinks = l.nextDep
		l.nextDep = nil
		return l
	}
	return &link{}
}

func freeLink(l *link) {
	*l = link{nextDep: freeLinks}
	freeLinks = l
}

// track records that sub read dep.
func track(dep, sub *node) {
	prevDep := sub.depsTail
	if prevDep != nil && prevDep.dep == dep {
		return
	}
	var nextDep *link
	if prevDep != nil {
		nextDep = prevDep.nextDep
	} else {
		nextDep = sub.deps
	}
	if nextDep != nil && nextDep.dep == dep {
		// Same dep as last run, in the same place: reuse the link.
		nextDep.version = sub.run
		sub.depsTail = nextDep
		return
	}
	prevSub := dep.subsTail
	if prevSub != nil && prevSub.sub == sub && prevSub.version == sub.run {
		return // already read during this run
	}
	l := newLink()
	l.dep, l.sub, l.version = dep, sub, sub.run
	l.prevDep, l.nextDep = prevDep, nextDep
	l.prevSub, l.nextSub = prevSub, nil
	if nextDep != nil {
		nextDep.prevDep = l
	}
	if prevDep != nil {
		prevDep.nextDep = l
	} else {
		sub.deps = l
	}
	sub.depsTail = l
	if prevSub != nil {
		prevSub.nextSub = l
	} else {
		dep.subs = l
	}
	dep.subsTail = l
}

// unlink removes l from both lists and returns the next dep of its sub.
func unlink(l *link) *link {
	dep, sub := l.dep, l.sub
	next := l.nextDep
	if next != nil {
		next.prevDep = l.prevDep
	} else {
		sub.depsTail = l.prevDep
	}
	if l.prevDep != nil {
		l.prevDep.nextDep = next
	} else {
		sub.deps = next
	}
	if l.nextSub != nil {
		l.nextSub.prevSub = l.prevSub
	} else {
		dep.subsTail = l.prevSub
	}
	if l.prevSub != nil {
		l.prevSub.nextSub = l.nextSub
	} else {
		dep.subs = l.nextSub
	}
	freeLink(l)
	if dep.subs == nil && dep.comp != nil {
		dep.comp.unwatched()
	}
	return next
}

func startTracking(n *node) {
	n.run++
	n.depsTail = nil
	n.flags &^= fDirty | fPending
}

// endTracking drops the deps that the run didn't read.
func endTracking(n *node) {
	var l *link
	if n.depsTail != nil {
		l = n.depsTail.nextDep
	} else {
		l = n.deps
	}
	for l != nil {
		l = unlink(l)
	}
}

func unlinkAllDeps(n *node) {
	for l := n.deps; l != nil; {
		l = unlink(l)
	}
}

// propagate marks the subscribers in l with flag (fDirty for direct
// subscribers of a written signal, fPending further down) and queues
// effects.
func propagate(l *link, flag flags) {
	for ; l != nil; l = l.nextSub {
		sub := l.sub
		f := sub.flags
		if f&fDisposed != 0 || f&fDirty != 0 || (flag == fPending && f&fPending != 0) {
			continue
		}
		sub.flags = f | flag
		if f&fEffect != 0 {
			enqueue(sub)
		} else if f&(fDirty|fPending) == 0 && sub.subs != nil {
			propagate(sub.subs, fPending)
		}
	}
}

// shallowPropagate turns pending subscribers of a memo that changed into
// dirty ones.
func shallowPropagate(l *link) {
	for ; l != nil; l = l.nextSub {
		sub := l.sub
		if sub.flags&(fPending|fDirty) == fPending {
			sub.flags |= fDirty
		}
	}
}

// checkDirty reports whether a pending node really must recompute:
// whether any memo it depends on changed value. It brings those memos up
// to date on the way.
func checkDirty(n *node) bool {
	for l := n.deps; l != nil; l = l.nextDep {
		dep := l.dep
		f := dep.flags
		if f&fMemo == 0 {
			continue
		}
		if f&fDirty != 0 || (f&fPending != 0 && checkDirty(dep)) {
			if dep.comp.update() {
				if dep.subs != nil {
					shallowPropagate(dep.subs)
				}
				if n.flags&fDirty != 0 {
					return true
				}
			}
		} else if f&fPending != 0 {
			dep.flags &^= fPending
		}
	}
	return n.flags&fDirty != 0
}

func nextSeq() uint64 {
	seqCounter++
	return seqCounter
}
