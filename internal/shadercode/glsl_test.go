package shadercode

import (
	"strings"
	"testing"
)

func TestGLSL(t *testing.T) {
	c, err := Compile(`package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	d := distance(pos/rectSize(), vec2(0.5))
	return vec4(Tint.rgb*smoothstep(0.5, 0, d)*(0.8+0.2*sin(Time*2)), 1) * Tint.a
}
`)
	if err != nil {
		t.Fatal(err)
	}
	src, err := GLSL(c.Encode())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#version 300 es", "vec4 asuka_shade(", "out vec4 asuka_color;", "void main()"} {
		if !strings.Contains(src, want) {
			t.Errorf("GLSL lacks %q:\n%s", want, src)
		}
	}
}
