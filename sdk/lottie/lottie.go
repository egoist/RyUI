// Package lottie plays Lottie animations (lottie-react-native's, in
// Expo): After Effects animations exported as Lottie JSON (.json) or
// dotLottie (.lottie) files, drawn by each platform's own Lottie player —
// lottie-ios on iOS and the Mac, lottie-android on Android, lottie-web on
// the web — and on Windows and Linux, which have none to call, by this
// package's own renderer, drawing through ui.Canvas.
//
//	progress := signal.New(0.0)
//	lottie.View(lottie.Props{
//		Source:   ui.Val("success.json"),
//		Progress: progress,
//		Style:    ui.Style{Width: ui.Pt(160)},
//	})
//
// An app that imports this package is built with its platforms' players
// (a Swift package, a Gradle library, a script: asuka native); the others
// go without. Under asuka dev on a phone, an app with it runs as a build
// of its own rather than in the player.
package lottie

import (
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/lottie/internal/lottieplayer"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Props are a View's.
type Props struct {
	// Source is the animation's file: an http(s) URL, a file in the app's
	// assets directory, or an absolute path; a Lottie JSON file or a
	// dotLottie. A new Source loads in its place.
	Source signal.Accessor[string]
	// Data is the animation's file itself instead (go:embed): Lottie JSON
	// or a dotLottie's bytes.
	Data []byte
	// Animation is the ID of the dotLottie's animation to play; "": its
	// manifest's active one, else its first.
	Animation string

	// Playing plays it while true and pauses it where it is while false.
	// nil: it plays as soon as it loads (autoplay).
	Playing signal.Accessor[bool]
	// Loop plays it again and again; else it stops at its end, and
	// OnComplete is called.
	Loop bool
	// Speed scales how fast it plays: 2 twice as fast, -1 backwards. nil: 1.
	Speed signal.Accessor[float64]
	// Progress is where it is, from 0 (its first frame) to 1 (its last):
	// the view writes it as it plays (each frame it shows), and a value
	// the app writes moves it there (scrubbing, a gesture driving it).
	// Written inside ui.Animate, it animates there, with the animation's
	// duration and curve; then it plays on from there if it's playing.
	Progress *signal.Signal[float64]
	// Segment is the frames it plays, From to To; the zero Segment: all
	// of them.
	Segment signal.Accessor[Segment]
	// Marker is the name of one of its markers: it plays that marker's
	// frames (instead of Segment). "": none.
	Marker signal.Accessor[string]

	// Mode is how it fills its box, as an Image's: ui.ImageFit (the
	// default), ImageFill, ImageStretch, ImageCenter.
	Mode ui.ImageMode
	// Overrides change its colors and values by keypath, for a theme or
	// dark mode: Color, Colors, Opacity, StrokeWidth.
	Overrides signal.Accessor[[]Override]
	// ReduceMotion is what it does while the user asks for less motion
	// (Reduce Motion; Android's animations off; prefers-reduced-motion):
	// by default it shows its last frame instead of playing.
	ReduceMotion ReduceMotion

	// Label describes it to screen readers; without it the animation is
	// decorative.
	Label signal.Accessor[string]
	// Style sizes it: without a width or a height, it's as big as the
	// animation says (one point per unit), no wider than its space; with
	// one of them, the other keeps the animation's aspect ratio. Until it
	// has loaded, it has no size of its own.
	Style ui.Styler

	// OnLoad gets what the animation is once it has loaded.
	OnLoad func(Info)
	// OnComplete is called when it has played to its end (not looping).
	OnComplete func()
	// OnError gets why it can't load or play.
	OnError func(message string)
}

// Segment is a range of an animation's frames.
type Segment struct {
	From, To float64
}

// Info is what an animation is.
type Info struct {
	// Width and Height are its size, in points.
	Width, Height float64
	// Start and End are its frames: its first, and the one after its last.
	Start, End float64
	FrameRate  float64
	Markers    []Marker
}

// Frames is how many frames it has.
func (i Info) Frames() float64 { return i.End - i.Start }

// Seconds is how long it plays once, at speed 1.
func (i Info) Seconds() float64 {
	if i.FrameRate <= 0 {
		return 0
	}
	return i.Frames() / i.FrameRate
}

// Marker is a named range of an animation's frames.
type Marker struct {
	Name            string
	Start, Duration float64 // frames
}

// ReduceMotion is what an animation does while the user asks for less
// motion.
type ReduceMotion uint8

const (
	ReduceToEnd   ReduceMotion = iota // it shows the last frame of what it plays (the default)
	ReduceToStart                     // its first frame
	ReduceIgnore                      // it plays as usual: its motion is what it says (a progress indicator)
)

// Override changes what a keypath names in an animation. A keypath is
// the names of layers, groups and shapes from its root, separated by
// dots, with * standing for any one name and ** for any number of them,
// as lottie-ios' and lottie-android's: "Circle.Disc.Fill 1", "**.Stroke 1",
// "Ring". Colors are RGB (colors.RGB, colors.Hex); a semantic color isn't
// one an animation can take, and leaves it as it is.
type Override struct {
	keypath     string
	kind        int
	light, dark ui.Color
	value       float64
}

// Color colors the fills and strokes the keypath names, and those of
// the layers and groups under it.
func Color(keypath string, c ui.Color) Override {
	return Override{keypath: keypath, light: c, dark: c}
}

// Colors is Color in light mode and dark in dark mode.
func Colors(keypath string, light, dark ui.Color) Override {
	return Override{keypath: keypath, light: light, dark: dark}
}

// Opacity sets the opacity (0 to 1) of the layer, group or paint the
// keypath names.
func Opacity(keypath string, v float64) Override {
	return Override{keypath: keypath, kind: 1, value: v}
}

// StrokeWidth sets the width of the strokes the keypath names, and those
// under it.
func StrokeWidth(keypath string, w float64) Override {
	return Override{keypath: keypath, kind: 2, value: w}
}

// View shows a Lottie animation.
func View(p Props) ui.Node {
	env := ui.UseEnvironment()
	reduced := signal.NewMemo(func() bool { return p.ReduceMotion != ReduceIgnore && env.Get().ReduceMotion })
	natural := signal.New([2]float64{})
	pp := lottieplayer.PlayerProps{
		Data:      p.Data,
		Animation: p.Animation,
		Mode:      int(p.Mode),
		Loop:      p.Loop,
		Report:    p.Progress != nil,
		Speed:     p.Speed,
	}
	if p.Source != nil {
		pp.Source = signal.Func(func() string { return ui.AssetURL(p.Source.Get()) })
	}
	pp.Playing = signal.Func(func() bool {
		if reduced.Get() {
			return false
		}
		return p.Playing == nil || p.Playing.Get()
	})
	if p.Segment != nil {
		pp.Segment = signal.Func(func() lottieplayer.FrameRange {
			s := p.Segment.Get()
			return lottieplayer.FrameRange{From: s.From, To: s.To}
		})
	}
	pp.Marker = p.Marker

	// Seeks: the app's writes of Progress (the view's own reports aside),
	// and Reduce Motion's still frame.
	seek := signal.New(lottieplayer.SeekTo{})
	serial := 0
	reported, haveReport := 0.0, false
	if p.Progress != nil {
		signal.RenderEffect(func() {
			v := p.Progress.Get()
			if haveReport && v == reported {
				return
			}
			serial++
			s := lottieplayer.SeekTo{Progress: v, Serial: serial}
			if a, ok := ui.Animating(); ok {
				s.Duration, s.Curve, s.Damping = float64(a.Duration), int(a.Curve), float64(a.Damping)
				if s.Damping <= 0 {
					s.Damping = 0.85
				}
			}
			seek.Set(s)
		})
	}
	signal.RenderEffect(func() {
		if !reduced.Get() {
			return
		}
		to := 2
		if p.ReduceMotion == ReduceToStart {
			to = 1
		}
		serial++
		seek.Set(lottieplayer.SeekTo{To: to, Serial: serial})
	})
	pp.Seek = seek
	pp.Progress = func(v float64) {
		reported, haveReport = v, true
		if p.Progress != nil {
			p.Progress.Set(v)
		}
	}
	pp.Loaded = func(i lottieplayer.AnimationInfo) {
		natural.Set([2]float64{i.Width, i.Height})
		if p.OnLoad != nil {
			info := Info{Width: i.Width, Height: i.Height, Start: i.Start, End: i.End, FrameRate: i.FrameRate}
			for _, m := range i.Markers {
				info.Markers = append(info.Markers, Marker(m))
			}
			p.OnLoad(info)
		}
	}
	pp.Completed = p.OnComplete
	pp.Failed = p.OnError
	if p.Overrides != nil {
		pp.Overrides = signal.Func(func() []lottieplayer.KeypathOverride {
			return wireOverrides(p.Overrides.Get(), env.Get().Dark)
		})
	}
	sized := ui.StyleFunc(func() ui.Style { return sizedStyle(ui.StyleValue(p.Style), natural.Get()) })
	if !hasLabel(p.Label) {
		pp.Style = sized
		return player(pp)
	}
	// A screen reader reads it as an image: a view of its own, around it.
	pp.Style = ui.Style{Grow: 1, AlignSelf: ui.AlignStretch}
	return ui.View(ui.ViewProps{Style: sized, Label: p.Label, Role: ui.RoleImage}, player(pp))
}

func hasLabel(a signal.Accessor[string]) bool {
	if a == nil {
		return false
	}
	if v, ok := signal.StaticValue(a); ok {
		return v != ""
	}
	return true
}

// sizedStyle is the app's style, sized as an Image is by the animation's
// natural size n: as wide as it, no wider than its space, its aspect
// ratio kept; one side given, the other from the ratio.
func sizedStyle(s ui.Style, n [2]float64) ui.Style {
	if n[0] <= 0 || n[1] <= 0 {
		return s
	}
	set := func(d ui.Dim) bool { return d != (ui.Dim{}) }
	w, h := set(s.Width), set(s.Height)
	if w && h {
		return s
	}
	if s.AspectRatio == 0 {
		s.AspectRatio = float32(n[0] / n[1])
	}
	if !w && !h && s.Grow == 0 {
		s.Width = ui.Pt(float32(n[0]))
		if !set(s.MaxWidth) {
			s.MaxWidth = ui.Pct(100)
		}
	}
	return s
}

// wireOverrides are overrides as the players take them, their colors
// for the appearance.
func wireOverrides(os []Override, dark bool) []lottieplayer.KeypathOverride {
	out := make([]lottieplayer.KeypathOverride, 0, len(os))
	for _, o := range os {
		w := lottieplayer.KeypathOverride{Keypath: o.keypath, Kind: o.kind, Value: o.value}
		if o.kind == 0 {
			c := o.light
			if dark && o.dark != 0 {
				c = o.dark
			}
			r, g, b, a, ok := ops.Color(c).RGBA()
			if !ok {
				continue // semantic: no color to give
			}
			w.R, w.G, w.B, w.A = float64(r)/255, float64(g)/255, float64(b)/255, float64(a)/255
		}
		out = append(out, w)
	}
	return out
}
