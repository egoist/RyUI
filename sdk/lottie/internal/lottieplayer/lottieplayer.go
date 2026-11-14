// Package lottieplayer is asuka/sdk/lottie's view on the platforms with a
// Lottie player of their own (asuka native): lottie-ios on iOS and the Mac
// (swift/), lottie-android on Android (android/), lottie-web on the web
// (web/). Package lottie drives it; on Windows and Linux it draws the
// animation itself (lottie/internal/render) and this view isn't used.
//
// Each platform's view does what these props say, in whatever order they
// come:
//
//   - Source (a URL, an asset's path or a file's) or Data (the file's
//     bytes): a Lottie JSON or a dotLottie, and Animation, one of a
//     dotLottie's by ID. Once it has loaded, Loaded; if it can't, Failed.
//   - Playing plays it from where it is (from the range's start if it's
//     at its end), and false pauses it there. Loop plays the range again
//     and again; else Completed when it reaches the range's end. Speed
//     scales its frame rate (negative: backwards).
//   - Segment (frames; To <= From: the whole) or Marker (a marker's
//     frames, by name; wins over Segment) is the range it plays.
//   - Seek moves it to Progress (0 to 1, of the whole animation; To: the
//     range's start or end instead) when its Serial changes, over
//     Duration seconds on Curve (ui.Curve's values) if not 0; playing
//     goes on from there.
//   - Report sends Progress at each frame it shows (and after a seek).
//   - Overrides change what keypaths name (Kind: 0 the color, 1 the
//     opacity, 2 the stroke width).
//   - Mode is ui.ImageMode's: how it fills the view.
package lottieplayer

import (
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// FrameRange is a range of frames.
type FrameRange struct {
	From, To float64
}

// SeekTo moves the animation (see the package's doc).
type SeekTo struct {
	Progress float64
	To       int // 0: Progress; 1: the range's start; 2: its end
	Duration float64
	Curve    int
	Damping  float64
	Serial   int
}

// KeypathOverride is a keypath's override.
type KeypathOverride struct {
	Keypath    string
	Kind       int
	R, G, B, A float64 // 0 to 1
	Value      float64
}

// MarkerInfo is a named range of frames.
type MarkerInfo struct {
	Name            string
	Start, Duration float64
}

// AnimationInfo is an animation that loaded.
type AnimationInfo struct {
	Width, Height float64 // its size, one point per unit
	Start, End    float64 // its frames: the first, and the one after its last
	FrameRate     float64
	Markers       []MarkerInfo
}

// PlayerProps are the player's.
//
//asuka:view
type PlayerProps struct {
	Source    signal.Accessor[string]
	Data      []byte
	Animation string
	Mode      int
	Loop      bool
	Report    bool
	Playing   signal.Accessor[bool]
	Speed     signal.Accessor[float64]
	Segment   signal.Accessor[FrameRange]
	Marker    signal.Accessor[string]
	Seek      signal.Accessor[SeekTo]
	Overrides signal.Accessor[[]KeypathOverride]
	Loaded    func(info AnimationInfo)
	Progress  func(progress float64)
	Completed func()
	Failed    func(message string)
	Style     ui.Styler
}
