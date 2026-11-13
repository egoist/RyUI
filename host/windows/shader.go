//go:build windows

package windows

import (
	"log"
	"math"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/shader"
	"github.com/egoist/asuka/internal/shadercode"
	"github.com/egoist/asuka/internal/shaderir/interp"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A Shader on Windows: its Kage program run on the CPU (package interp),
// off the main thread. A frame renders on goroutines, across the cores,
// into a buffer of its own, and crosses back with Post: the main thread
// swaps it in and paints it, stretched over the view, and never waits for
// one. Inputs that change while a frame renders render again once it's
// done. Animated, a timer starts a frame whenever none is rendering, so
// the frame rate is what the CPU affords, up to the timer's.
//
// The pixels a frame has are capped (an animated shader's more tightly):
// a large view renders at a fraction of its resolution, scaled up. The
// positions the program gets are the view's pixels' still, so it draws
// the same picture as on a GPU, coarser.

const timerShader = 7

const (
	shaderFramePixels  = 64 << 10  // an animated shader's pixels a frame, at most
	shaderStaticPixels = 512 << 10 // a still one's
)

type cpuShader struct {
	code     *shadercode.Code
	prog     *interp.Program
	failed   bool
	words    []uint32 // the core's lanes
	animated bool
	start    time.Time

	busy  bool // a frame is rendering
	dirty bool // and the inputs changed since it started

	bmp    *Bitmap // the frame on show, over pix, pw by ph
	pix    []byte
	pw, ph int
	spare  []byte // the buffer the next frame renders into

	lastInputs   []uint32 // the inputs of the last frame started
	lastW, lastH int      // and the view's size in pixels for it

	gen *struct{} // a frame started for an older program is dropped
}

func (h *Host) setShaderProp(n *node, p ops.PropID, v *ops.Value) {
	s := n.kage
	if s == nil {
		s = &cpuShader{start: time.Now(), gen: new(struct{})}
		n.kage = s
	}
	switch p {
	case ops.PropShaderCode:
		s.code, s.prog, s.failed = nil, nil, false
		s.gen = new(struct{})
		s.lastInputs = nil
		c, err := shadercode.Decode(v.Bytes)
		if err == nil {
			var prog *interp.Program
			if prog, err = compileCPU(c.Source); err == nil {
				s.code, s.prog = c, prog
			}
		}
		if err != nil {
			s.failed = true
			log.Printf("asuka: a ui.Shader doesn't run: %v", err)
		}
		if s.code != nil && len(s.words) != s.code.Lanes {
			s.words = make([]uint32, s.code.Lanes)
		}
	case ops.PropShaderUniforms:
		n := len(v.Bytes) / 4
		if len(s.words) != n {
			s.words = make([]uint32, n)
		}
		for i := range s.words {
			s.words[i] = le32(v.Bytes[4*i:])
		}
	case ops.PropShaderAnimated:
		s.animated = v.Int != 0
		h.shaderClock(n)
	}
	h.renderShader(n)
}

// compileCPU compiles a shader's Kage source for package interp.
func compileCPU(src string) (*interp.Program, error) {
	p, err := shader.CompileFragment([]byte(src))
	if err != nil {
		return nil, err
	}
	return interp.New(p)
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// shaderClock runs the timer of an animated shader's frames.
func (h *Host) shaderClock(n *node) {
	if n.hwnd == 0 || n.kage == nil {
		return
	}
	if n.kage.animated && n.kage.code != nil && n.kage.code.Time >= 0 {
		SetTimer(n.hwnd, timerShader, 16)
	} else {
		KillTimer(n.hwnd, timerShader)
	}
}

// renderShader starts a frame of n's shader if its inputs changed since
// the last one, or marks them changed if one is rendering.
func (h *Host) renderShader(n *node) {
	s := n.kage
	if s == nil || s.prog == nil || n.hwnd == 0 {
		return
	}
	r := ClientRect(n.hwnd)
	w, ht := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if w <= 0 || ht <= 0 || len(s.words) != s.code.Lanes {
		return
	}
	if s.busy {
		s.dirty = true
		return
	}
	k := h.scaleOf(n)
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
	budget := shaderStaticPixels
	if s.animated && c.Time >= 0 {
		budget = shaderFramePixels
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
	id, gen, prog := n.id, s.gen, s.prog
	sx, sy := float32(w)/float32(pw), float32(ht)/float32(ph)
	go func() {
		ok := shade(prog, in, buf, pw, ph, sx, sy, k)
		h.Post(func() {
			n := h.nodes[id]
			if n == nil || n.kage == nil {
				return
			}
			s := n.kage
			s.busy = false
			if s.gen == gen && ok {
				s.bmp.Close()
				s.spare = s.pix
				s.pix, s.pw, s.ph = buf, pw, ph
				s.bmp = NewBitmap(int32(pw), int32(ph), buf)
				h.redraw(n)
			}
			if s.dirty || s.gen != gen {
				s.dirty = false
				h.renderShader(n)
			}
		})
	}()
}

// shade renders the program into buf, pw by ph premultiplied BGRA pixels,
// each sx by sy of the view's pixels, across the cores. It's false if the
// program failed.
func shade(prog *interp.Program, words []uint32, buf []byte, pw, ph int, sx, sy, k float32) (ok bool) {
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
						log.Printf("asuka: a ui.Shader failed: %v", r)
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
					a := unit(c[3])
					buf[o] = byte(min(unit(c[2]), a)*255 + 0.5)
					buf[o+1] = byte(min(unit(c[1]), a)*255 + 0.5)
					buf[o+2] = byte(min(unit(c[0]), a)*255 + 0.5)
					buf[o+3] = byte(a*255 + 0.5)
					o += 4
				}
			}
		}(y0, min(y0+rows, ph))
	}
	wg.Wait()
	return ok
}

func unit(v float32) float32 {
	if !(v > 0) { // NaN too
		return 0
	}
	return min(v, 1)
}

// paintShader draws the frame on show over the view, clipped to its
// corners.
func (h *Host) paintShader(n *node, g Graphics, w, ht, radius float32) {
	s := n.kage
	if s == nil {
		return
	}
	if r := ClientRect(n.hwnd); int(r.Right-r.Left) != s.lastW || int(r.Bottom-r.Top) != s.lastH {
		h.renderShader(n) // resized: a frame for the new size
	}
	if s.bmp == nil {
		return
	}
	if radius > 0 {
		p := NewPath()
		p.RoundRect(0, 0, w, ht, radius)
		g.ClipPath(p)
		defer func() { g.ResetClip(); p.Close() }()
	}
	g.DrawImageClamped(s.bmp, 0, 0, w, ht, 0, 0, float32(s.pw), float32(s.ph))
}

// release lets go of the frame's bitmap.
func (s *cpuShader) release() {
	s.bmp.Close()
	s.bmp = nil
	s.gen = new(struct{}) // a frame rendering is dropped
}
