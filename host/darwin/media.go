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

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
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

// Images: the view is a layer-backed RyUIView with a sublayer showing the
// image at its mode's gravity; a tint is the color, masked by the image.

func (h *Host) setImageProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropImageSource:
		n.source = v.Str
		n.loaded = false
		h.loadImage(n, v.Str, false)
	case ops.PropImagePlaceholder:
		n.placeholder = v.Str
		if !n.loaded {
			h.loadImage(n, v.Str, true)
		}
	case ops.PropImageMode:
		n.imageMode = int32(v.Int)
		h.showImage(n, Send(n.view, "image"))
	}
}

func (h *Host) loadImage(n *node, src string, placeholder bool) {
	if src == "" {
		if !placeholder {
			Send(n.view, "setImage:", 0)
		}
		return
	}
	id := n.id
	if img, ok := images.get(src); ok {
		h.imageLoaded(id, src, img, nil, placeholder)
		return
	}
	go func() {
		img, err := decodeImage(src, h.assets)
		h.Post(func() {
			if err == nil {
				images.put(src, img)
				Release(img) // the cache holds it
			}
			h.imageLoaded(id, src, img, err, placeholder)
		})
	}()
}

func (h *Host) imageLoaded(id ops.NodeID, src string, img ID, err error, placeholder bool) {
	WithPool(func() {
		n := h.nodes[id]
		if n == nil || n.view == 0 {
			return
		}
		if placeholder {
			if !n.loaded && n.placeholder == src && err == nil {
				h.showImage(n, img)
			}
			return
		}
		if n.source != src {
			return
		}
		if err != nil {
			if n.placeholder != "" {
				h.loadImage(n, n.placeholder, true)
			}
			h.emit(n, ops.Event{Kind: ops.EventFailed, Str: err.Error()})
			return
		}
		n.loaded = true
		h.showImage(n, img)
		s := MsgSize(img, Sel("size"))
		h.emit(n, ops.Event{Kind: ops.EventLoaded, X: s.Width, Y: s.Height})
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
// (+1) sized one point per pixel.
func decodeImage(src, assets string) (ID, error) {
	data, err := readImage(src, assets)
	if err != nil {
		return 0, err
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
		return 0, fmt.Errorf("can't decode %s", src)
	}
	return img, nil
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
	img ID
}

var images = &imageCache{order: clist.New(), byKey: map[string]*clist.Element{}}

const imageCacheSize = 128

func (c *imageCache) get(key string) (ID, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.byKey[key]
	if e == nil {
		return 0, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cached).img, true
}

func (c *imageCache) put(key string, img ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.byKey[key]; e != nil {
		c.order.MoveToFront(e)
		return
	}
	Retain(img)
	c.byKey[key] = c.order.PushFront(&cached{key, img})
	for c.order.Len() > imageCacheSize {
		e := c.order.Back()
		x := e.Value.(*cached)
		c.order.Remove(e)
		delete(c.byKey, x.key)
		Release(x.img)
	}
}
