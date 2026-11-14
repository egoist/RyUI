//go:build darwin && !ios

package darwin

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// writeGIF writes an animated GIF of frames solid colors, 20×10, each
// shown for 2 hundredths of a second, looping forever.
func writeGIF(t *testing.T, frames int) string {
	g := &gif.GIF{LoopCount: 0}
	pal := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}, color.RGBA{0, 255, 0, 255}}
	for i := range frames {
		img := image.NewPaletted(image.Rect(0, 0, 20, 10), pal)
		for p := range img.Pix {
			img.Pix[p] = uint8(i % len(pal))
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 2)
	}
	p := filepath.Join(t.TempDir(), "a.gif")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := gif.EncodeAll(f, g); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

// An animated GIF plays through ImageIO (the frame it shows moves on),
// reports Animated after Loaded, and stops where it is when paused; a
// hash placeholder shows in a layer of its own until the image loads,
// then fades out.
func TestAnimatedImage(t *testing.T) {
	path := writeGIF(t, 3)
	playing := signal.New(true)
	src := signal.New("")
	var loaded [2]float32
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(8)},
			ui.Image(ui.ImageProps{Source: src, Animated: playing, BlurHash: ui.Val("LEHV6nWB2yk8pyo0adR*.7kCMdnj"),
				OnLoad: func(w, h float32) { loaded = [2]float32{w, h} }, Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(40)}}),
		)
	})
	var n *node
	onMain(func() {
		n = find(h, ops.KindImage)[0]
		if n.imgx == nil || n.imgx.hash == 0 || n.imgx.layer == 0 {
			fatal(t, "no placeholder layer before the image loads")
		}
		h.enter(func() { ui.Post(func() { src.Set(path) }) })
		h.runPosted()
	})
	onMain(func() {
		if !pumpUntil(func() bool { return n.loaded }) {
			fatal(t, "the GIF didn't load")
		}
		if loaded != [2]float32{20, 10} || n.imgx.anim == nil {
			fatalf(t, "loaded %v, animated %v", loaded, n.imgx.anim != nil)
		}
		start := n.imgx.frame
		if !pumpUntil(func() bool { return n.imgx.frame != start }) {
			fatal(t, "the GIF's frames don't move on")
		}
		h.enter(func() { ui.Post(func() { playing.Set(false) }) })
		h.runPosted()
		if !n.imgx.paused {
			fatal(t, "not paused")
		}
		at := n.imgx.frame
		k := 0
		pumpUntil(func() bool { k++; return k > 30 }) // several frames' time passes
		if n.imgx.frame != at {
			fatalf(t, "paused at %d, now at %d", at, n.imgx.frame)
		}
		if !pumpUntil(func() bool { return n.imgx.layer == 0 }) {
			fatal(t, "the placeholder stays")
		}
	})
}

// A Placeholder image (a file) shows in the placeholder's layer while the
// source loads, in its own mode.
func TestImagePlaceholderFile(t *testing.T) {
	ph := writeGIF(t, 1)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(8)},
			ui.Image(ui.ImageProps{Source: ui.Val("https://localhost:1/never.png"), Placeholder: ph, Mode: ui.ImageFill,
				PlaceholderMode: ui.PlaceholderFit, Style: ui.Style{Width: ui.Pt(80), Height: ui.Pt(40)}}),
		)
	})
	onMain(func() {
		n := find(h, ops.KindImage)[0]
		if !pumpUntil(func() bool { return n.imgx != nil && n.imgx.layer != 0 && n.imgx.shown != 0 }) {
			fatal(t, "the placeholder doesn't show")
		}
	})
}
