//go:build linux && !android

package linux

import (
	"log"
	"math"
	"runtime"
	"slices"
	"sync"
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/internal/shader"
	"github.com/egoist/ryui/internal/shadercode"
	"github.com/egoist/ryui/internal/shaderir/interp"
	"github.com/egoist/ryui/ops"
)

// A Shader on Linux, as on Windows: its Kage program run on the CPU
// (package interp), off the main thread. A frame renders on goroutines,
// across the cores, into a buffer of its own, and crosses back with
// Post: the main thread makes it a GdkMemoryTexture, which the view
// draws stretched over itself, and never waits for one. Inputs that
// change while a frame renders render again once it's done. Animated,
// the view's frame clock starts a frame whenever none is rendering, so
// the frame rate is what the CPU affords, up to the display's.
//
// The pixels a frame has are capped (an animated shader's more tightly):
// a large view renders at a fraction of its resolution, scaled up. The
// positions the program gets are the view's pixels' still, so it draws
// the same picture as on a GPU, coarser.

const (
	kageFramePixels  = 64 << 10  // an animated shader's pixels a frame, at most
	kageStaticPixels = 512 << 10 // a still one's
)

type kageShader struct {
	code     *shadercode.Code
	prog     *interp.Program
	words    []uint32 // the core's lanes
	animated bool
	start    time.Time
	tick     uint32 // the frame clock's callback, animated

	busy  bool // a frame is rendering
	dirty bool // and the inputs changed since it started

	texture Ptr    // the frame on show
	spare   []byte // the buffer the next frame renders into

	lastInputs   []uint32 // the inputs of the last frame started
	lastW, lastH int      // and the view's size in pixels for it

	gen *struct{} // a frame started for an older program is dropped
}

var kages = map[*node]*kageShader{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindShader {
			return false
		}
		kages[n] = &kageShader{start: time.Now(), gen: new(struct{})}
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if kages[n] == nil {
			return false
		}
		h.setShaderProp(n, p, v)
		return true
	})
	drawHooks = append(drawHooks, func(h *Host, n *node, snap Ptr, bounds Rect) bool {
		s := kages[n]
		if s == nil {
			return false
		}
		h.drawShader(n, s, snap, bounds)
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if s := kages[n]; s != nil {
			if s.tick != 0 && n.widget != 0 {
				Draw.WidgetRemoveTickCallback(n.widget, s.tick)
			}
			if s.texture != 0 {
				ObjectUnref(s.texture)
			}
			s.gen = new(struct{}) // a frame rendering is dropped
			delete(kages, n)
		}
	})
}

func (h *Host) setShaderProp(n *node, p ops.PropID, v *ops.Value) {
	s := kages[n]
	switch p {
	case ops.PropShaderCode:
		s.code, s.prog = nil, nil
		s.gen = new(struct{})
		s.lastInputs = nil
		c, err := shadercode.Decode(v.Bytes)
		if err == nil {
			var prog *interp.Program
			if prog, err = compileKage(c.Source); err == nil {
				s.code, s.prog = c, prog
			}
		}
		if err != nil {
			log.Printf("ryui: a ui.Shader doesn't run: %v", err)
		}
		if s.code != nil && len(s.words) != s.code.Lanes {
			s.words = make([]uint32, s.code.Lanes)
		}
	case ops.PropShaderUniforms:
		c := len(v.Bytes) / 4
		if len(s.words) != c {
			s.words = make([]uint32, c)
		}
		for i := range s.words {
			s.words[i] = le.Uint32(v.Bytes[4*i:])
		}
	case ops.PropShaderAnimated:
		s.animated = v.Int != 0
	default:
		return
	}
	h.shaderClock(n, s)
	h.renderShader(n, s)
}

// compileKage compiles a shader's Kage source for package interp.
func compileKage(src string) (*interp.Program, error) {
	p, err := shader.CompileFragment([]byte(src))
	if err != nil {
		return nil, err
	}
	return interp.New(p)
}

// shaderClock runs an animated shader's frames on its view's frame
// clock: one starts whenever none is rendering.
func (h *Host) shaderClock(n *node, s *kageShader) {
	on := s.animated && s.code != nil && s.code.Time >= 0 && n.widget != 0
	if on == (s.tick != 0) {
		return
	}
	if !on {
		Draw.WidgetRemoveTickCallback(n.widget, s.tick)
		s.tick = 0
		return
	}
	s.tick = AddTickCallback(n.widget, func(Ptr) bool {
		if kages[n] != s || s.tick == 0 {
			return false
		}
		h.renderShader(n, s)
		return true
	})
}

// pixels is the view's size in pixels.
func (h *Host) pixels(n *node) (w, ht int, k float32) {
	k = h.scaleOf(n)
	fw, fh := h.size(n, n.widget)
	return int(math.Ceil(float64(fw * k))), int(math.Ceil(float64(fh * k))), k
}

// renderShader starts a frame of n's shader if its inputs changed since
// the last one, or marks them changed if one is rendering.
func (h *Host) renderShader(n *node, s *kageShader) {
	if s.prog == nil || n.widget == 0 || !n.placed {
		return
	}
	w, ht, k := h.pixels(n)
	if w <= 0 || ht <= 0 || len(s.words) != s.code.Lanes {
		return
	}
	if s.busy {
		s.dirty = true
		return
	}
	c := s.code
	in := slices.Clone(s.words)
	in[c.Size] = math.Float32bits(float32(w) / k)
	in[c.Size+1] = math.Float32bits(float32(ht) / k)
	in[c.Scale] = math.Float32bits(k)
	if c.Time >= 0 {
		in[c.Time] = math.Float32bits(float32(time.Since(s.start).Seconds()))
	}
	if w == s.lastW && ht == s.lastH && slices.Equal(in, s.lastInputs) {
		return
	}
	s.lastInputs, s.lastW, s.lastH = in, w, ht
	budget := kageStaticPixels
	if s.animated && c.Time >= 0 {
		budget = kageFramePixels
	}
	step := max(1, math.Sqrt(float64(w*ht)/float64(budget)))
	pw, ph := max(1, int(math.Ceil(float64(w)/step))), max(1, int(math.Ceil(float64(ht)/step)))
	buf := s.spare
	s.spare = nil
	if cap(buf) < pw*ph*4 {
		buf = make([]byte, pw*ph*4)
	}
	buf = buf[:pw*ph*4]
	s.busy, s.dirty = true, false
	gen, prog := s.gen, s.prog
	sx, sy := float32(w)/float32(pw), float32(ht)/float32(ph)
	go func() {
		ok := kageShade(prog, in, buf, pw, ph, sx, sy, k)
		h.Post(func() {
			if kages[n] != s {
				return
			}
			s.busy = false
			if s.gen == gen && ok {
				b := BytesNew(&buf[0], uintptr(len(buf)))
				t := MemoryTextureNew(int32(pw), int32(ph), MemoryB8G8R8A8Premultiplied, b, uintptr(pw*4))
				BytesUnref(b)
				if s.texture != 0 {
					ObjectUnref(s.texture)
				}
				s.texture = t
				s.spare = buf // GBytes copied it
				queueDraw(n)
			}
			if s.dirty || s.gen != gen {
				s.dirty = false
				h.renderShader(n, s)
			}
		})
	}()
}

// kageShade renders the program into buf, pw by ph premultiplied BGRA
// pixels, each sx by sy of the view's pixels, across the cores. It's
// false if the program failed.
func kageShade(prog *interp.Program, words []uint32, buf []byte, pw, ph int, sx, sy, k float32) (ok bool) {
	workers := min(runtime.NumCPU(), ph)
	rows := (ph + workers - 1) / workers
	var wg sync.WaitGroup
	var failed sync.Once
	ok = true
	for y0 := 0; y0 < ph; y0 += rows {
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					failed.Do(func() {
						ok = false
						log.Printf("ryui: a ui.Shader failed: %v", r)
					})
				}
			}()
			in := &interp.Inputs{Uniforms: words}
			for y := y0; y < y1; y++ {
				py := (float32(y) + 0.5) * sy
				o := y * pw * 4
				for x := range pw {
					px := (float32(x) + 0.5) * sx
					c, drawn := prog.Color(in, [4]float32{px, py, 0, 1}, px/k, py/k)
					if !drawn {
						c = [4]float32{}
					}
					a := unitClamp(c[3])
					buf[o] = byte(min(unitClamp(c[2]), a)*255 + 0.5)
					buf[o+1] = byte(min(unitClamp(c[1]), a)*255 + 0.5)
					buf[o+2] = byte(min(unitClamp(c[0]), a)*255 + 0.5)
					buf[o+3] = byte(a*255 + 0.5)
					o += 4
				}
			}
		}(y0, min(y0+rows, ph))
	}
	wg.Wait()
	return ok
}

func unitClamp(v float32) float32 {
	if !(v > 0) { // NaN too
		return 0
	}
	return min(v, 1)
}

// drawShader draws the frame on show over the view, clipped to its
// corners; a new size renders a frame for it.
func (h *Host) drawShader(n *node, s *kageShader, snap Ptr, bounds Rect) {
	if w, ht, _ := h.pixels(n); w != s.lastW || ht != s.lastH {
		h.renderShader(n, s)
	}
	if s.texture == 0 {
		return
	}
	rounded := n.cornerRadius > 0 && !n.clip // a clipping view clips it already
	if rounded {
		rr := Rounded(bounds, n.cornerRadius)
		Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
	}
	Call(Draw.FnSnapshotAppendScaledTexure, snap, s.texture, 0, Addr(&bounds)) // GSK_SCALING_FILTER_LINEAR
	if rounded {
		Call(FnSnapshotPop, snap)
	}
}
