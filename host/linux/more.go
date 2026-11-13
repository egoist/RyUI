//go:build linux && !android

package linux

import (
	"path/filepath"
	"strings"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
)

// The kinds and props beyond the controls: each area's file registers
// its hooks here.

func (h *Host) createMore(n *node) bool {
	if h.createNav(n) {
		return true
	}
	for _, f := range createHooks {
		if f(h, n) {
			return true
		}
	}
	return false
}

func (h *Host) setOwnMore(n *node, p ops.PropID, v *ops.Value) {
	if h.setNavProp(n, p, v) {
		return
	}
	for _, f := range propHooks {
		if f(h, n, p, v) {
			return
		}
	}
}

func (h *Host) setCommonMore(n *node, p ops.PropID, v *ops.Value) {
	for _, f := range commonHooks {
		if f(h, n, p, v) {
			return
		}
	}
}

func (h *Host) applyTextMore(n *node) {
	for _, f := range textHooks {
		f(h, n)
	}
}

func (h *Host) listenMore(n *node, e ops.EventKind) {
	for _, f := range listenHooks {
		if f(h, n, e) {
			return
		}
	}
}

func (h *Host) commandMore(n *node, c ops.CommandID, args []byte) {
	for _, f := range commandHooks {
		if f(h, n, c, args) {
			return
		}
	}
}

func (h *Host) measureMore(n *node, c layout.Constraints) (layout.Size, bool) {
	for _, f := range measureHooks {
		if s, ok := f(h, n, c); ok {
			return s, true
		}
	}
	return layout.Size{}, false
}

func (h *Host) setFieldMore(n *node, p ops.PropID, v *ops.Value) {
	for _, f := range propHooks {
		if f(h, n, p, v) {
			return
		}
	}
}

func (h *Host) setScrollMore(n *node, p ops.PropID, v *ops.Value) {
	for _, f := range propHooks {
		if f(h, n, p, v) {
			return
		}
	}
}

func (h *Host) moreModules(m map[string]map[string]method) {
	for _, f := range moduleHooks {
		f(h, m)
	}
}

// Hooks the areas' files add to, in their init.
var (
	createHooks   []func(h *Host, n *node) bool
	propHooks     []func(h *Host, n *node, p ops.PropID, v *ops.Value) bool
	commonHooks   []func(h *Host, n *node, p ops.PropID, v *ops.Value) bool
	textHooks     []func(h *Host, n *node)
	listenHooks   []func(h *Host, n *node, e ops.EventKind) bool
	commandHooks  []func(h *Host, n *node, c ops.CommandID, args []byte) bool
	measureHooks  []func(h *Host, n *node, c layout.Constraints) (layout.Size, bool)
	drawHooks     []func(h *Host, n *node, snap Ptr, bounds Rect) bool
	moduleHooks   []func(h *Host, m map[string]map[string]method)
	scrollHooks   []func(h *Host, n *node)
	scrollToHooks []func(h *Host, n *node, x, y float64, animated bool) bool
	// frameHooks take a node's frame instead of the host's placing it (a
	// list's cells, a split view's detail): true, taken.
	frameHooks []func(h *Host, n *node, f [4]float32) bool
	// releaseHooks let go of an area's own state as a node goes.
	releaseHooks []func(h *Host, n *node)
)

func (h *Host) frameMore(n *node, f [4]float32) bool {
	for _, fn := range frameHooks {
		if fn(h, n, f) {
			return true
		}
	}
	return false
}

func (h *Host) releaseMore(n *node) {
	for _, f := range releaseHooks {
		f(h, n)
	}
}

// snapshotContent draws what a node's view shows of its own, under its
// children: an image, a symbol, page dots, a shape, a canvas.
func (h *Host) snapshotContent(n *node, w, snap Ptr, bounds Rect) {
	switch n.kind {
	case ops.KindImage:
		h.drawImage(n, snap, bounds)
		return
	case ops.KindSymbol:
		h.drawSymbol(n, snap, bounds)
		return
	}
	for _, f := range drawHooks {
		if f(h, n, snap, bounds) {
			return
		}
	}
}

func (h *Host) scrollMoved(n *node) {
	for _, f := range scrollHooks {
		f(h, n)
	}
}

func (h *Host) listScrollToOffset(n *node, x, y float64, animated bool) {
	for _, f := range scrollToHooks {
		if f(h, n, x, y, animated) {
			return
		}
	}
}

// finishRelease lets go of a node that went out with a transition.
func (h *Host) finishRelease(n *node) { h.release(n) }

// customFamily is the family of a font of the app's (assets/fonts),
// loaded for the process: Inter-Bold names Inter's file, whose family is
// Inter.
func (h *Host) customFamily(name string) string {
	h.loadAppFonts()
	if fam, ok := h.fontFamilies[strings.ToLower(name)]; ok {
		return fam
	}
	return name // a system family
}

// loadAppFonts adds the app's font files (assets/fonts) to fontconfig's
// fonts for the process, once.
func (h *Host) loadAppFonts() {
	if h.fontFamilies != nil {
		return
	}
	h.fontFamilies = map[string]string{}
	files, _ := filepath.Glob(filepath.Join(h.assets, "fonts", "*"))
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
			continue
		}
		fam, ok := addFontFile(f)
		if !ok {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		h.fontFamilies[strings.ToLower(base)] = fam
		h.fontFamilies[strings.ToLower(fam)] = fam
		if i := strings.IndexByte(base, '-'); i > 0 {
			h.fontFamilies[strings.ToLower(base[:i])] = fam
		}
	}
	if len(h.fontFamilies) > 0 {
		fontsChanged()
	}
}

// toast shows a short message over the window in use (AdwToast).
func (h *Host) toast(text string) {
	w := h.activeWin
	if w == nil {
		w = h.main
	}
	if w == nil || w.toasts == 0 {
		return
	}
	t := AdwToastNew(text)
	AdwToastOverlayAddToast(w.toasts, t)
}

// presentPopover shows a Popover node (popover.go adds it).
var presentPopoverHook func(h *Host, n *node)

func (h *Host) presentPopover(n *node) {
	if presentPopoverHook != nil {
		presentPopoverHook(h, n)
	}
}
