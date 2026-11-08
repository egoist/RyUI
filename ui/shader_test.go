package ui_test

import (
	"testing"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// A shader sends its source once, and its uniforms when they change.
func TestShader(t *testing.T) {
	tint := signal.New(ui.Hex(0x38bdf8))
	h, r := mount(t, func() ui.Node {
		return ui.Shader(ui.ShaderProps{
			Source:   "float4 shade(float2 uv) { return tint; }",
			Uniforms: signal.Func(func() []ui.Uniform { return []ui.Uniform{ui.ColorUniform("tint", tint.Get())} }),
			Animated: true,
			Style:    ui.Style{Height: ui.Pt(100)},
		})
	})
	s := h.OfKind(ops.KindShader)[0]
	if s.String(ops.PropShaderSource) == "" || !s.Bool(ops.PropShaderAnimated) || len(s.Props[ops.PropShaderUniforms].Bytes) != 4+4+4+16 {
		t.Fatalf("shader props %v", s.Props)
	}
	h.Reset()
	tint.Set(ui.Hex(0x38bdf8)) // the same: nothing sent
	r.Flush()
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatal("an unchanged uniform was sent")
	}
	tint.Set(ui.Hex(0xff0000))
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 {
		t.Fatal("a changed uniform wasn't sent")
	}
}
