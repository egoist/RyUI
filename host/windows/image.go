//go:build windows && amd64

package windows

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

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// Images are fetched and decoded off the main thread (Go's decoders:
// PNG, JPEG, GIF), kept in memory by source, and drawn with GDI+ at the
// view's mode, clipped to its corners.
type image struct {
	source      string
	placeholder string
	mode        ops.ImageMode
	bmp         *Bitmap // what shows: the source, or the placeholder until it loads
	loaded      bool
	tinted      *Bitmap
	tint        ops.Color
}

func (im *image) release() {
	if im.tinted != nil {
		im.tinted.Close()
		im.tinted = nil
	}
}

// decoded is an image's premultiplied BGRA pixels.
type decoded struct {
	w, h   int32
	pixels []byte
	bmp    *Bitmap
	err    error
}

var (
	imageMu    sync.Mutex
	imageCache = map[string]*decoded{}
	imageWait  = map[string][]func(*decoded){}
)

func (h *Host) setImageProp(n *node, p ops.PropID, v *ops.Value) {
	im := n.image
	switch p {
	case ops.PropImageSource:
		im.source, im.loaded = v.Str, false
		h.loadImage(n, v.Str, false)
	case ops.PropImagePlaceholder:
		im.placeholder = v.Str
		if !im.loaded {
			h.loadImage(n, v.Str, true)
		}
	case ops.PropImageMode:
		im.mode = ops.ImageMode(v.Int)
		h.redraw(n)
	}
}

// loadImage shows src in n once it's decoded, and reports Loaded (its
// natural size) or Failed.
func (h *Host) loadImage(n *node, src string, placeholder bool) {
	if src == "" {
		return
	}
	id := n.id
	h.fetchImage(src, func(d *decoded) {
		n := h.nodes[id]
		if n == nil || n.image == nil {
			return
		}
		im := n.image
		if placeholder && (im.loaded || im.placeholder != src) || !placeholder && im.source != src {
			return
		}
		if d.err != nil {
			if !placeholder {
				h.emit(n, ops.Event{Kind: ops.EventFailed, Str: d.err.Error()})
			}
			return
		}
		if d.bmp == nil {
			d.bmp = NewBitmap(d.w, d.h, d.pixels)
		}
		im.bmp = d.bmp
		im.release()
		if !placeholder {
			im.loaded = true
			h.emit(n, ops.Event{Kind: ops.EventLoaded, X: float64(d.w), Y: float64(d.h)})
		}
		h.redraw(n)
	})
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
		imageMu.Lock()
		imageCache[src] = d
		fs := imageWait[src]
		delete(imageWait, src)
		imageMu.Unlock()
		h.Post(func() {
			for _, f := range fs {
				f(d)
			}
		})
	}()
}

func decodeImage(src, assets string) *decoded {
	var data []byte
	var err error
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		var resp *http.Response
		resp, err = http.Get(src)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				err = fmt.Errorf("%s: %s", src, resp.Status)
			} else {
				data, err = io.ReadAll(resp.Body)
			}
		}
	default:
		p := src
		if !filepath.IsAbs(p) {
			p = filepath.Join(assets, filepath.FromSlash(src))
		}
		data, err = os.ReadFile(p)
	}
	if err != nil {
		return &decoded{err: err}
	}
	img, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return &decoded{err: err}
	}
	b := img.Bounds()
	rgba := stdimage.NewRGBA(stdimage.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src) // premultiplied
	px := rgba.Pix
	for i := 0; i < len(px); i += 4 {
		px[i], px[i+2] = px[i+2], px[i] // RGBA to BGRA
	}
	return &decoded{w: int32(b.Dx()), h: int32(b.Dy()), pixels: px}
}

// paintImage draws the image at its mode: fit, fill (cropped), stretched
// or centered at its size, clipped to the view's corners; Color tints it.
func (h *Host) paintImage(n *node, g Graphics, w, ht, radius float32) {
	im := n.image
	b := im.bmp
	if b == nil || b.W == 0 || b.H == 0 {
		return
	}
	if n.color != 0 {
		b = h.tinted(n)
	}
	iw, ih := float32(b.W), float32(b.H)
	sx, sy, sw, sh := float32(0), float32(0), iw, ih
	x, y, dw, dh := float32(0), float32(0), w, ht
	switch im.mode {
	case ops.ImageModeFit:
		s := min(w/iw, ht/ih)
		dw, dh = iw*s, ih*s
		x, y = (w-dw)/2, (ht-dh)/2
	case ops.ImageModeFill:
		s := max(w/iw, ht/ih)
		sw, sh = w/s, ht/s
		sx, sy = (iw-sw)/2, (ih-sh)/2
	case ops.ImageModeCenter:
		k := h.scale
		dw, dh = iw*k, ih*k
		x, y = (w-dw)/2, (ht-dh)/2
	}
	if radius > 0 {
		p := NewPath()
		p.RoundRect(0, 0, w, ht, radius)
		g.ClipPath(p)
		defer func() { g.ResetClip(); p.Close() }()
	}
	g.DrawImage(b, x, y, dw, dh, sx, sy, sw, sh)
}

// tinted is the image in n's color, its alpha kept (a template image).
func (h *Host) tinted(n *node) *Bitmap {
	im := n.image
	if im.tinted != nil && im.tint == n.color {
		return im.tinted
	}
	im.release()
	c, ok := h.resolve(n.color)
	if !ok {
		return im.bmp
	}
	src := im.bmp
	var px []byte
	imageMu.Lock()
	for _, d := range imageCache {
		if d.bmp == src {
			px = d.pixels
		}
	}
	imageMu.Unlock()
	if px == nil {
		return src
	}
	out := make([]byte, len(px))
	for i := 0; i < len(px); i += 4 {
		a := uint32(px[i+3]) * uint32(c.a) / 255
		out[i] = uint8(uint32(c.b) * a / 255)
		out[i+1] = uint8(uint32(c.g) * a / 255)
		out[i+2] = uint8(uint32(c.r) * a / 255)
		out[i+3] = uint8(a)
	}
	im.tinted, im.tint = NewBitmap(src.W, src.H, out), n.color
	return im.tinted
}

// Symbols: SF Symbol names drawn as the Segoe Fluent Icons glyphs for
// them (Segoe MDL2 Assets before Windows 11), a curated set (".fill"
// variants take the filled glyph where there's one).

var symbols = map[string]uint16{
	"house": 0xE80F, "house.fill": 0xEA8A, "magnifyingglass": 0xE721, "gearshape": 0xE713, "gearshape.fill": 0xE713,
	"gear": 0xE713, "person": 0xE77B, "person.fill": 0xEA8C, "person.2": 0xE716, "person.2.fill": 0xE716,
	"person.crop.circle": 0xE77B, "star": 0xE734, "star.fill": 0xE735, "heart": 0xEB51, "heart.fill": 0xEB52,
	"list.bullet": 0xE8FD, "line.3.horizontal": 0xE700, "line.3.horizontal.decrease": 0xE71C, "ellipsis": 0xE712,
	"ellipsis.circle": 0xE712, "plus": 0xE710, "minus": 0xE738, "xmark": 0xE711, "xmark.circle.fill": 0xEB90,
	"checkmark": 0xE73E, "checkmark.circle": 0xE73E, "checkmark.circle.fill": 0xEC61, "circle": 0xEA3A,
	"chevron.left": 0xE76B, "chevron.right": 0xE76C, "chevron.up": 0xE70E, "chevron.down": 0xE70D,
	"arrow.left": 0xE72B, "arrow.right": 0xE72A, "arrow.up": 0xE74A, "arrow.down": 0xE74B,
	"arrow.clockwise": 0xE72C, "arrow.counterclockwise": 0xE7A7, "trash": 0xE74D, "trash.fill": 0xE74D,
	"pencil": 0xE70F, "square.and.pencil": 0xE70F, "square.and.arrow.up": 0xE72D, "square.and.arrow.down": 0xE896,
	"paperplane": 0xE724, "paperplane.fill": 0xE724, "bell": 0xEA8F, "bell.fill": 0xEA8F, "envelope": 0xE715,
	"envelope.fill": 0xE715, "phone": 0xE717, "phone.fill": 0xE717, "message": 0xE8BD, "message.fill": 0xE8BD,
	"bubble.left": 0xE8BD, "camera": 0xE722, "camera.fill": 0xE722, "photo": 0xE91B, "photo.fill": 0xE91B,
	"qrcode": 0xED14, "video": 0xE714, "video.fill": 0xE714, "play": 0xE768, "play.fill": 0xE768,
	"pause": 0xE769, "pause.fill": 0xE769, "stop.fill": 0xE71A, "forward.fill": 0xEB9D, "backward.fill": 0xEB9E,
	"speaker.wave.2": 0xE767, "speaker.wave.2.fill": 0xE767, "speaker.slash": 0xE74F, "mic": 0xE720, "mic.fill": 0xE720,
	"lock": 0xE72E, "lock.fill": 0xE72E, "lock.open": 0xE785, "location": 0xE81D, "location.fill": 0xE81D,
	"mappin": 0xE707, "map": 0xE707, "globe": 0xE774, "link": 0xE71B, "doc": 0xE8A5, "doc.fill": 0xE8A5,
	"doc.text": 0xE8A5, "doc.on.doc": 0xE8C8, "folder": 0xE8B7, "folder.fill": 0xE8D5, "archivebox": 0xE7B8,
	"tray": 0xE7B8, "calendar": 0xE787, "clock": 0xE823, "clock.fill": 0xE823, "timer": 0xE916,
	"cart": 0xE7BF, "cart.fill": 0xE7BF, "bag": 0xE7BF, "creditcard": 0xE8C7, "gift": 0xE88C,
	"bookmark": 0xE8A4, "bookmark.fill": 0xE8A4, "book": 0xE82D, "tag": 0xE8EC, "flag": 0xE7C1, "flag.fill": 0xE7C1,
	"info.circle": 0xE946, "info.circle.fill": 0xE946, "questionmark.circle": 0xE897, "exclamationmark.triangle": 0xE7BA,
	"exclamationmark.triangle.fill": 0xE7BA, "exclamationmark.circle": 0xE783, "eye": 0xE7B3, "eye.slash": 0xED1A,
	"wifi": 0xE701, "bolt": 0xE945, "bolt.fill": 0xE945, "sun.max": 0xE706, "moon": 0xE708, "moon.fill": 0xE708,
	"cloud": 0xE753, "icloud": 0xE753, "square.grid.2x2": 0xF0E2, "rectangle.grid.2x2": 0xF0E2,
	"slider.horizontal.3": 0xE9E9, "paintbrush": 0xE771, "paintpalette": 0xE790, "textformat": 0xE8D2,
	"curlybraces": 0xE943, "chart.bar": 0xE9D2, "chart.bar.fill": 0xE9D2, "hand.thumbsup": 0xE8E1,
	"hand.thumbsdown": 0xE8E0, "square.and.arrow.up.fill": 0xE72D, "arrow.up.right.square": 0xE8A7,
	"safari": 0xE774, "keyboard": 0xE765, "printer": 0xE749, "music.note": 0xE8D6, "headphones": 0xE7F6,
	"battery.100": 0xE83F, "lightbulb": 0xEA80, "shield": 0xEA18, "key": 0xE8D7, "house.circle": 0xE80F,
	"square.stack": 0xE8F1, "photo.on.rectangle": 0xE91B, "film": 0xE8B2, "scissors": 0xE8C6,
	"doc.on.clipboard": 0xE77F, "arrow.triangle.2.circlepath": 0xE895, "arrow.uturn.backward": 0xE7A7,
	"arrow.uturn.forward": 0xE7A6, "plus.circle": 0xE948, "plus.circle.fill": 0xE948, "minus.circle": 0xE949,
	"person.circle": 0xE77B, "person.crop.circle.fill": 0xEA8C, "ellipsis.bubble": 0xE8BD,
}

func symbolGlyph(name string) uint16 {
	if name == "" {
		return 0
	}
	if g, ok := symbols[name]; ok {
		return g
	}
	if g, ok := symbols[strings.TrimSuffix(name, ".fill")]; ok {
		return g
	}
	return 0
}

var warned = map[string]bool{}

// warnSymbol tells, once, of a symbol the host has no glyph for: it
// shows nothing.
func warnSymbol(name string) {
	if !warned[name] {
		warned[name] = true
		fmt.Fprintf(os.Stderr, "nui: the Windows host has no glyph for the symbol %q\n", name)
	}
}
