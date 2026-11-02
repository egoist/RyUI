package signal

import "sync/atomic"

// Ambient state is process-wide state that any goroutine may read but
// that effects follow as they follow a signal: the language the app
// shows, whose messages a goroutine may say as readily as an effect.
//
// A signal can't serve: a read subscribes the running computation through
// activeSub, which only the main thread may touch. So a read only counts,
// atomically, and the main thread settles the count whenever the running
// computation changes, subscribing the one that was running to the
// ambient signal. A read from a goroutine counts too, and subscribes
// whatever ran on the main thread at that moment: that computation runs
// once more when the state changes, which is harmless and rare.

var (
	ambientReads atomic.Uint64
	ambientSeen  uint64 // the reads settled so far; main thread
	ambient      = New[uint64](0)
)

// Ambient records a read of ambient state: the effect or memo running on
// the main thread, if any, runs again at the next AmbientChanged. Safe
// from any goroutine, and it allocates nothing.
func Ambient() { ambientReads.Add(1) }

// AmbientChanged runs the effects and memos that read ambient state (see
// Ambient) again, as a signal's write does. Main thread.
func AmbientChanged() {
	settleAmbient()
	ambient.Set(ambient.Peek() + 1)
}

// settleAmbient subscribes the running computation to the ambient signal
// if ambient state was read since the last call. Whatever changes
// activeSub calls it first.
func settleAmbient() {
	if c := ambientReads.Load(); c != ambientSeen {
		ambientSeen = c
		if activeSub != nil {
			track(&ambient.n, activeSub)
		}
	}
}
