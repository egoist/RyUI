//go:build linux && !android

package linux

import (
	"sync"

	"github.com/ebitengine/purego"

	. "github.com/egoist/ryui/internal/gtk"
)

// The app's fonts are added to fontconfig's application fonts, which
// Pango (on fontconfig) then finds by their family.

var (
	fcOnce                      sync.Once
	fcAppFontAddFile            func(config Ptr, file string) int32
	fcConfigGetFonts            func(config Ptr, set int32) Ptr
	fcPatternGetString          func(p Ptr, object string, n int32, s *Ptr) int32
	pangoFcFontMapConfigChanged func(fm Ptr)
)

func loadFontconfig() bool {
	fcOnce.Do(func() {
		lib, err := purego.Dlopen("libfontconfig.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&fcAppFontAddFile, lib, "FcConfigAppFontAddFile")
		purego.RegisterLibFunc(&fcConfigGetFonts, lib, "FcConfigGetFonts")
		purego.RegisterLibFunc(&fcPatternGetString, lib, "FcPatternGetString")
		if p := Sym("pango_fc_font_map_config_changed"); p != 0 {
			purego.RegisterFunc(&pangoFcFontMapConfigChanged, p)
		}
	})
	return fcAppFontAddFile != nil
}

// addFontFile adds a font file for the process and returns its family.
func addFontFile(path string) (string, bool) {
	if !loadFontconfig() || fcAppFontAddFile(0, path) == 0 {
		return "", false
	}
	set := fcConfigGetFonts(0, 1) // FcSetApplication
	if set == 0 {
		return "", false
	}
	// FcFontSet: int nfont, int sfont, FcPattern **fonts.
	nfont := *At[int32](set)
	fonts := *At[Ptr](set + 8)
	for i := int32(0); i < nfont; i++ {
		pat := *At[Ptr](fonts + uintptr(i)*8)
		var file, fam Ptr
		if fcPatternGetString(pat, "file", 0, &file) == 0 && GoString(file) == path &&
			fcPatternGetString(pat, "family", 0, &fam) == 0 {
			return GoString(fam), true
		}
	}
	return "", false
}

// fontsChanged has Pango see the fonts added.
func fontsChanged() {
	if pangoFcFontMapConfigChanged != nil {
		pangoFcFontMapConfigChanged(FontMapGetDefault())
	}
}
