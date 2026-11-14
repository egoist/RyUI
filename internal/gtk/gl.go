//go:build linux && !android

package gtk

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// OpenGL, as host/linux's shaders draw with it: a GdkGLContext of the
// display's, which shares its objects with GTK's renderer, GL's
// functions from libepoxy (which GTK draws through too, so it is
// loaded), and GdkGLTextureBuilder, which makes a GdkTexture of a GL
// texture the context drew. LoadGL binds them on first use.
var GL struct {
	// GDK.
	DisplayCreateGLContext func(display Ptr, err *Ptr) Ptr
	GLContextRealize       func(ctx Ptr, err *Ptr) bool
	GLContextMakeCurrent   func(ctx Ptr)
	GLContextGetCurrent    func() Ptr
	GLContextClearCurrent  func()
	GLContextGetUseES      func(ctx Ptr) bool
	GLContextGetVersion    func(ctx Ptr, major, minor *int32)
	TextureBuilderNew      func() Ptr
	TextureBuilderContext  func(b, ctx Ptr)
	TextureBuilderID       func(b Ptr, id uint32)
	TextureBuilderWidth    func(b Ptr, w int32)
	TextureBuilderHeight   func(b Ptr, h int32)
	TextureBuilderFormat   func(b Ptr, format uint32)
	TextureBuilderSync     func(b Ptr, sync Ptr)
	TextureBuilderBuild    func(b, destroy, data Ptr) Ptr

	// GL.
	CreateShader           func(kind uint32) uint32
	ShaderSource           func(shader uint32, count int32, sources **byte, lengths *int32)
	CompileShader          func(shader uint32)
	GetShaderiv            func(shader, name uint32, out *int32)
	GetShaderInfoLog       func(shader uint32, size int32, length *int32, log *byte)
	DeleteShader           func(shader uint32)
	CreateProgram          func() uint32
	AttachShader           func(program, shader uint32)
	LinkProgram            func(program uint32)
	GetProgramiv           func(program, name uint32, out *int32)
	GetProgramInfoLog      func(program uint32, size int32, length *int32, log *byte)
	DeleteProgram          func(program uint32)
	UseProgram             func(program uint32)
	GetUniformBlockIndex   func(program uint32, name string) uint32
	UniformBlockBinding    func(program, index, binding uint32)
	GetUniformLocation     func(program uint32, name string) int32
	Uniform2f              func(location int32, x, y float32)
	GenBuffers             func(n int32, out *uint32)
	BindBuffer             func(target, buffer uint32)
	BufferData             func(target uint32, size int, data unsafe.Pointer, usage uint32)
	BindBufferBase         func(target, index, buffer uint32)
	DeleteBuffers          func(n int32, buffers *uint32)
	GenVertexArrays        func(n int32, out *uint32)
	BindVertexArray        func(array uint32)
	GenTextures            func(n int32, out *uint32)
	BindTexture            func(target, texture uint32)
	TexImage2D             func(target uint32, level, internalFormat, w, h, border int32, format, kind uint32, pix unsafe.Pointer)
	TexParameteri          func(target, name uint32, v int32)
	DeleteTextures         func(n int32, textures *uint32)
	GenFramebuffers        func(n int32, out *uint32)
	BindFramebuffer        func(target, fb uint32)
	FramebufferTexture2D   func(target, attachment, texTarget, texture uint32, level int32)
	CheckFramebufferStatus func(target uint32) uint32
	DeleteFramebuffers     func(n int32, fbs *uint32)
	Viewport               func(x, y, w, h int32)
	Disable                func(cap uint32)
	DrawArrays             func(mode uint32, first, count int32)
	GetIntegerv            func(name uint32, out *int32)
	FenceSync              func(condition, flags uint32) Ptr
	DeleteSync             func(sync Ptr)
	Flush                  func()
	GetError               func() uint32
}

// GL's enums the shaders use.
const (
	GLFragmentShader      = 0x8B30
	GLVertexShader        = 0x8B31
	GLCompileStatus       = 0x8B81
	GLLinkStatus          = 0x8B82
	GLInfoLogLength       = 0x8B84
	GLUniformBuffer       = 0x8A11
	GLDynamicDraw         = 0x88E8
	GLTexture2D           = 0x0DE1
	GLTextureMinFilter    = 0x2801
	GLTextureMagFilter    = 0x2800
	GLTextureWrapS        = 0x2802
	GLTextureWrapT        = 0x2803
	GLLinear              = 0x2601
	GLClampToEdge         = 0x812F
	GLRGBA                = 0x1908
	GLRGBA8               = 0x8058
	GLUnsignedByte        = 0x1401
	GLFramebuffer         = 0x8D40
	GLColorAttachment0    = 0x8CE0
	GLFramebufferComplete = 0x8CD5
	GLTriangles           = 0x0004
	GLBlend               = 0x0BE2
	GLScissorTest         = 0x0C11
	GLDepthTest           = 0x0B71
	GLMaxTextureSize      = 0x0D33
	GLSyncGPUCommandsDone = 0x9117
	GLInvalidIndex        = 0xFFFFFFFF
)

var (
	glOnce sync.Once
	glErr  error
)

// LoadGL binds GL's functions; it fails when GTK has no GL (libepoxy
// isn't there, or GDK is too old to build GL textures).
func LoadGL() error {
	glOnce.Do(func() { glErr = loadGL() })
	return glErr
}

func loadGL() error {
	g := &GL
	var missing []string
	gdk := func(f any, name string) {
		if p := Sym(name); p != 0 {
			purego.RegisterFunc(f, p)
		} else {
			missing = append(missing, name)
		}
	}
	gdk(&g.DisplayCreateGLContext, "gdk_display_create_gl_context")
	gdk(&g.GLContextRealize, "gdk_gl_context_realize")
	gdk(&g.GLContextMakeCurrent, "gdk_gl_context_make_current")
	gdk(&g.GLContextGetCurrent, "gdk_gl_context_get_current")
	gdk(&g.GLContextClearCurrent, "gdk_gl_context_clear_current")
	gdk(&g.GLContextGetUseES, "gdk_gl_context_get_use_es")
	gdk(&g.GLContextGetVersion, "gdk_gl_context_get_version")
	gdk(&g.TextureBuilderNew, "gdk_gl_texture_builder_new")
	gdk(&g.TextureBuilderContext, "gdk_gl_texture_builder_set_context")
	gdk(&g.TextureBuilderID, "gdk_gl_texture_builder_set_id")
	gdk(&g.TextureBuilderWidth, "gdk_gl_texture_builder_set_width")
	gdk(&g.TextureBuilderHeight, "gdk_gl_texture_builder_set_height")
	gdk(&g.TextureBuilderFormat, "gdk_gl_texture_builder_set_format")
	gdk(&g.TextureBuilderSync, "gdk_gl_texture_builder_set_sync")
	gdk(&g.TextureBuilderBuild, "gdk_gl_texture_builder_build")
	if len(missing) > 0 {
		return fmt.Errorf("gtk: no GL textures in this GTK (%s)", missing[0])
	}

	lib, err := purego.Dlopen("libepoxy.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("gtk: no libepoxy: %w", err)
	}
	// libepoxy exports a pointer for each GL function, to a stub that
	// finds the function of the context current when it's called.
	gl := func(f any, name string) {
		p, err := purego.Dlsym(lib, "epoxy_"+name)
		if err != nil || p == 0 || *At[uintptr](p) == 0 {
			missing = append(missing, name)
			return
		}
		purego.RegisterFunc(f, *At[uintptr](p))
	}
	gl(&g.CreateShader, "glCreateShader")
	gl(&g.ShaderSource, "glShaderSource")
	gl(&g.CompileShader, "glCompileShader")
	gl(&g.GetShaderiv, "glGetShaderiv")
	gl(&g.GetShaderInfoLog, "glGetShaderInfoLog")
	gl(&g.DeleteShader, "glDeleteShader")
	gl(&g.CreateProgram, "glCreateProgram")
	gl(&g.AttachShader, "glAttachShader")
	gl(&g.LinkProgram, "glLinkProgram")
	gl(&g.GetProgramiv, "glGetProgramiv")
	gl(&g.GetProgramInfoLog, "glGetProgramInfoLog")
	gl(&g.DeleteProgram, "glDeleteProgram")
	gl(&g.UseProgram, "glUseProgram")
	gl(&g.GetUniformBlockIndex, "glGetUniformBlockIndex")
	gl(&g.UniformBlockBinding, "glUniformBlockBinding")
	gl(&g.GetUniformLocation, "glGetUniformLocation")
	gl(&g.Uniform2f, "glUniform2f")
	gl(&g.GenBuffers, "glGenBuffers")
	gl(&g.BindBuffer, "glBindBuffer")
	gl(&g.BufferData, "glBufferData")
	gl(&g.BindBufferBase, "glBindBufferBase")
	gl(&g.DeleteBuffers, "glDeleteBuffers")
	gl(&g.GenVertexArrays, "glGenVertexArrays")
	gl(&g.BindVertexArray, "glBindVertexArray")
	gl(&g.GenTextures, "glGenTextures")
	gl(&g.BindTexture, "glBindTexture")
	gl(&g.TexImage2D, "glTexImage2D")
	gl(&g.TexParameteri, "glTexParameteri")
	gl(&g.DeleteTextures, "glDeleteTextures")
	gl(&g.GenFramebuffers, "glGenFramebuffers")
	gl(&g.BindFramebuffer, "glBindFramebuffer")
	gl(&g.FramebufferTexture2D, "glFramebufferTexture2D")
	gl(&g.CheckFramebufferStatus, "glCheckFramebufferStatus")
	gl(&g.DeleteFramebuffers, "glDeleteFramebuffers")
	gl(&g.Viewport, "glViewport")
	gl(&g.Disable, "glDisable")
	gl(&g.DrawArrays, "glDrawArrays")
	gl(&g.GetIntegerv, "glGetIntegerv")
	gl(&g.FenceSync, "glFenceSync")
	gl(&g.DeleteSync, "glDeleteSync")
	gl(&g.Flush, "glFlush")
	gl(&g.GetError, "glGetError")
	if len(missing) > 0 {
		return fmt.Errorf("gtk: libepoxy has no %s", missing[0])
	}
	return nil
}

// NewGLContext makes and realizes a GL context of the default display's,
// for drawing into textures, and reports whether it's OpenGL ES and its
// version.
func NewGLContext() (ctx Ptr, es bool, major, minor int, err error) {
	if err := LoadGL(); err != nil {
		return 0, false, 0, 0, err
	}
	var gerr Ptr
	ctx = GL.DisplayCreateGLContext(DisplayGetDefault(), &gerr)
	if ctx == 0 {
		return 0, false, 0, 0, glError(gerr, "GDK has no GL")
	}
	if !GL.GLContextRealize(ctx, &gerr) {
		ObjectUnref(ctx)
		return 0, false, 0, 0, glError(gerr, "GDK's GL context doesn't realize")
	}
	var ma, mi int32
	GL.GLContextGetVersion(ctx, &ma, &mi)
	return ctx, GL.GLContextGetUseES(ctx), int(ma), int(mi), nil
}

func glError(gerr Ptr, what string) error {
	if gerr != 0 {
		return GError(gerr)
	}
	return errors.New("gtk: " + what)
}

// GLString is a GL info log of n bytes, read by get.
func GLString(n int32, get func(size int32, length *int32, log *byte)) string {
	if n <= 1 {
		return ""
	}
	b := make([]byte, n)
	var got int32
	get(n, &got, &b[0])
	return string(b[:max(0, min(got, n))])
}
