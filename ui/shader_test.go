package ui_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/internal/shadercode"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/shader"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var glow = shader.MustCompile(`package main

var Time float
var Tint vec4
var Count int

func Fragment(dst vec4, pos vec2) vec4 {
	g := smoothstep(0.5, 0, distance(pos/rectSize(), vec2(0.5))) * float(Count)
	return vec4(Tint.rgb*g, 1) * Tint.a
}
`)

// A shader sends its compiled code once, and its uniforms' lanes when
// they change: a vec4's floats where its lanes are, an int as an int.
func TestShader(t *testing.T) {
	tint := signal.New(colors.Hex(0x38bdf8))
	h, r := mount(t, func() ui.Node {
		return ui.Shader(ui.ShaderProps{
			Program: glow,
			Uniforms: signal.Func(func() []ui.Uniform {
				return []ui.Uniform{ui.ColorUniform("Tint", tint.Get()), {Name: "Count", Value: []float32{3}}, {Name: "Nope", Value: []float32{1}}}
			}),
			Animated: true,
			Style:    ui.Style{Height: ui.Pt(100)},
		})
	})
	s := h.OfKind(ops.KindShader)[0]
	code, err := shadercode.Decode(s.Props[ops.PropShaderCode].Bytes)
	if err != nil || code.Metal == "" || code.AGSL == "" || code.Time < 0 || !s.Bool(ops.PropShaderAnimated) {
		t.Fatalf("code %+v, %v", code, err)
	}
	lanes := s.Props[ops.PropShaderUniforms].Bytes
	if len(lanes) != 4*code.Lanes {
		t.Fatalf("%d bytes of %d lanes", len(lanes), code.Lanes)
	}
	word := func(i int) uint32 { return binary.LittleEndian.Uint32(lanes[4*i:]) }
	at, _, _, _ := glow.ShaderUniform("Tint")
	if math.Float32frombits(word(at+3)) != 1 || math.Float32frombits(word(at)) < 0.2 {
		t.Fatalf("Tint's lanes %v", lanes[4*at:4*at+16])
	}
	at, _, _, _ = glow.ShaderUniform("Count")
	if word(at) != 3 {
		t.Fatalf("Count %d", word(at))
	}
	h.Reset()
	tint.Set(colors.Hex(0x38bdf8)) // the same: nothing sent
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatal("unchanged uniforms were sent")
	}
	tint.Set(colors.Hex(0xff0000))
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 {
		t.Fatal("changed uniforms weren't sent")
	}
}

// What Android can't run is an error at Compile, on every platform.
func TestShaderAndroidLimits(t *testing.T) {
	if _, err := shader.Compile("package main\nvar N int\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\treturn vec4(float(N & 1))\n}\n"); err == nil {
		t.Fatal("a bitwise operation compiled")
	}
}
