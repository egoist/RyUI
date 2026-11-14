//go:build linux && !android

package linux

import (
	"encoding/binary"
	stdimage "image"
	"time"

	"github.com/egoist/asuka/internal/animimage"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// An Image's placeholder, arrival and frames. The placeholder (the core's
// decoded hash, else the Placeholder image) is drawn under the image in
// its own mode until the image arrives, which fades in over it on the
// widget's frame clock (Transition; 0.25s over a placeholder). An
// animated GIF, APNG or WebP (its frames' pictures by GDK's loaders)
// plays here, frame after frame on GLib timeouts (internal/animimage),
// while it isn't Paused.

type imageExtra struct {
	hash     Ptr // PlaceholderImage's texture (a reference)
	ph       Ptr // the placeholder showing (the hash's or the asset's; a reference)
	phAsset  bool
	phMode   int32 // PlaceholderMode; -1: the image's
	trans    float64
	fade     float32 // the image's opacity as it arrives
	fadeGen  int
	paused   bool
	player   *animimage.Player
	frameGen int
	wait     time.Duration
}

func (n *node) imx() *imageExtra {
	if n.imgx == nil {
		n.imgx = &imageExtra{phMode: -1, fade: 1}
	}
	return n.imgx
}

func (x *imageExtra) setPlaceholder(t Ptr, asset bool) {
	if x.ph == t {
		x.phAsset = asset
		return
	}
	if x.ph != 0 {
		ObjectUnref(x.ph)
	}
	x.ph, x.phAsset = t, asset
	if t != 0 {
		ObjectRef(t)
	}
}

// release lets go of the textures the view holds and stops its timers.
func (x *imageExtra) release() {
	if x == nil {
		return
	}
	x.fadeGen++
	x.frameGen++
	x.setPlaceholder(0, false)
	if x.hash != 0 {
		ObjectUnref(x.hash)
		x.hash = 0
	}
}

// rgbaTexture is a texture (a new reference) of RGBA pixels, not
// premultiplied.
func rgbaTexture(w, h int, px []byte) Ptr {
	if w <= 0 || h <= 0 || len(px) < w*h*4 {
		return 0
	}
	b := BytesNew(&px[0], uintptr(w*h*4))
	t := MemoryTextureNew(int32(w), int32(h), MemoryR8G8B8A8, b, uintptr(w*4))
	BytesUnref(b)
	return t
}

// hashTexture is PlaceholderImage's texture (0: none).
func hashTexture(b []byte) Ptr {
	if len(b) < 8 {
		return 0
	}
	w, h := int(binary.LittleEndian.Uint32(b)), int(binary.LittleEndian.Uint32(b[4:]))
	if w <= 0 || h <= 0 || len(b) < 8+w*h*4 {
		return 0
	}
	return rgbaTexture(w, h, b[8:8+w*h*4])
}

// gdkStill decodes a still image file with GDK's loaders (a WebP frame).
// Main thread.
func gdkStill(data []byte) (stdimage.Image, error) {
	if len(data) == 0 {
		return nil, animimage.ErrNotAnimated
	}
	var gerr Ptr
	b := BytesNew(&data[0], uintptr(len(data)))
	t := TextureNewFromBytes(b, &gerr)
	BytesUnref(b)
	if t == 0 {
		if gerr != 0 {
			ErrorFree(gerr)
		}
		return nil, animimage.ErrNotAnimated
	}
	defer ObjectUnref(t)
	w, h := int(TextureGetWidth(t)), int(TextureGetHeight(t))
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	if w == 0 || h == 0 {
		return img, nil
	}
	Draw.TextureDownload(t, &img.Pix[0], uintptr(img.Stride)) // B, G, R, A, premultiplied
	px := img.Pix
	for i := 0; i < len(px); i += 4 {
		b, g, r, a := px[i], px[i+1], px[i+2], px[i+3]
		if a != 0 && a != 255 {
			r, g, b = uint8(uint32(r)*255/uint32(a)), uint8(uint32(g)*255/uint32(a)), uint8(uint32(b)*255/uint32(a))
		}
		px[i], px[i+1], px[i+2], px[i+3] = r, g, b, a
	}
	return img, nil
}

func (h *Host) setImageExtraProp(n *node, p ops.PropID, v *ops.Value) {
	x := n.imx()
	switch p {
	case ops.PropImagePlaceholderImage:
		if x.hash != 0 {
			ObjectUnref(x.hash)
		}
		x.hash = hashTexture(v.Bytes)
		h.showPlaceholder(n)
	case ops.PropImagePlaceholderMode:
		x.phMode = int32(v.Int)
		queueDraw(n)
	case ops.PropImageTransition:
		x.trans = v.Float
	case ops.PropImagePaused:
		x.paused = v.Int != 0
		if x.paused {
			x.frameGen++
		} else {
			h.nextFrame(n, x.wait)
		}
	}
}

// showPlaceholder shows the image's placeholder while it hasn't loaded:
// the hash's, else the Placeholder image's.
func (h *Host) showPlaceholder(n *node) {
	if n.loaded {
		return
	}
	x := n.imx()
	if x.hash != 0 {
		x.setPlaceholder(x.hash, false)
		queueDraw(n)
		return
	}
	x.setPlaceholder(0, false)
	queueDraw(n)
	h.loadImage(n, n.imagePlaceholder, true)
}

// arrive fades the image in over its placeholder, or from nothing.
func (h *Host) arrive(n *node) {
	x := n.imx()
	d := x.trans
	switch {
	case d < 0:
		d = 0
	case d == 0 && x.ph != 0:
		d = 0.25
	}
	x.fadeGen++
	if d <= 0 || n.widget == 0 || !WidgetGetMapped(n.widget) {
		x.fade = 1
		x.setPlaceholder(0, false)
		return
	}
	x.fade = 0
	gen, id, start := x.fadeGen, n.id, time.Now()
	AddTickCallback(n.widget, func(Ptr) bool {
		n := h.nodes[id]
		if n == nil || n.imgx == nil || n.imgx.fadeGen != gen {
			return false
		}
		t := float32(time.Since(start).Seconds() / d)
		if t >= 1 {
			n.imgx.fade = 1
			n.imgx.setPlaceholder(0, false)
		} else {
			n.imgx.fade = t
		}
		queueDraw(n)
		return t < 1
	})
}

// startFrames plays an animation that loaded, from its first frame.
func (h *Host) startFrames(n *node, a *animimage.Animation) {
	x := n.imx()
	x.player = animimage.NewPlayer(a)
	x.player.Next()
	x.wait = a.Frames[0].Delay
	if !x.paused {
		h.nextFrame(n, x.wait)
	}
}

// nextFrame shows the animation's next frame after wait, and the one
// after it after its time, while it plays.
func (h *Host) nextFrame(n *node, wait time.Duration) {
	x := n.imx()
	x.frameGen++
	if x.player == nil || x.paused || !n.loaded {
		return
	}
	gen, id := x.frameGen, n.id
	TimeoutAdd(uint32(max(wait.Milliseconds(), 1)), func() bool {
		n := h.nodes[id]
		if n == nil || n.imgx == nil || n.imgx.frameGen != gen {
			return false
		}
		x := n.imgx
		d, done := x.player.Next()
		if done {
			return false
		}
		c := x.player.Canvas()
		t := rgbaTexture(c.Rect.Dx(), c.Rect.Dy(), c.Pix)
		h.setTexture(n, t)
		if t != 0 {
			ObjectUnref(t) // setTexture holds it
		}
		x.wait = d
		h.nextFrame(n, d)
		return false
	})
}
