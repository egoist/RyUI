//go:build linux && !android

package linux

import (
	"log"
	"math"
	"slices"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/shadercode"
	"github.com/egoist/asuka/ops"
)

// A Shader on Linux: its Kage program compiled to GLSL and drawn on the
// GPU, with OpenGL, in a context of GDK's, into a texture that GTK's
// renderer draws over the view, stretched while a new size renders, and
// clipped to its corners (shader_gl.go). A frame renders as the view
// snapshots, when its inputs changed since the last one: its uniforms,
// its size in pixels, the scale, and Time, which an animated shader's
// frame clock moves on each frame the display draws.
//
// Where GDK has no GL (no driver, GDK_DISABLE=gl), the program runs on
// the CPU instead, off the main thread (shader_cpu.go).

type kageShader struct {
	code     *shadercode.Code
	words    []uint32 // the core's lanes
	animated bool
	start    time.Time
	tick     uint32 // the frame clock's callback, animated

	texture Ptr // the frame on show

	lastInputs   []uint32 // the inputs of the last frame rendered (or started)
	lastW, lastH int      // and the view's size in pixels for it

	cpu *cpuShader // without GL
}

var kages = map[*node]*kageShader{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindShader {
			return false
		}
		kages[n] = &kageShader{start: time.Now()}
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
			if s.cpu != nil {
				s.cpu.gen = new(struct{}) // a frame rendering is dropped
			}
			delete(kages, n)
		}
	})
}

func (h *Host) setShaderProp(n *node, p ops.PropID, v *ops.Value) {
	s := kages[n]
	switch p {
	case ops.PropShaderCode:
		s.code = nil
		s.lastInputs = nil
		s.cpu = nil
		c, err := shadercode.Decode(v.Bytes)
		if err != nil {
			log.Printf("asuka: a ui.Shader doesn't run: %v", err)
			break
		}
		s.code = c
		if len(s.words) != c.Lanes {
			s.words = make([]uint32, c.Lanes)
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
	queueDraw(n)
}

// shaderClock runs an animated shader's frames on its view's frame
// clock: each frame queues a draw, which renders one with the new Time.
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
		queueDraw(n)
		return true
	})
}

// pixels is the view's size in pixels.
func (h *Host) pixels(n *node) (w, ht int, k float32) {
	k = h.scaleOf(n)
	fw, fh := h.size(n, n.widget)
	return int(math.Ceil(float64(fw * k))), int(math.Ceil(float64(fh * k))), k
}

// inputs are the lanes the next frame of n's shader renders with, and
// its size in pixels; changed is false when they're the last frame's.
func (h *Host) shaderInputs(n *node, s *kageShader) (in []uint32, w, ht int, k float32, changed bool) {
	if s.code == nil || n.widget == 0 || !n.placed {
		return nil, 0, 0, 0, false
	}
	w, ht, k = h.pixels(n)
	c := s.code
	if w <= 0 || ht <= 0 || len(s.words) != c.Lanes {
		return nil, 0, 0, 0, false
	}
	in = slices.Clone(s.words)
	in[c.Size] = math.Float32bits(float32(w) / k)
	in[c.Size+1] = math.Float32bits(float32(ht) / k)
	in[c.Scale] = math.Float32bits(k)
	if c.Time >= 0 {
		in[c.Time] = math.Float32bits(float32(time.Since(s.start).Seconds()))
	}
	if w == s.lastW && ht == s.lastH && slices.Equal(in, s.lastInputs) {
		return nil, 0, 0, 0, false
	}
	return in, w, ht, k, true
}

// renderShader renders a frame of n's shader if its inputs changed
// since the last one: on the GPU, or, without GL, by starting one on the
// CPU.
func (h *Host) renderShader(n *node, s *kageShader) {
	if s.cpu == nil && s.code != nil && glReady() {
		in, w, ht, _, changed := h.shaderInputs(n, s)
		if !changed {
			return
		}
		s.lastInputs, s.lastW, s.lastH = in, w, ht
		if t := renderGL(s.code, in, w, ht); t != 0 {
			if s.texture != 0 {
				ObjectUnref(s.texture)
			}
			s.texture = t
		}
		return
	}
	h.renderShaderCPU(n, s)
}

// drawShader draws the frame on show over the view, clipped to its
// corners, after rendering a new one if its inputs changed.
func (h *Host) drawShader(n *node, s *kageShader, snap Ptr, bounds Rect) {
	h.renderShader(n, s)
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
