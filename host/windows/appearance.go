//go:build windows

package windows

import . "github.com/egoist/asuka/internal/windows"

// asuka/sdk/appearance on Windows: the accent color (Settings,
// Personalization, Colors: the accent palette's own entry, else DWM's
// colorization color), a contrast theme on (SPI_GETHIGHCONTRAST) as
// Increase Contrast, and transparency effects off (the Personalize key's
// EnableTransparency) as Reduce Transparency. They're read again as the
// system window hears WM_SETTINGCHANGE (ImmersiveColorSet, the contrast
// theme), WM_SYSCOLORCHANGE or WM_DWMCOLORIZATIONCOLORCHANGED.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["appearance"] = appearance.methods()
	})
	appearance.start = watchAppearance
}

var appearance = watched{read: readAppearance}

func watchAppearance() {
	onSystemMessage(func(m uint32, _, _ uintptr) {
		switch m {
		case WM_SETTINGCHANGE, WM_SYSCOLORCHANGE, WM_DWMCOLORIZATIONCOLORCHANGED, WM_THEMECHANGED:
			appearance.changed()
		}
	})
}

// readAppearance is asuka/sdk/appearance's settings: u8 has accent, red,
// green, blue, increase contrast, reduce transparency.
func readAppearance() []byte {
	b := make([]byte, 6)
	if c, ok := AccentColor(); ok { // 0x00BBGGRR
		b[0], b[1], b[2], b[3] = 1, byte(c), byte(c>>8), byte(c>>16)
	}
	b[4] = b2u(HighContrast())
	b[5] = b2u(TransparencyOff())
	return b
}
