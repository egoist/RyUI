//go:build darwin && !ios

package darwin

import (
	clist "container/list"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ebitengine/purego"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// symbolImage is an SF Symbol's image (0: none, or no such symbol).
func symbolImage(name string) ID {
	if name == "" {
		return 0
	}
	return Send(Class("NSImage"), "imageWithSystemSymbolName:accessibilityDescription:", uintptr(String(name)), 0)
}

var msgSymbolConfig func(cls ID, sel SEL, size, weight float64, scale int) ID

// applySymbol shows a Symbol at its node's font: the text style's size
// and the weight.
func (h *Host) applySymbol(n *node) {
	img := symbolImage(n.symbol)
	if img == 0 {
		Send(n.view, "setImage:", 0)
		return
	}
	if msgSymbolConfig == nil {
		purego.RegisterFunc(&msgSymbolConfig, MsgSendAddr())
	}
	f := h.nodeFont(n)
	w := 0.0
	if n.fontWeight > 0 && int(n.fontWeight) < len(weights) {
		w = weights[n.fontWeight]
	}
	conf := msgSymbolConfig(Class("NSImageSymbolConfiguration"), Sel("configurationWithPointSize:weight:scale:"),
		MsgFloat(f, Sel("pointSize")), w, 2) // medium
	img = Send(img, "imageWithSymbolConfiguration:", uintptr(conf))
	Send(n.view, "setImage:", uintptr(img))
	if n.color == 0 {
		Send(n.view, "setContentTintColor:", uintptr(Send(Class("NSColor"), "labelColor")))
	}
}

// Images: the view is a layer-backed AsukaView with a sublayer showing the
// image at its mode's gravity; a tint is the color, masked by the image.

func (h *Host) setImageProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropImageSource:
		n.source = v.Str
		n.loaded = false
		x := n.imx()
		x.stopFrames()
		x.anim, x.frame = nil, 0
		Send(n.view, "setImage:", 0)
		if SendBool(n.view, "wantsLayer") {
			Send(Send(n.view, "layer"), "setContents:", 0)
		}
		h.showPlaceholder(n)
		h.loadImage(n, v.Str, false)
	case ops.PropImagePlaceholder:
		n.placeholder = v.Str
		h.showPlaceholder(n)
	case ops.PropImagePlaceholderImage:
		x := n.imx()
		if x.hash != 0 {
			Release(x.hash)
		}
		x.hash = hashImage(v.Bytes)
		h.showPlaceholder(n)
	case ops.PropImagePlaceholderMode:
		n.imx().mode = int32(v.Int)
		if x := n.imgx; x.shown != 0 {
			h.setPlaceholderImage(n, x.shown, x.tinted)
		}
	case ops.PropImageTransition:
		n.imx().transition = v.Float
	case ops.PropImagePaused:
		n.imx().paused = v.Int != 0
		if n.imgx.paused {
			n.imgx.stopFrames()
		} else {
			h.playFrames(n)
		}
	case ops.PropImageMode:
		n.imageMode = int32(v.Int)
		h.showImage(n, Send(n.view, "image"))
		if x := n.imgx; x != nil && x.shown != 0 && x.mode < 0 {
			h.setPlaceholderImage(n, x.shown, x.tinted)
		}
	}
}

func (h *Host) loadImage(n *node, src string, placeholder bool) {
	if src == "" {
		return
	}
	id := n.id
	if img, ok := images.get(src); ok {
		h.imageLoaded(id, src, img, nil, placeholder)
		return
	}
	go func() {
		img, anim, err := decodeImage(src, h.assets)
		h.Post(func() {
			c := cachedImage{img, anim}
			if err == nil {
				// Two loads of one source may race: the cache's is the one.
				c = images.put(src, img, anim)
				Release(img) // the cache holds it
			}
			h.imageLoaded(id, src, c, err, placeholder)
		})
	}()
}

func (h *Host) imageLoaded(id ops.NodeID, src string, c cachedImage, err error, placeholder bool) {
	img := c.img
	WithPool(func() {
		n := h.nodes[id]
		if n == nil || n.view == 0 {
			return
		}
		if placeholder {
			if !n.loaded && n.placeholder == src && err == nil && (n.imgx == nil || n.imgx.hash == 0) {
				h.setPlaceholderImage(n, img, n.color != 0)
			}
			return
		}
		if n.source != src {
			return
		}
		if err != nil {
			h.emit(n, ops.Event{Kind: ops.EventFailed, Str: err.Error()})
			return
		}
		n.loaded = true
		x := n.imx()
		x.anim, x.frame = c.anim, 0
		// ImageIO plays an animated file (APNG and WebP too), not the view.
		Send(n.view, "setAnimates:", Bool(c.anim == nil))
		h.showImage(n, img)
		h.arrive(n)
		s := MsgSize(img, Sel("size"))
		h.emit(n, ops.Event{Kind: ops.EventLoaded, X: s.Width, Y: s.Height})
		if c.anim != nil {
			h.emit(n, ops.Event{Kind: ops.EventAnimated})
			h.playFrames(n)
		}
	})
}

func (h *Host) showImage(n *node, img ID) {
	switch ops.ImageMode(n.imageMode) {
	case ops.ImageModeFill:
		// NSImageView can't fill: a layer of the image, cropped.
		Send(n.view, "setImageScaling:", 3)
		Send(n.view, "setImage:", 0)
		l := h.layer(n)
		Send(l, "setContents:", uintptr(img))
		Send(l, "setContentsGravity:", uintptr(String("resizeAspectFill")))
		Send(l, "setMasksToBounds:", 1)
		return
	case ops.ImageModeStretch:
		Send(n.view, "setImageScaling:", 1) // axes independently
	case ops.ImageModeCenter:
		Send(n.view, "setImageScaling:", 2) // none
	default:
		Send(n.view, "setImageScaling:", 3)
	}
	if SendBool(n.view, "wantsLayer") {
		Send(Send(n.view, "layer"), "setContents:", 0)
	}
	if img != 0 && n.color != 0 {
		Send(img, "setTemplate:", 1)
	}
	Send(n.view, "setImage:", uintptr(img))
}

// decodeImage reads and decodes an image off the main thread: an NSImage
// (+1) sized one point per pixel (PNG, JPEG, GIF, WebP, HEIC, AVIF, SVG:
// what NSImage reads), and an animated one's file.
func decodeImage(src, assets string) (ID, []byte, error) {
	data, err := readImage(src, assets)
	if err != nil {
		return 0, nil, err
	}
	var img ID
	WithPool(func() {
		d := Data(data)
		rep := Send(Class("NSBitmapImageRep"), "imageRepWithData:", uintptr(d))
		img = Send(Send(Class("NSImage"), "alloc"), "initWithData:", uintptr(d))
		if img == 0 {
			return
		}
		if rep != 0 {
			w, hh := SendInt(rep, "pixelsWide"), SendInt(rep, "pixelsHigh")
			if w > 0 && hh > 0 {
				MsgSetSize(img, Sel("setSize:"), Size{Width: float64(w), Height: float64(hh)})
			}
		}
	})
	if img == 0 {
		return 0, nil, fmt.Errorf("can't decode %s", src)
	}
	var anim []byte
	if frameCount(data) > 1 {
		anim = data
	}
	return img, anim, nil
}

func readImage(src, assets string) ([]byte, error) {
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		resp, err := http.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("%s: %s", src, resp.Status)
		}
		return io.ReadAll(resp.Body)
	case strings.HasPrefix(src, "file://"):
		return os.ReadFile(strings.TrimPrefix(src, "file://"))
	case filepath.IsAbs(src):
		return os.ReadFile(src)
	}
	return os.ReadFile(filepath.Join(assets, filepath.FromSlash(strings.TrimPrefix(src, "/"))))
}

// imageCache keeps decoded images in memory, the least recently used
// going first.
type imageCache struct {
	mu    sync.Mutex
	order *clist.List
	byKey map[string]*clist.Element
}

type cached struct {
	key string
	cachedImage
}

// cachedImage is a decoded image (an NSImage), and an animated one's file.
type cachedImage struct {
	img  ID
	anim []byte
}

var images = &imageCache{order: clist.New(), byKey: map[string]*clist.Element{}}

const imageCacheSize = 128

func (c *imageCache) get(key string) (cachedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.byKey[key]
	if e == nil {
		return cachedImage{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cached).cachedImage, true
}

// put keeps img for key, and returns what the cache has for key: img,
// or the image an earlier load put there.
func (c *imageCache) put(key string, img ID, anim []byte) cachedImage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.byKey[key]; e != nil {
		c.order.MoveToFront(e)
		return e.Value.(*cached).cachedImage
	}
	Retain(img)
	c.byKey[key] = c.order.PushFront(&cached{key, cachedImage{img, anim}})
	for c.order.Len() > imageCacheSize {
		e := c.order.Back()
		x := e.Value.(*cached)
		c.order.Remove(e)
		delete(c.byKey, x.key)
		Release(x.img)
	}
	return cachedImage{img, anim}
}
