package ui

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// ShaderProgram is a compiled shader: package shader's *Program, which
// compiles Kage's Go-syntax shaders (shader.MustCompile).
type ShaderProgram interface {
	// ShaderCode is the program as the hosts take it.
	ShaderCode() []byte
	// ShaderLanes is how many 32-bit lanes its uniforms' values take.
	ShaderLanes() int
	// ShaderUniform says where a uniform variable's lanes are, and their
	// kind: 0 float, 1 int, 2 uint, 3 bool.
	ShaderUniform(name string) (lane, lanes, kind int, ok bool)
}

// Uniform is the value of a shader's uniform variable, by its name: a
// scalar's one number, a vector's components, a matrix's columns in
// turn, an array's elements in order. An int's, a uint's and a bool's
// are their numbers too (a bool true when not 0).
type Uniform struct {
	Name  string
	Value []float32
}

// ColorUniform is a color as a uniform vec4: its red, green, blue and
// alpha, 0 to 1, not premultiplied (an RGB color; a semantic one reads as
// black).
func ColorUniform(name string, c Color) Uniform {
	r, g, b, a, _ := c.RGBA()
	return Uniform{name, []float32{float32(r) / 255, float32(g) / 255, float32(b) / 255, float32(a) / 255}}
}

// ShaderProps configures a Shader.
type ShaderProps struct {
	// Program is the shader (package shader: shader.MustCompile(src)).
	Program ShaderProgram
	// Uniforms are its uniform variables' values. A change draws it
	// again; a name it hasn't is left out, and a variable left out is 0.
	Uniforms signal.Accessor[[]Uniform]
	// Animated runs the shader's Time (var Time float): it's drawn again
	// every frame the display draws, with nothing running in Go.
	Animated bool
	Style    Styler
}

// Shader is a box a GPU program draws, under its children, over its
// background: flowing gradients, noise, glows, ripples (SwiftUI's
// Shader, Android's RuntimeShader). The program is a Kage shader, Go's
// syntax, which package shader compiles: Metal on iOS and macOS, AGSL on
// Android, and on Windows the same program run on the CPU.
//
//	var glow = shader.MustCompile(`package main
//
//	var Time float
//	var Tint vec4
//
//	func Fragment(dst vec4, pos vec2) vec4 {
//		d := distance(pos/rectSize(), vec2(0.5))
//		g := smoothstep(0.5, 0, d) * (0.8 + 0.2*sin(Time*2))
//		return vec4(Tint.rgb*g, 1) * Tint.a
//	}`)
//
//	ui.Shader(ui.ShaderProps{Program: glow, Animated: true,
//		Uniforms: ui.Val([]ui.Uniform{ui.ColorUniform("Tint", colors.Hex(0x38bdf8))}),
//		Style:    ui.Style{Height: ui.Pt(200), CornerRadius: 16, Clip: true}})
//
//go:noinline
func Shader(p ShaderProps, children ...Node) Node {
	e := newElement(ops.KindShader, true)
	e.setStyle(p.Style, false)
	b := e.r.batch
	if p.Program != nil {
		b.SetBytes(e.id, ops.PropShaderCode, p.Program.ShaderCode())
	}
	if p.Animated {
		b.SetBool(e.id, ops.PropShaderAnimated, true)
	}
	if p.Program != nil && p.Uniforms != nil {
		prog := p.Program
		lanes := make([]uint32, prog.ShaderLanes())
		var sent []byte
		signal.RenderEffect(func() {
			clear(lanes)
			for _, u := range p.Uniforms.Get() {
				at, n, kind, ok := prog.ShaderUniform(u.Name)
				if !ok {
					continue
				}
				for i, v := range u.Value[:min(n, len(u.Value))] {
					var w uint32
					switch kind {
					case 1:
						w = uint32(int32(v))
					case 2:
						w = uint32(v)
					case 3:
						if v != 0 {
							w = 1
						}
					default:
						w = math.Float32bits(v)
					}
					lanes[at+i] = w
				}
			}
			enc := e.encoded[:0]
			for _, w := range lanes {
				enc = binary.LittleEndian.AppendUint32(enc, w)
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
