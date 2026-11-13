//go:build linux && !android

package linux

import (
	"math"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

func (h *Host) command(n *node, c ops.CommandID, args []byte) {
	r := reader{b: args}
	switch c {
	case ops.CmdFocus:
		if n.kind == ops.KindTextField {
			h.focusField(n)
		} else if n.widget != 0 {
			WidgetGrabFocus(n.widget)
		}
	case ops.CmdBlur:
		if w := h.winOf(n); w != nil {
			WindowSetFocus(w.window, 0)
		}
	case ops.CmdInsertText:
		if n.kind == ops.KindTextField {
			h.insertText(n, string(args))
		}
	case ops.CmdPresent:
		h.present(n)
	case ops.CmdHaptic:
		// Desktops have no haptics.
	case ops.CmdAnimateBegin:
		h.beginAnimation(args)
	case ops.CmdAnimateEnd:
		h.endAnimation()
	case ops.CmdOpenURL:
		h.openURL(string(args))
	case ops.CmdCall:
		h.call(n, args)
	case ops.CmdCopy:
		ClipboardSetText(DisplayGetClipboard(DisplayGetDefault()), string(args))
	case ops.CmdFrames:
		h.setFrames(len(args) > 0 && args[0] != 0)
	case ops.CmdAnnounce:
		h.announce(string(args))
	case ops.CmdReplace:
		if n.nav != nil {
			n.nav.pending = -2
		}
	case ops.CmdA11yFocus:
		if n.widget != 0 {
			WidgetGrabFocus(n.widget)
		}
	case ops.CmdShare:
		h.share(n, string(args))
	case ops.CmdScrollToOffset:
		x, y := float64(r.f32()), float64(r.f32())
		animated := r.u8() != 0
		h.scrollToOffset(n, x, y, animated)
	case ops.CmdAccent:
		h.accent = ops.Color(r.u64())
		h.updateCSS()
		h.themeChanged()
	case ops.CmdMenuBar:
		h.setMenuBar(args)
	case ops.CmdWindowOptions:
		h.setWindowOptions(args)
	case ops.CmdWindowAction:
		if w := h.winFor(n); w != nil {
			h.windowAction(w, args)
		}
	case ops.CmdWindowTitle:
		if h.main != nil {
			h.main.name = string(args)
			if h.main.name == "" {
				h.main.name = h.appName
			}
			h.updateTitle(h.main)
		}
	case ops.CmdLocale:
		h.setLocale(args)
	case ops.CmdSaveLocale:
		h.saveLocale(string(args))
	case ops.CmdColorScheme:
		if len(args) > 0 {
			scheme := int32(ColorSchemeDefault)
			switch ops.ColorScheme(args[0]) {
			case ops.ColorSchemeLight:
				scheme = ColorSchemeForceLight
			case ops.ColorSchemeDark:
				scheme = ColorSchemeForceDark
			}
			AdwStyleManagerSetColorScheme(AdwStyleManagerGetDefault(), scheme)
		}
	default:
		h.commandMore(n, c, args)
	}
}

// scrollToOffset scrolls a Scroll (or List) to an offset.
func (h *Host) scrollToOffset(n *node, x, y float64, animated bool) {
	if n.scroll != nil {
		h.scrollTo(n, x, y, animated)
		return
	}
	h.listScrollToOffset(n, x, y, animated)
}

var _ = math.Abs
