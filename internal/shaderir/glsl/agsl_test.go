package glsl_test

import (
	"strings"
	"testing"

	"github.com/egoist/ryui/internal/shader"
	"github.com/egoist/ryui/internal/shaderir/glsl"
)

const glow = `package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	d := distance(pos/rectSize(), vec2(0.5))
	g := smoothstep(0.5, 0, d) * (0.8 + 0.2*sin(Time*2))
	for i := 0; i < 3; i++ {
		g *= 1.01
	}
	return vec4(Tint.rgb*g, 1) * Tint.a
}
`

// A shader in AGSL: its uniforms, its functions and main, with its numbers
// typed (2.0, not 2), which AGSL needs.
func TestAGSL(t *testing.T) {
	p, err := shader.CompileFragment([]byte(glow))
	if err != nil {
		t.Fatal(err)
	}
	code, err := glsl.AGSL(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"uniform float U2;", "half4 main(float2 p)", "p / U2", "smoothstep(", "for (int"} {
		if !strings.Contains(code, want) {
			t.Errorf("no %q in\n%s", want, code)
		}
	}
	if strings.Contains(code, "sampler2D") || strings.Contains(code, "precision") {
		t.Errorf("GLSL's own in\n%s", code)
	}
	t.Log(code)
}

// What AGSL can't run is an error.
func TestAGSLRejects(t *testing.T) {
	for name, src := range map[string]string{
		"unsigned": "package main\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\tvar u uint = 3\n\treturn vec4(float(u))\n}\n",
		"bitwise":  "package main\nvar N int\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\treturn vec4(float(N & 1))\n}\n",
		"image":    "package main\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\treturn image0At(pos)\n}\n",
		"loop":     "package main\nvar N int\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\tv := 0.0\n\tfor i := 0; i < N; i++ {\n\t\tv += 0.1\n\t}\n\treturn vec4(v)\n}\n",
	} {
		p, err := shader.CompileFragment([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := glsl.AGSL(p, 2); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
