//go:build windows

package windows

import (
	"testing"
	"time"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	kage "github.com/egoist/asuka/sdk/shader"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var tint = kage.MustCompile(`package main

var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	if pos.x < rectSize().x/2 && pos.y < rectSize().y/2 {
		return vec4(Tint.rgb, 1) * Tint.a
	}
	return vec4(0)
}
`)

// pumpUntil runs the thread's messages until ok (the frames the shader's
// goroutines post back).
func pumpUntil(ok func() bool) bool {
	var msg Msg
	// A safety net, not what passes: long enough for a slow machine (a CI
	// runner's two cores) to render off the main thread.
	deadline := time.Now().Add(60 * time.Second)
	for !ok() && time.Now().Before(deadline) {
		if PeekMessage(&msg) {
			TranslateMessage(&msg)
			DispatchMessage(&msg)
		} else {
			time.Sleep(time.Millisecond) // the work comes from another thread
		}
	}
	return ok()
}

func TestShader(t *testing.T) {
	color := signal.New(colors.Hex(0xff0000))
	h, r := mount(t, func() ui.Node {
		return ui.Shader(ui.ShaderProps{Program: tint,
			Uniforms: signal.Func(func() []ui.Uniform { return []ui.Uniform{ui.ColorUniform("Tint", color.Get())} }),
			Style:    ui.Style{Width: ui.Pt(40), Height: ui.Pt(30)}})
	})
	onMain(t, func() {
		ns := find(h, ops.KindShader)
		if len(ns) != 1 || ns[0].kage == nil || ns[0].kage.code == nil {
			fatalf("no shader program: %v", ns)
		}
		s := ns[0].kage
		// BGRA, premultiplied: the top left quarter the tint, the rest
		// clear, at the view's pixels.
		px := func(x, y int) [4]byte {
			o := (y*s.pw + x) * 4
			return [4]byte(s.pix[o : o+4])
		}
		if !pumpUntil(func() bool { return s.bmp != nil }) {
			fatalf("no frame")
		}
		d3d.mu.Lock()
		t.Logf("Direct3D 11, feature level %#x, WARP %v", d3d.level, d3d.software)
		d3d.mu.Unlock()
		if got := px(0, 0); got != [4]byte{0, 0, 255, 255} {
			fatalf("left pixel %v", got)
		}
		if got := px(s.pw-1, 0); got != [4]byte{} {
			fatalf("right pixel %v", got)
		}
		if got := px(0, s.ph-1); got != [4]byte{} {
			fatalf("bottom pixel %v", got)
		}
		if k := h.scaleOf(ns[0]); s.pw != int(40*k) || s.ph != int(30*k) {
			fatalf("a frame of %dx%d", s.pw, s.ph)
		}
		h.enter(func() { ui.Post(func() { color.Set(colors.Hex(0x0000ff)) }) }) // flushed as the app's code is
		h.runPosted()
		if !pumpUntil(func() bool { return px(0, 0) == [4]byte{255, 0, 0, 255} }) {
			fatalf("after a change: %v", px(0, 0))
		}
		if s.busy {
			fatalf("still rendering with nothing changed")
		}
		h.enter(r.Dispose)
		if s.bmp != nil {
			fatalf("the frame outlives the view")
		}
	})
}
