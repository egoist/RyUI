//go:build linux && !android

package linux

import (
	"bytes"
	"fmt"
	stdimage "image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// Images are fetched and decoded off the main thread (Go's decoders:
// PNG, JPEG, GIF; others by GDK's loaders, on the main thread), kept in
// memory by source as GDK textures, and drawn by the node's view at its
// mode, clipped to its corners; Color tints them (a mask).

type decoded struct {
	w, h    int32
	pixels  []byte // RGBA, not premultiplied
	data    []byte // the file, for GDK's loaders when Go has no decoder
	texture Ptr
	err     error
}

var (
	imageMu    sync.Mutex
	imageCache = map[string]*decoded{}
	imageWait  = map[string][]func(*decoded){}
)

func (h *Host) setImageProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropImageSource:
		n.source = v.Str
		n.loaded = false
		h.loadImage(n, v.Str, false)
	case ops.PropImagePlaceholder:
		n.imagePlaceholder = v.Str
		if !n.loaded {
			h.loadImage(n, v.Str, true)
		}
	case ops.PropImageMode:
		n.imageMode = int32(v.Int)
		queueDraw(n)
	}
}

// loadImage shows src in n once it's decoded, and reports Loaded (its
// natural size) or Failed.
func (h *Host) loadImage(n *node, src string, placeholder bool) {
	if src == "" {
		if !placeholder {
			h.setTexture(n, 0)
		}
		return
	}
	id := n.id
	h.fetchImage(src, func(d *decoded) {
		n := h.nodes[id]
		if n == nil {
			return
		}
		if placeholder && (n.loaded || n.imagePlaceholder != src) || !placeholder && n.source != src {
			return
		}
		if d.err != nil {
			if !placeholder {
				h.emit(n, ops.Event{Kind: ops.EventFailed, Str: d.err.Error()})
			}
			return
		}
		h.setTexture(n, d.texture)
		if !placeholder {
			n.loaded = true
			h.emit(n, ops.Event{Kind: ops.EventLoaded, X: float64(d.w), Y: float64(d.h)})
		}
	})
}

func (h *Host) setTexture(n *node, t Ptr) {
	if n.texture == t {
		return
	}
	if n.texture != 0 {
		ObjectUnref(n.texture)
	}
	n.texture = t
	if t != 0 {
		ObjectRef(t)
	}
	queueDraw(n)
}

// fetchImage calls done on the main thread with src decoded, from the
// cache or a goroutine.
func (h *Host) fetchImage(src string, done func(*decoded)) {
	imageMu.Lock()
	if d := imageCache[src]; d != nil {
		imageMu.Unlock()
		done(d)
		return
	}
	waiting := imageWait[src]
	imageWait[src] = append(waiting, done)
	imageMu.Unlock()
	if len(waiting) > 0 {
		return
	}
	assets := h.assets
	go func() {
		d := decodeImage(src, assets)
		h.Post(func() {
			d.makeTexture()
			imageMu.Lock()
			imageCache[src] = d
			fs := imageWait[src]
			delete(imageWait, src)
			imageMu.Unlock()
			for _, f := range fs {
				f(d)
			}
		})
	}()
}

// makeTexture makes the GDK texture of the decoded pixels (or decodes
// the file with GDK's loaders). Main thread.
func (d *decoded) makeTexture() {
	if d.err != nil || d.texture != 0 {
		return
	}
	if d.pixels != nil {
		b := BytesNew(&d.pixels[0], uintptr(len(d.pixels)))
		d.texture = MemoryTextureNew(d.w, d.h, MemoryR8G8B8A8, b, uintptr(d.w*4))
		BytesUnref(b)
		d.pixels = nil
		return
	}
	if len(d.data) > 0 {
		var gerr Ptr
		b := BytesNew(&d.data[0], uintptr(len(d.data)))
		d.texture = TextureNewFromBytes(b, &gerr)
		BytesUnref(b)
		d.data = nil
		if d.texture == 0 {
			d.err = fmt.Errorf("asuka: an image in a format there's no decoder for")
			if gerr != 0 {
				ErrorFree(gerr)
			}
			return
		}
		d.w, d.h = TextureGetWidth(d.texture), TextureGetHeight(d.texture)
	}
}

func readSource(src, assets string) ([]byte, error) {
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		resp, err := http.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("%s: %s", src, resp.Status)
		}
		return io.ReadAll(resp.Body)
	case strings.HasPrefix(src, "file://"):
		return os.ReadFile(strings.TrimPrefix(src, "file://"))
	}
	p := src
	if !filepath.IsAbs(p) {
		p = filepath.Join(assets, filepath.FromSlash(src))
	}
	return os.ReadFile(p)
}

func decodeImage(src, assets string) *decoded {
	data, err := readSource(src, assets)
	if err != nil {
		return &decoded{err: err}
	}
	img, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return &decoded{data: data} // GDK's loaders may know it (SVG, WebP...)
	}
	b := img.Bounds()
	rgba := stdimage.NewNRGBA(stdimage.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	if b.Dx() == 0 || b.Dy() == 0 {
		return &decoded{err: fmt.Errorf("%s: an empty image", src)}
	}
	return &decoded{w: int32(b.Dx()), h: int32(b.Dy()), pixels: rgba.Pix}
}

// drawImage draws a node's texture at its mode: fit, fill (cropped),
// stretched or centered at its size, clipped to the view's corners;
// Color tints it.
func (h *Host) drawImage(n *node, snap Ptr, bounds Rect) {
	t := n.texture
	if t == 0 {
		return
	}
	iw, ih := float32(TextureGetWidth(t)), float32(TextureGetHeight(t))
	if iw == 0 || ih == 0 || bounds.W == 0 || bounds.H == 0 {
		return
	}
	r := bounds
	switch ops.ImageMode(n.imageMode) {
	case ops.ImageModeFit:
		s := min(bounds.W/iw, bounds.H/ih)
		r.W, r.H = iw*s, ih*s
	case ops.ImageModeFill:
		s := max(bounds.W/iw, bounds.H/ih)
		r.W, r.H = iw*s, ih*s
	case ops.ImageModeCenter:
		r.W, r.H = iw/h.env.Scale, ih/h.env.Scale
	}
	r.X, r.Y = (bounds.W-r.W)/2, (bounds.H-r.H)/2
	rr := Rounded(bounds, n.cornerRadius)
	Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
	if c, ok := h.resolve(n.color); ok && n.color != 0 {
		SnapshotPushMask(snap, 0) // GSK_MASK_MODE_ALPHA
		SnapshotAppendScaledTexture(snap, t, 2, &r)
		Call(FnSnapshotPop, snap)
		Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&r))
		Call(FnSnapshotPop, snap)
	} else {
		SnapshotAppendScaledTexture(snap, t, 2, &r) // GSK_SCALING_FILTER_TRILINEAR
	}
	Call(FnSnapshotPop, snap)
}
