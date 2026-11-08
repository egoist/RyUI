//go:build js && wasm

package shadercode

import "github.com/egoist/ryui/internal/boot"

// The web host draws shaders with WebGL: an app with a shader links this
// package, and with it the GLSL its host asks for.
func init() { boot.ShaderGLSL = GLSL }
