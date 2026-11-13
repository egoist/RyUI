//go:build darwin && !ios

package darwin

import (
	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/sdk/appearance on a Mac: the accent is NSColor's controlAccentColor
// (System Settings, Appearance; multicolor is blue), in sRGB; Increase
// Contrast and Reduce Transparency are NSWorkspace's accessibility
// display options. The accent's changes come as the system's colors
// change (NSSystemColorsDidChangeNotification), the options' as
// NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["appearance"] = appearanceWatch.methods()
	})
}

var appearanceWatch = watched{
	read: readAppearance,
	start: func(w *watched) {
		observeNamed(0, "NSSystemColorsDidChangeNotification", w.changed)
		observeNamed(workspaceCenter(), "NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification", w.changed)
	},
}

// readAppearance is u8 has accent, u8 red, green, blue, u8 increase
// contrast, u8 reduce transparency.
func readAppearance() []byte {
	s := make([]byte, 6)
	if accent := Send(Class("NSColor"), "controlAccentColor"); accent != 0 {
		rgb := Send(accent, "colorUsingColorSpace:", uintptr(Send(Class("NSColorSpace"), "sRGBColorSpace")))
		if rgb != 0 {
			c := func(sel string) byte { return byte(min(max(MsgFloat(rgb, Sel(sel)), 0), 1)*255 + 0.5) }
			s[0], s[1], s[2], s[3] = 1, c("redComponent"), c("greenComponent"), c("blueComponent")
		}
	}
	ws := Send(Class("NSWorkspace"), "sharedWorkspace")
	s[4] = b2u(SendBool(ws, "accessibilityDisplayShouldIncreaseContrast"))
	s[5] = b2u(SendBool(ws, "accessibilityDisplayShouldReduceTransparency"))
	return s
}
