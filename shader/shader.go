// Package shader compiles the shaders ui.Shader draws: fragment shaders in
// Go's syntax, Ebitengine's Kage language (through MyGo's port of its
// compiler), which RyUI translates for each platform: Metal on iOS and
// macOS, AGSL on Android, and package interp's CPU on Windows.
//
// A shader is the source of a Go file of package main whose function
// Fragment returns the color of a pixel, premultiplied, from its position:
//
//	package main
//
//	var Time float // seconds since it showed: the host runs it (ShaderProps.Animated)
//	var Tint vec4  // set with ShaderProps.Uniforms
//
//	func Fragment(dst vec4, pos vec2) vec4 {
//		d := distance(pos/rectSize(), vec2(0.5))
//		glow := smoothstep(0.5, 0, d) * (0.8 + 0.2*sin(Time*2))
//		return vec4(Tint.rgb*glow, 1) * Tint.a
//	}
//
// pos is the pixel's center in points from the box's top left corner, dst
// the same in pixels, rectSize the box's size in points and pixelScale the
// pixels a point. The language has the types bool, int, float, vec2 to
// vec4, ivec2 to ivec4, mat2 to mat4 and arrays of them, GLSL's built-in
// functions (sin, mix, smoothstep, dot...), functions of its own, if, and
// for loops; exported variables at the package level are uniforms.
//
// Android's language, AGSL, is GLSL ES 1.00's: Compile reports what of a
// shader it can't run (unsigned integers, bitwise operations, derivatives,
// loops between values that aren't constants), on every platform alike.
package shader

import (
	"github.com/egoist/ryui/internal/shadercode"
)

// Program is a compiled shader, which ui.Shader draws.
type Program struct {
	code     []byte
	lanes    int
	uniforms map[string]shadercode.Uniform
}

// Compile compiles a shader's source, reporting its errors with their
// lines. Compile a shader once (a package's variable, MustCompile), not
// each time a view is made.
func Compile(src string) (*Program, error) {
	c, err := shadercode.Compile(src)
	if err != nil {
		return nil, err
	}
	p := &Program{code: c.Encode(), lanes: c.Lanes, uniforms: map[string]shadercode.Uniform{}}
	for _, u := range c.Uniforms {
		p.uniforms[u.Name] = u
	}
	return p, nil
}

// MustCompile is Compile for a shader known to compile, as a package's
// variable: it panics when it doesn't.
func MustCompile(src string) *Program {
	p, err := Compile(src)
	if err != nil {
		panic(err)
	}
	return p
}

// ShaderCode is the program as the hosts take it (ui.ShaderProgram).
func (p *Program) ShaderCode() []byte { return p.code }

// ShaderLanes is how many 32-bit lanes its uniforms' values take.
func (p *Program) ShaderLanes() int { return p.lanes }

// ShaderUniform says where the uniform variable name's lanes are, and
// their kind: 0 float, 1 int, 2 uint, 3 bool.
func (p *Program) ShaderUniform(name string) (lane, lanes, kind int, ok bool) {
	u, ok := p.uniforms[name]
	return u.Lane, u.Lanes, int(u.Kind), ok
}
