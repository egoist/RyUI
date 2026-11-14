//go:build linux && !android

package linux

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/shader"
	"github.com/egoist/asuka/internal/shadercode"
	"github.com/egoist/asuka/internal/shaderir/glsl"
)

// Shaders on the GPU: one GL context of the display's draws every
// shader's frames, each into a texture of its own, a triangle over the
// whole of it, through a framebuffer. The texture becomes a GdkGLTexture
// (with a fence GTK waits on, so nothing waits for the GPU here), which
// GTK's renderer draws from its own context: they share their objects,
// so no frame is copied. When GTK lets go of a frame, its texture goes
// back to a pool the next frames draw into, as GtkGLArea's do.
//
// A texture's rows go down in GTK's eyes (GtkGLArea flips what GL draws
// to show it), so the program's position is GL's window position as it
// is: y counts from the texture's first row, the view's top.

var glr struct {
	tried bool
	ctx   Ptr
	es    bool

	vao, fbo, ubo uint32
	maxSize       int
	programs      map[string]*glProgram // by Kage source

	mu   sync.Mutex // free, which GTK's destroy notifications fill
	free []glFrame  // frames GTK let go of
	pool []glFrame  // their textures, to draw into again (their syncs deleted)

	frames  map[uintptr]glFrame // by the key a GdkTexture's destroy notification gets
	nextKey uintptr
	release uintptr // the GDestroyNotify
}

// glFrame is a texture a frame drew into, and the fence after it.
type glFrame struct {
	tex  uint32
	w, h int
	sync Ptr
}

// glProgram is a shader's program, linked, and where its lanes go in its
// uniform block.
type glProgram struct {
	id         uint32
	block      uint32 // its uniform block's index, or GLInvalidIndex
	step       int32  // asuka_step's location
	lanes      []int
	size       int
	failed     bool
	uniformBuf []byte
}

// glReady reports whether shaders draw with GL, making the context the
// first time.
func glReady() bool {
	if glr.tried {
		return glr.ctx != 0
	}
	glr.tried = true
	ctx, es, major, minor, err := NewGLContext()
	if err == nil && (es && major < 3 || !es && (major < 3 || major == 3 && minor < 3)) {
		ObjectUnref(ctx)
		err = fmt.Errorf("its GL is %d.%d", major, minor)
	}
	if err != nil {
		log.Printf("asuka: shaders run on the CPU: %v", err)
		return false
	}
	glr.ctx, glr.es = ctx, es
	glr.programs = map[string]*glProgram{}
	glr.frames = map[uintptr]glFrame{}
	glr.release = purego.NewCallback(func(key uintptr) uintptr {
		glr.mu.Lock()
		if f, ok := glr.frames[key]; ok {
			delete(glr.frames, key)
			glr.free = append(glr.free, f)
		}
		glr.mu.Unlock()
		return 0
	})
	withGL(func() {
		GL.GenVertexArrays(1, &glr.vao)
		GL.GenFramebuffers(1, &glr.fbo)
		GL.GenBuffers(1, &glr.ubo)
		var m int32
		GL.GetIntegerv(GLMaxTextureSize, &m)
		glr.maxSize = max(int(m), 1024)
	})
	return true
}

// withGL runs f with the shaders' context current, then makes current
// again whichever context was.
func withGL(f func()) {
	prev := GL.GLContextGetCurrent()
	GL.GLContextMakeCurrent(glr.ctx)
	f()
	if prev != 0 && prev != glr.ctx {
		GL.GLContextMakeCurrent(prev)
	} else if prev == 0 {
		GL.GLContextClearCurrent()
	}
}

// renderGL draws a frame of the shader c with the lanes in, w by ht
// pixels, and returns it as a GdkTexture (0 if it didn't draw).
func renderGL(c *shadercode.Code, in []uint32, w, ht int) (texture Ptr) {
	withGL(func() {
		reapFrames()
		p := glProgramOf(c)
		if p.failed {
			return
		}
		// A view larger than a texture can be renders at a fraction of its
		// pixels, which the program's positions are still.
		tw, th := w, ht
		if m := glr.maxSize; tw > m || th > m {
			f := min(float64(m)/float64(tw), float64(m)/float64(th))
			tw, th = max(1, min(m, int(float64(tw)*f))), max(1, min(m, int(float64(th)*f)))
		}
		f := takeFrame(tw, th)
		GL.BindFramebuffer(GLFramebuffer, glr.fbo)
		GL.FramebufferTexture2D(GLFramebuffer, GLColorAttachment0, GLTexture2D, f.tex, 0)
		if GL.CheckFramebufferStatus(GLFramebuffer) != GLFramebufferComplete {
			GL.DeleteTextures(1, &f.tex)
			log.Printf("asuka: a ui.Shader has no framebuffer to draw into")
			return
		}
		GL.Viewport(0, 0, int32(tw), int32(th))
		GL.Disable(GLBlend)
		GL.Disable(GLScissorTest)
		GL.Disable(GLDepthTest)
		GL.UseProgram(p.id)
		if p.step >= 0 {
			GL.Uniform2f(p.step, float32(w)/float32(tw), float32(ht)/float32(th))
		}
		if p.block != GLInvalidIndex && p.size > 0 {
			b := p.uniformBuf
			for i, o := range p.lanes {
				if i < len(in) {
					le.PutUint32(b[o:], in[i])
				}
			}
			GL.BindBuffer(GLUniformBuffer, glr.ubo)
			GL.BufferData(GLUniformBuffer, len(b), unsafe.Pointer(&b[0]), GLDynamicDraw)
			GL.BindBufferBase(GLUniformBuffer, 0, glr.ubo)
		}
		GL.BindVertexArray(glr.vao)
		GL.DrawArrays(GLTriangles, 0, 3)
		f.sync = GL.FenceSync(GLSyncGPUCommandsDone, 0)
		GL.Flush()

		b := GL.TextureBuilderNew()
		GL.TextureBuilderContext(b, glr.ctx)
		GL.TextureBuilderID(b, f.tex)
		GL.TextureBuilderWidth(b, int32(tw))
		GL.TextureBuilderHeight(b, int32(th))
		GL.TextureBuilderFormat(b, MemoryR8G8B8A8Premultiplied)
		GL.TextureBuilderSync(b, f.sync)
		glr.mu.Lock()
		glr.nextKey++
		key := glr.nextKey
		glr.frames[key] = f
		glr.mu.Unlock()
		texture = GL.TextureBuilderBuild(b, glr.release, key)
		ObjectUnref(b)
	})
	return texture
}

// reapFrames deletes the fences of the frames GTK let go of and pools
// their textures, keeping a few.
func reapFrames() {
	glr.mu.Lock()
	free := glr.free
	glr.free = nil
	glr.mu.Unlock()
	for _, f := range free {
		if f.sync != 0 {
			GL.DeleteSync(f.sync)
			f.sync = 0
		}
		glr.pool = append(glr.pool, f)
	}
	const keep = 8
	for len(glr.pool) > keep {
		GL.DeleteTextures(1, &glr.pool[0].tex)
		glr.pool = glr.pool[1:]
	}
}

// takeFrame is a texture of w by ht pixels to draw into: a pooled one of
// that size, or a new one.
func takeFrame(w, ht int) glFrame {
	for i, f := range glr.pool {
		if f.w == w && f.h == ht {
			glr.pool = append(glr.pool[:i], glr.pool[i+1:]...)
			return f
		}
	}
	f := glFrame{w: w, h: ht}
	GL.GenTextures(1, &f.tex)
	GL.BindTexture(GLTexture2D, f.tex)
	GL.TexParameteri(GLTexture2D, GLTextureMinFilter, GLLinear)
	GL.TexParameteri(GLTexture2D, GLTextureMagFilter, GLLinear)
	GL.TexParameteri(GLTexture2D, GLTextureWrapS, GLClampToEdge)
	GL.TexParameteri(GLTexture2D, GLTextureWrapT, GLClampToEdge)
	GL.TexImage2D(GLTexture2D, 0, GLRGBA8, int32(w), int32(ht), 0, GLRGBA, GLUnsignedByte, nil)
	GL.BindTexture(GLTexture2D, 0)
	return f
}

// glProgramOf is c's program, compiled and linked the first time its
// source draws.
func glProgramOf(c *shadercode.Code) *glProgram {
	if p := glr.programs[c.Source]; p != nil {
		return p
	}
	p := &glProgram{block: GLInvalidIndex, step: -1}
	glr.programs[c.Source] = p
	vs, fs, err := glslSources(c, glr.es)
	if err == nil {
		p.id, err = linkGL(vs, fs.src)
	}
	if err != nil {
		p.failed = true
		log.Printf("asuka: a ui.Shader doesn't run: %v", err)
		return p
	}
	p.lanes, p.size = fs.lanes, fs.size
	p.uniformBuf = make([]byte, max(p.size, 16))
	p.block = GL.GetUniformBlockIndex(p.id, glslBlock)
	if p.block != GLInvalidIndex {
		GL.UniformBlockBinding(p.id, p.block, 0)
	}
	p.step = GL.GetUniformLocation(p.id, "asuka_step")
	return p
}

const glslBlock = "AsukaUniforms"

type glslFragment struct {
	src   string
	lanes []int // each lane's offset in the uniform block
	size  int
}

// glslSources are c's vertex and fragment shaders for GL 3.3 or GL ES
// 3.0: the fragment shader's uniforms in a std140 block, its position
// GL's window position times asuka_step (1 but where the frame has fewer
// pixels than the view), and its colors clamped, premultiplied.
func glslSources(c *shadercode.Code, es bool) (vs string, fs glslFragment, err error) {
	p, err := shader.CompileFragment([]byte(c.Source))
	if err != nil {
		return "", fs, err
	}
	scale := -1
	for i, u := range c.Uniforms {
		if u.Name == shader.ScaleUniform {
			scale = i
		}
	}
	if scale < 0 {
		return "", fs, errors.New("shader: no scale uniform in the bridge")
	}
	head := "#version 330 core\n"
	if es {
		head = "#version 300 es\nprecision highp float;\nprecision highp int;\n"
	}
	lib := glsl.Fragment(p, "asuka_shade", glsl.Options{Block: glslBlock})
	fs.src = head + "\n" + lib + fmt.Sprintf(glslEntry, scale)
	fs.lanes, fs.size = glsl.UniformLanes(p)
	if len(fs.lanes) != c.Lanes {
		return "", fs, fmt.Errorf("shader: %d lanes, the code has %d", len(fs.lanes), c.Lanes)
	}
	return head + glslVertex, fs, nil
}

const glslVertex = `
void main() {
	vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
`

const glslEntry = `
uniform vec2 asuka_step;
out vec4 asuka_color;

void main() {
	vec4 p = vec4(gl_FragCoord.xy * asuka_step, gl_FragCoord.zw);
	vec4 c = clamp(asuka_shade(p, p.xy / U%d), 0.0, 1.0);
	asuka_color = vec4(min(c.rgb, vec3(c.a)), c.a);
}
`

// linkGL compiles and links a program.
func linkGL(vs, fs string) (uint32, error) {
	v, err := compileGL(GLVertexShader, vs)
	if err != nil {
		return 0, err
	}
	defer GL.DeleteShader(v)
	f, err := compileGL(GLFragmentShader, fs)
	if err != nil {
		return 0, err
	}
	defer GL.DeleteShader(f)
	p := GL.CreateProgram()
	GL.AttachShader(p, v)
	GL.AttachShader(p, f)
	GL.LinkProgram(p)
	var ok, n int32
	GL.GetProgramiv(p, GLLinkStatus, &ok)
	if ok == 0 {
		GL.GetProgramiv(p, GLInfoLogLength, &n)
		msg := GLString(n, func(size int32, length *int32, log *byte) { GL.GetProgramInfoLog(p, size, length, log) })
		GL.DeleteProgram(p)
		return 0, fmt.Errorf("GL doesn't link it: %s", msg)
	}
	return p, nil
}

func compileGL(kind uint32, src string) (uint32, error) {
	s := GL.CreateShader(kind)
	cs := CString(src)
	n := int32(len(src))
	GL.ShaderSource(s, 1, &cs, &n)
	runtime.KeepAlive(cs)
	GL.CompileShader(s)
	var ok, ln int32
	GL.GetShaderiv(s, GLCompileStatus, &ok)
	if ok == 0 {
		GL.GetShaderiv(s, GLInfoLogLength, &ln)
		msg := GLString(ln, func(size int32, length *int32, log *byte) { GL.GetShaderInfoLog(s, size, length, log) })
		GL.DeleteShader(s)
		return 0, fmt.Errorf("GL doesn't compile it: %s", msg)
	}
	return s, nil
}
