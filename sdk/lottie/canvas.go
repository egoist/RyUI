package lottie

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/lottie/internal/lottieplayer"
	"github.com/egoist/asuka/sdk/lottie/internal/render"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// canvasPlayer is the player where the platform has none (Windows,
// Linux): the animation read and drawn by package render into a
// ui.Canvas, its frames timed by ui.OnFrame while it plays. It does what
// lottieplayer's props say, as the platforms' players do.
func canvasPlayer(p lottieplayer.PlayerProps) ui.Node {
	c := &canvas{p: p, speed: 1}
	c.frame = signal.New(0.0)
	c.anim = signal.New[*render.Animation](nil)
	var stop func()
	owner := signal.CurrentOwner()
	c.wake = func() {
		if stop != nil || !c.moving() {
			return
		}
		c.last = -1
		signal.RunWithOwner(owner, func() {
			stop = ui.OnFrame(func(t time.Duration) {
				c.tick(t)
				if !c.moving() {
					stop()
					stop = nil
				}
			})
		})
	}
	c.bind()
	return ui.Canvas(func(g *ui.Graphics) {
		a := c.anim.Get()
		f := c.frame.Get()
		if a == nil {
			return
		}
		w, h := g.Size()
		c.r.A = a
		c.r.Draw(g, math.Min(f, a.Out-0.001), float64(w), float64(h), ops.ImageMode(p.Mode))
	}, p.Style)
}

type canvas struct {
	p     lottieplayer.PlayerProps
	anim  *signal.Signal[*render.Animation]
	frame *signal.Signal[float64] // what's drawn
	r     render.Renderer

	playing bool
	speed   float64
	seg     lottieplayer.FrameRange
	marker  string
	serial  int
	pending *lottieplayer.SeekTo // a seek before the animation loaded
	tween   *tween
	last    time.Duration // the last frame's time; -1: none yet
	wake    func()
	done    bool // it reached its range's end, not looping
}

type tween struct {
	from, to float64 // frames
	start    time.Duration
	seconds  float64
	curve    ui.Curve
	damping  float64
	begun    bool
}

func (c *canvas) moving() bool {
	return c.anim.Peek() != nil && (c.tween != nil || c.playing && !c.done)
}

// span is the range it plays, in frames.
func (c *canvas) span() (from, to float64) {
	a := c.anim.Peek()
	if a == nil {
		return 0, 0
	}
	from, to = a.In, a.Out
	if c.marker != "" {
		for _, m := range a.Markers {
			if m.Name == c.marker {
				return m.Start, m.Start + m.Duration
			}
		}
	}
	if c.seg.To > c.seg.From {
		from, to = math.Max(c.seg.From, a.In), math.Min(c.seg.To, a.Out)
	}
	return from, to
}

func (c *canvas) bind() {
	p := c.p
	if p.Source != nil {
		signal.RenderEffect(func() {
			src := p.Source.Get()
			if src != "" {
				c.load(src, nil)
			}
		})
	} else if len(p.Data) > 0 {
		c.load("", p.Data)
	}
	if p.Playing != nil {
		signal.RenderEffect(func() {
			v := p.Playing.Get()
			signal.Untrack(func() int { c.setPlaying(v); return 0 })
		})
	}
	if p.Speed != nil {
		signal.RenderEffect(func() {
			if v := p.Speed.Get(); v != 0 {
				c.speed = v
			} else {
				c.speed = 1
			}
		})
	}
	if p.Segment != nil {
		signal.RenderEffect(func() {
			c.seg = p.Segment.Get()
			signal.Untrack(func() int { c.rangeChanged(); return 0 })
		})
	}
	if p.Marker != nil {
		signal.RenderEffect(func() {
			c.marker = p.Marker.Get()
			signal.Untrack(func() int { c.rangeChanged(); return 0 })
		})
	}
	if p.Seek != nil {
		signal.RenderEffect(func() {
			s := p.Seek.Get()
			signal.Untrack(func() int { c.seek(s); return 0 })
		})
	}
	if p.Overrides != nil {
		signal.RenderEffect(func() {
			var os []render.Override
			for _, o := range p.Overrides.Get() {
				os = append(os, render.Override{Keypath: render.ParseKeypath(o.Keypath), Kind: render.OverrideKind(o.Kind),
					Color: [4]float64{o.R, o.G, o.B, o.A}, Value: o.Value})
			}
			c.r.Overrides = os
			c.frame.Set(c.frame.Peek() + 1e-9) // drawn again
		})
	}
}

// load reads the animation off the main thread.
func (c *canvas) load(src string, data []byte) {
	id := c.p.Animation
	go func() {
		a, err := read(src, data, id)
		ui.Post(func() {
			if c.p.Source != nil && src != "" && ui.AssetURL(signal.Untrack(c.p.Source.Get)) != src {
				return // another source since
			}
			if err != nil {
				if c.p.Failed != nil {
					c.p.Failed(err.Error())
				}
				return
			}
			c.anim.Set(a)
			c.done = false
			from, _ := c.span()
			c.frame.Set(from)
			if c.p.Loaded != nil {
				info := lottieplayer.AnimationInfo{Width: a.Width, Height: a.Height, Start: a.In, End: a.Out, FrameRate: a.FrameRate}
				for _, m := range a.Markers {
					info.Markers = append(info.Markers, lottieplayer.MarkerInfo{Name: m.Name, Start: m.Start, Duration: m.Duration})
				}
				c.p.Loaded(info)
			}
			if c.pending != nil {
				s := *c.pending
				c.pending = nil
				c.serial = 0
				c.seek(s)
			}
			c.report()
			c.wake()
		})
	}()
}

// read is the animation in a file (an asset, a URL, a path) or bytes.
func read(src string, data []byte, id string) (*render.Animation, error) {
	if data == nil {
		var err error
		if data, err = readSource(src); err != nil {
			return nil, err
		}
	}
	if id != "" {
		return render.ParseDotLottie(data, id)
	}
	return render.Parse(data)
}

func readSource(src string) ([]byte, error) {
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		resp, err := http.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("%s: %s", src, resp.Status)
		}
		return io.ReadAll(resp.Body)
	case strings.HasPrefix(src, "file://"):
		return os.ReadFile(strings.TrimPrefix(src, "file://"))
	case filepath.IsAbs(src):
		return os.ReadFile(src)
	}
	return os.ReadFile(filepath.Join(assetsDir(), filepath.FromSlash(strings.TrimPrefix(src, "/"))))
}

// assetsDir is where a desktop app's assets are: as the hosts find them
// (ASUKA_ASSETS; beside the executable; an install prefix's share; the
// working directory's, under asuka dev).
func assetsDir() string {
	if d := os.Getenv("ASUKA_ASSETS"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		for _, d := range []string{
			filepath.Join(filepath.Dir(exe), "assets"),
			filepath.Join(filepath.Dir(exe), "..", "share", strings.TrimSuffix(filepath.Base(exe), ".exe"), "assets"),
		} {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d
			}
		}
	}
	return "assets"
}

func (c *canvas) setPlaying(v bool) {
	if v == c.playing {
		return
	}
	c.playing = v
	if v && c.done {
		// At its end: from the start again.
		from, to := c.span()
		if c.speed < 0 {
			from = to
		}
		c.frame.Set(from)
		c.done = false
	}
	c.wake()
}

// rangeChanged plays the new range from its start.
func (c *canvas) rangeChanged() {
	if c.anim.Peek() == nil {
		return
	}
	from, to := c.span()
	if c.speed < 0 {
		from = to
	}
	c.done = false
	c.tween = nil
	c.frame.Set(from)
	c.report()
	c.wake()
}

func (c *canvas) seek(s lottieplayer.SeekTo) {
	if s.Serial == c.serial {
		return
	}
	a := c.anim.Peek()
	if a == nil {
		c.pending = &s
		return
	}
	c.serial = s.Serial
	from, to := c.span()
	target := a.In + s.Progress*(a.Out-a.In)
	switch s.To {
	case 1:
		target = from
	case 2:
		target = to
	}
	c.done = false
	if s.Duration > 0 {
		c.tween = &tween{from: c.frame.Peek(), to: target, seconds: s.Duration, curve: ui.Curve(s.Curve), damping: s.Damping}
		c.wake()
		return
	}
	c.tween = nil
	c.frame.Set(target)
	c.report()
}

func (c *canvas) report() {
	a := c.anim.Peek()
	if c.p.Report && c.p.Progress != nil && a != nil && a.Out > a.In {
		c.p.Progress(math.Max(0, math.Min(1, (c.frame.Peek()-a.In)/(a.Out-a.In))))
	}
}

// tick moves the animation on to the display's frame at t.
func (c *canvas) tick(t time.Duration) {
	a := c.anim.Peek()
	if a == nil {
		return
	}
	dt := 0.0
	if c.last >= 0 {
		dt = (t - c.last).Seconds()
	}
	c.last = t
	if tw := c.tween; tw != nil {
		if !tw.begun {
			tw.start, tw.begun = t, true
		}
		x := (t - tw.start).Seconds() / tw.seconds
		if x >= 1 {
			c.tween = nil
			c.frame.Set(tw.to)
		} else {
			c.frame.Set(tw.from + (tw.to-tw.from)*ease(tw.curve, tw.damping, x, tw.seconds))
		}
		c.report()
		return
	}
	if !c.playing || c.done {
		return
	}
	from, to := c.span()
	f := c.frame.Peek() + dt*a.FrameRate*c.speed
	switch {
	case c.speed >= 0 && f >= to, c.speed < 0 && f <= from:
		if c.p.Loop && to > from {
			n := to - from
			f = from + math.Mod(math.Mod(f-from, n)+n, n)
		} else {
			f = to
			if c.speed < 0 {
				f = from
			}
			c.done = true
		}
	case f < from && c.speed >= 0:
		f = from
	}
	c.frame.Set(f)
	c.report()
	if c.done && c.p.Completed != nil {
		c.p.Completed()
	}
}

// ease is a seek's animation curve at x (0 to 1) of its time: ui.Curve's,
// as UIKit's are (a spring, critically damped at damping 1).
func ease(curve ui.Curve, damping, x, seconds float64) float64 {
	bezier := func(x1, y1, x2, y2 float64) float64 {
		// Solve for the curve's x by bisection; then its y.
		lo, hi, t := 0.0, 1.0, x
		for range 30 {
			u := 1 - t
			v := 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t
			if math.Abs(v-x) < 1e-6 {
				break
			}
			if v < x {
				lo = t
			} else {
				hi = t
			}
			t = (lo + hi) / 2
		}
		u := 1 - t
		return 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t
	}
	switch curve {
	case ui.Linear:
		return x
	case ui.EaseIn:
		return bezier(0.42, 0, 1, 1)
	case ui.EaseOut:
		return bezier(0, 0, 0.58, 1)
	case ui.Spring:
		z := math.Max(0.01, math.Min(1, damping))
		w := 10 / math.Max(seconds, 0.01) // settles within its time
		tt := x * seconds
		if z >= 1 {
			return 1 - math.Exp(-w*tt)*(1+w*tt)
		}
		wd := w * math.Sqrt(1-z*z)
		return 1 - math.Exp(-z*w*tt)*(math.Cos(wd*tt)+z*w/wd*math.Sin(wd*tt))
	}
	return bezier(0.42, 0, 0.58, 1)
}
