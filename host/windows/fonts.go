//go:build windows && amd64

package windows

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/egoist/ryui/internal/windows"
)

// The app's fonts: the files in its assets/fonts, added for the process
// (AddFontResourceEx, private) the first time a FontFamily names one.
var appFonts map[string]string // a file's name (Inter-Bold) or family (Inter): GDI's family name

// customFamily is GDI's family name for a FontFamily of the app's, or
// "" if it has none of that name.
func (h *Host) customFamily(name string) string {
	if appFonts == nil {
		appFonts = map[string]string{}
		dir := filepath.Join(h.assets, "fonts")
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".ttf" && ext != ".otf" {
				continue
			}
			if !AddFontFile(filepath.Join(dir, e.Name())) {
				continue
			}
			base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			family, _, _ := strings.Cut(base, "-")
			// GDI knows a font by its family; the weight comes from the
			// view's FontWeight (a file named Inter-Bold is Inter).
			appFonts[base] = family
			appFonts[family] = family
		}
	}
	return appFonts[name]
}
