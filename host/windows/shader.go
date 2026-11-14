//go:build windows

package windows

import (
	"log"
	"math"
	"slices"
	"time"

	"github.com/egoist/asuka/internal/shadercode"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A Shader on Windows: its Kage program compiled to HLSL and drawn on
// the GPU with Direct3D 11 (shader_d3d.go), off the main thread. A frame
// renders on a goroutine into a texture of the view's size in pixels,
// which the GPU copies back to memory, and crosses back with Post: the
// main thread swaps it in and paints it over the view, as GDI+ paints
// the views around it, and never waits for one. Inputs that change while
// a frame renders render again once it's done. Animated, a timer starts
// a frame whenever none is rendering, so the frame rate is what the GPU
// and the copy afford, up to the timer's.

const timerShader = 7

type kageShader struct {
	code     *shadercode.Code
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

	target *d3dTarget // the GPU's side, which only the frames' goroutines touch
	gen    *struct{}  // a frame started for an older program is dropped
}

func (h *Host) setShaderProp(n *node, p ops.PropID, v *ops.Value) {
	s := n.kage
	if s == nil {
		s = &kageShader{start: time.Now(), gen: new(struct{}), target: new(d3dTarget)}
		n.kage = s
	}
	switch p {
	case ops.PropShaderCode:
		s.code, s.failed = nil, false
		s.gen = new(struct{})
		s.lastInputs = nil
		c, err := shadercode.Decode(v.Bytes)
		if err != nil {
			s.failed = true
			log.Printf("asuka: a ui.Shader doesn't run: %v", err)
			break
		}
		s.code = c
		if len(s.words) != c.Lanes {
			s.words = make([]uint32, c.Lanes)
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
	if s == nil || s.code == nil || s.failed || n.hwnd == 0 {
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
	buf := s.spare
	s.spare = nil
	s.busy, s.dirty = true, false
	id, gen, t := n.id, s.gen, s.target
	go func() {
		pix, pw, ph, err := t.render(c, in, w, ht, buf)
		h.Post(func() {
			n := h.nodes[id]
			if n == nil || n.kage == nil {
				return
			}
			s := n.kage
			s.busy = false
			if s.gen == gen {
				switch {
				case err != nil:
					s.failed = true
					log.Printf("asuka: a ui.Shader doesn't run: %v", err)
				case pix != nil:
					s.bmp.Close()
					s.spare = s.pix
					s.pix, s.pw, s.ph = pix, pw, ph
					s.bmp = NewBitmap(int32(pw), int32(ph), pix)
					h.redraw(n)
				}
			}
			if s.dirty || s.gen != gen {
				s.dirty = false
				h.renderShader(n)
			}
		})
	}()
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

// release lets go of the frame's bitmap and the GPU's side.
func (s *kageShader) release() {
	s.bmp.Close()
	s.bmp = nil
	s.gen = new(struct{}) // a frame rendering is dropped
	go s.target.release()
}
