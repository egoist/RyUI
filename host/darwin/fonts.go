//go:build darwin && !ios

package darwin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego"

	. "github.com/egoist/nui/internal/darwin"
)

// The app's fonts: the files in its assets/fonts, registered for the
// process the first time a font is asked for.
var (
	fontFiles     map[string]string // a file's name (no extension) → its PostScript name
	ctRegisterURL func(url ID, scope int, err uintptr) bool
	ctDescriptors func(url ID) ID
	msgFontFamily func(obj ID, sel SEL, family ID, traits uint, weight int, size float64) ID
	familyWeights = [...]int{5, 2, 3, 4, 5, 6, 8, 9, 10, 11} // AppKit's 0-15 scale, by FontWeight
)

func (h *Host) assetFonts() map[string]string {
	if fontFiles != nil {
		return fontFiles
	}
	fontFiles = map[string]string{}
	ct := Dlopen("/System/Library/Frameworks/CoreText.framework/CoreText")
	purego.RegisterLibFunc(&ctRegisterURL, ct, "CTFontManagerRegisterFontsForURL")
	purego.RegisterLibFunc(&ctDescriptors, ct, "CTFontManagerCreateFontDescriptorsFromURL")
	dir := filepath.Join(h.assets, "fonts")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".ttf" && ext != ".otf" {
			continue
		}
		abs, _ := filepath.Abs(filepath.Join(dir, e.Name()))
		u := FileURL(abs)
		ctRegisterURL(u, 1, 0) // the process's
		ds := ctDescriptors(u)
		if items := ArrayItems(ds); len(items) > 0 {
			ps := GoString(Send(items[0], "objectForKey:", uintptr(Const(AppKit(), "NSFontNameAttribute"))))
			if ps != "" {
				fontFiles[strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))] = ps
			}
		}
		CFRelease(uintptr(ds))
	}
	return fontFiles
}

// familyFont is a font of family at size: an asset file's (Inter-Bold),
// a family's (an asset family, Inter, or the system's, Georgia) in
// weight, or a generic family (serif, monospace, rounded: the system
// font's designs). 0: none such.
func (h *Host) familyFont(family string, size float64, weight int32) ID {
	if strings.Contains(family, "://") {
		return 0
	}
	if ps := h.assetFonts()[family]; ps != "" {
		return MsgIDFloatID(Class("NSFont"), Sel("fontWithName:size:"), String(ps), size)
	}
	designs := map[string]string{
		"serif": "NSFontDescriptorSystemDesignSerif", "monospace": "NSFontDescriptorSystemDesignMonospaced",
		"rounded": "NSFontDescriptorSystemDesignRounded", "sans-serif": "NSFontDescriptorSystemDesignDefault",
	}
	if d, ok := designs[family]; ok {
		w := 0.0
		if weight > 0 && int(weight) < len(weights) {
			w = weights[weight]
		}
		sys := MsgFloat2ID(Class("NSFont"), Sel("systemFontOfSize:weight:"), size, w)
		desc := Send(Send(sys, "fontDescriptor"), "fontDescriptorWithDesign:", uintptr(Const(AppKit(), d)))
		if desc == 0 {
			return sys
		}
		return MsgIDFloatID(Class("NSFont"), Sel("fontWithDescriptor:size:"), desc, size)
	}
	if msgFontFamily == nil {
		purego.RegisterFunc(&msgFontFamily, MsgSendAddr())
	}
	fw := 5
	if weight > 0 && int(weight) < len(familyWeights) {
		fw = familyWeights[weight]
	}
	fm := Send(Class("NSFontManager"), "sharedFontManager")
	return msgFontFamily(fm, Sel("fontWithFamily:traits:weight:size:"), String(family), 0, fw, size)
}
