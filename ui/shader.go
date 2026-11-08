package ui

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// Uniform is a value a shader reads by name: a float4 to its source (a
// color's components, a point, up to four numbers).
type Uniform struct {
	Name  string
	Value [4]float32
}

// ColorUniform is a color as a uniform: its red, green, blue and alpha,
// 0 to 1 (an RGB color; a semantic one reads as black).
func ColorUniform(name string, c Color) Uniform {
	r, g, b, a, _ := c.RGBA()
	return Uniform{name, [4]float32{float32(r) / 255, float32(g) / 255, float32(b) / 255, float32(a) / 255}}
}

// ShaderProps configures a Shader.
type ShaderProps struct {
	// Source is the shading function: the color of each point of the box,
	//
	//	float4 shade(float2 uv) { ... }
	//
	// uv going from 0,0 at its top left to 1,1 at its bottom right, the
	// color's red, green, blue and alpha from 0 to 1. It's written in what
	// Metal's language and AGSL share: float, float2, float3, float4,
	// their constructors and swizzles; mix, smoothstep, step, clamp,
	// fract, floor, mod, sin, cos, atan(y, x), pow, exp, length, distance,
	// dot, normalize, abs, min, max; functions of its own. Numbers are
	// written with a point (2.0, not 2): AGSL doesn't turn integers into
	// floats. It reads time (seconds since it was shown), size (its size
	// in points, a float2) and its Uniforms by name.
	Source string
	// Uniforms are the values the source reads by name, each a float4. A
	// change draws it again; the names are fixed once it's made.
	Uniforms signal.Accessor[[]Uniform]
	// Animated runs time: it's drawn again every frame the display draws
	// (nothing in Go runs for it), for motion the source computes.
	Animated bool
	Style    Styler
}

// Shader is a box a GPU program draws (SwiftUI's Shader, Android's
// RuntimeShader): flowing gradients, noise, glows, ripples, under its
// children, over its background. Metal on iOS and macOS; AGSL on Android
// 13 and later. Android before 13 and Windows have no shader to run it
// with: they show the box's background alone, so give it one that
// stands in.
//
//	ui.Shader(ui.ShaderProps{Source: `
//		float4 shade(float2 uv) {
//			float d = distance(uv, float2(0.5, 0.5));
//			float glow = smoothstep(0.5, 0.0, d) * (0.8 + 0.2 * sin(time * 2.0));
//			return float4(tint.rgb * glow, 1.0);
//		}`,
//		Uniforms: ui.Val([]ui.Uniform{ui.ColorUniform("tint", ui.Hex(0x38bdf8))}),
//		Animated: true,
//		Style:    ui.Style{Height: ui.Pt(200), CornerRadius: 16, Clip: true}})
//
//go:noinline
func Shader(p ShaderProps, children ...Node) Node {
	e := newElement(ops.KindShader, true)
	e.setStyle(p.Style, false)
	b := e.r.batch
	b.SetString(e.id, ops.PropShaderSource, p.Source)
	if p.Animated {
		b.SetBool(e.id, ops.PropShaderAnimated, true)
	}
	if p.Uniforms != nil {
		var sent []byte
		signal.RenderEffect(func() {
			us := p.Uniforms.Get()
			enc := binary.LittleEndian.AppendUint32(e.encoded[:0], uint32(len(us)))
			for _, u := range us {
				enc = binary.LittleEndian.AppendUint32(enc, uint32(len(u.Name)))
				enc = append(enc, u.Name...)
				for _, v := range u.Value {
					enc = binary.LittleEndian.AppendUint32(enc, math.Float32bits(v))
				}
			}
			e.encoded = enc // reused from change to change
			if bytes.Equal(enc, sent) {
				return
			}
			sent = slices.Clone(enc)
			e.r.batch.SetBytes(e.id, ops.PropShaderUniforms, sent)
		})
	}
	addKids(e, children)
	return Node{e}
}
