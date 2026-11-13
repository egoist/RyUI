//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"

	"github.com/egoist/asuka/i18n"
	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// The language the app shows, and which way it goes.
//
// Frames from the core are physical, and a host never mirrors what the core
// places, so nothing inside the app's windows has mirrored coordinates: a
// view's window and its DC stay left to right, as the host draws them with
// GDI+, which isn't documented to follow a mirrored DC (WinForms doesn't
// support painting under RightToLeftLayout), and what the host draws itself
// takes the direction from isRTL.
//
// The top-level windows alone are mirrored, for what Windows draws around
// the app: the title bar (its buttons on the left, its text on the right) and
// the menu bar and its menus. WS_EX_NOINHERITLAYOUT keeps that from their
// children, and the one child each has fills its client area, which is the
// same rectangle in either direction, so the coordinates below it don't
// change. (x is 0 in a mirrored parent at the child's right edge, and the
// child is as wide as its parent.) Nothing draws in the client area of a
// top-level window but its background. See mirrorWindow. Should Windows
// ignore the style (it's documented for shell languages that read right to
// left), the frame stays as it is and the content is still right.

// isRTL reports whether n goes right to left: its LayoutDirection, or the
// app's. Host-made views take theirs from the view they belong to
// (setDirection).
func (h *Host) isRTL(n *node) bool {
	if n != nil {
		switch n.dir {
		case ops.LayoutDirectionLeftToRight:
			return false
		case ops.LayoutDirectionRightToLeft:
			return true
		}
	}
	return h.rtl
}

// setLocale is the Locale command: the language the app shows, and its
// direction.
func (h *Host) setLocale(args []byte) {
	if len(args) < 1 {
		return
	}
	rtl, tag := args[0]&1 != 0, string(args[1:])
	if tag != h.locale {
		h.locale = tag
		SetThreadLanguage(tag)
		h.sayWords()
	}
	if rtl == h.rtl {
		return
	}
	h.rtl = rtl
	h.mirrorWindow(h.window, h.root)
	for _, s := range h.sheets {
		h.mirrorWindow(s.nav.window, s)
	}
	for _, w := range h.wins {
		h.mirrorWindow(w.nav.window, w)
	}
	for _, n := range h.nodes {
		if n.dir == ops.LayoutDirectionInherit {
			h.directionChanged(n)
		}
	}
	if len(h.appMenus) > 0 {
		h.buildMenuBar()
	}
}

// sayWords says the host's own words in the app's language again.
func (h *Host) sayWords() {
	for _, n := range h.nodes {
		switch n.kind {
		case kindHeader:
			SetWindowText(n.nav.back, l10n.Back(h.locale))
		case ops.KindStepper:
			h.nameStepper(n)
		}
	}
}

// nameStepper names a Stepper's buttons: they draw a glyph, their text is
// what a screen reader says.
func (h *Host) nameStepper(n *node) {
	if n.ctl != 0 {
		SetWindowText(n.ctl, l10n.Decrease(h.locale))
	}
	if n.ctl2 != 0 {
		SetWindowText(n.ctl2, l10n.Increase(h.locale))
	}
}

// topLevelStyle is the extended style of the app's top-level windows, in
// the app's direction.
func (h *Host) topLevelStyle() uint32 {
	if h.rtl {
		return mirroredStyle
	}
	return 0
}

const mirroredStyle = WS_EX_LAYOUTRTL | WS_EX_NOINHERITLAYOUT | WS_EX_RTLREADING

// mirrorWindow gives a top-level window the app's direction, whose content
// (its one child, content) keeps its rectangle and its own coordinates.
func (h *Host) mirrorWindow(win uintptr, content *node) {
	if win == 0 {
		return
	}
	old := WindowLong(win, GWL_EXSTYLE)
	next := old&^mirroredStyle | uintptr(h.topLevelStyle())
	if next == old {
		return
	}
	SetWindowLong(win, GWL_EXSTYLE, next)
	// The frame and the menu bar again, in the new direction.
	SetWindowPos(win, 0, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED)
	if content != nil && content.hwnd != 0 {
		r := ClientRect(win)
		SetWindowPos(content.hwnd, 0, 0, 0, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	}
	if c := h.ctls[win]; c != nil {
		h.layoutChrome(c) // a hidden title bar's buttons to the other side
	}
	RedrawWindow(win, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE|RDW_FRAME)
	if GetMenu(win) != 0 {
		DrawMenuBar(win)
	}
}

// setDirection is a view's LayoutDirection. It's for one view: the core
// sends it to each view in the subtree. What the host made for the view (a
// stack's header, a tab bar, a list's rows) goes the same way.
func (h *Host) setDirection(n *node, d ops.LayoutDirection) {
	before := h.isRTL(n)
	n.dir = d
	if n.nav != nil && n.nav.bar != nil {
		n.nav.bar.dir = d
	}
	if n.list != nil {
		n.list.setDirection(d)
	}
	if h.isRTL(n) != before {
		h.directionChanged(n)
	}
}

// directionChanged redoes what depends on a view's direction, now changed.
func (h *Host) directionChanged(n *node) {
	switch {
	case n.kind == ops.KindTextField:
		h.alignField(n)
	case n.kind == ops.KindWheel:
		h.remakeWheel(n)
	case n.kind == ops.KindSlider:
		h.applySlider(n)
	case n.kind == ops.KindStepper:
		h.layoutStepper(n)
	case n.kind == ops.KindScroll:
		h.restartScroll(n)
	case n.kind == ops.KindStack:
		h.layoutScreens(n)
	case n.list != nil:
		n.list.rebind = true
		n.list.queue(h)
	case n.rich != nil:
		n.rich.invalidate(h, n)
	}
	if n.bar != nil {
		n.bar.thumb = Rect{}
		h.updateBars(n)
	}
	h.redraw(n)
}

// flipIn is r mirrored in outer.
func flipIn(r, outer Rect) Rect {
	r.Left, r.Right = outer.Left+outer.Right-r.Right, outer.Left+outer.Right-r.Left
	return r
}

// readsRTL reports whether n's text reads right to left: as its
// LayoutDirection says, else as its first strong character does, as the other
// hosts' views do (its side comes from the view's direction, not from this).
func (h *Host) readsRTL(n *node) bool {
	if n.dir != ops.LayoutDirectionInherit {
		return n.dir == ops.LayoutDirectionRightToLeft
	}
	return firstStrongRTL(n.utext, h.rtl)
}

// firstStrongRTL reports whether text's first strong character reads right
// to left (Unicode's bidi rule P2), or what's given for text without one.
func firstStrongRTL(text []uint16, none bool) bool {
	for i := 0; i < len(text); i++ {
		r := rune(text[i])
		if utf16.IsSurrogate(r) && i+1 < len(text) {
			r = utf16.DecodeRune(r, rune(text[i+1]))
			i++
		}
		switch {
		case r >= 0x0590 && r <= 0x08FF, r >= 0xFB1D && r <= 0xFDFF, r >= 0xFE70 && r <= 0xFEFF, r >= 0x10800 && r <= 0x10FFF, r >= 0x1E800 && r <= 0x1EFFF:
			if unicode.IsLetter(r) {
				return true
			}
		case unicode.IsLetter(r):
			return false
		}
	}
	return none
}

// textDirection is what DrawText needs for text going right to left, on top
// of its side.
func textDirection(rtl bool) uint32 {
	if rtl {
		return DT_RTLREADING
	}
	return 0
}

// startSide is DrawText's flags for text at the start of a line in a
// direction.
func startSide(rtl bool) uint32 {
	if rtl {
		return DT_RIGHT | DT_RTLREADING
	}
	return DT_LEFT
}

// alignedRight reports whether an alignment puts text on the right side in
// a direction: End on the side the direction ends on, Center on neither, and
// Start (Justify too, where a line can't be justified) on the side it starts
// on.
func alignedRight(a ops.TextAlign, rtl bool) bool {
	switch a {
	case ops.TextAlignEnd:
		return !rtl
	case ops.TextAlignCenter:
		return false
	}
	return rtl
}

// Symbols: a name that says a direction by forward or backward mirrors
// right to left; chevron.right doesn't, as it means right.

// directedGlyph is the glyph of a symbol in a direction. A name that says
// forward or backward takes its glyph if there's one (arrow.uturn.forward,
// or the other way round right to left), else that of the side it means
// (chevron.forward: chevron.right, left to right).
func directedGlyph(name string, rtl bool) uint16 {
	if !strings.Contains(name, "forward") && !strings.Contains(name, "backward") {
		return plainGlyph(name)
	}
	parts := strings.Split(name, ".")
	found := false
	for i, p := range parts {
		switch p {
		case "forward":
			found = true
			if rtl {
				parts[i] = "backward"
			}
		case "backward":
			found = true
			if rtl {
				parts[i] = "forward"
			}
		}
	}
	if !found {
		return plainGlyph(name)
	}
	if g := plainGlyph(strings.Join(parts, ".")); g != 0 {
		return g
	}
	for i, p := range parts {
		switch p {
		case "forward":
			parts[i] = "right"
		case "backward":
			parts[i] = "left"
		}
	}
	return plainGlyph(strings.Join(parts, "."))
}

// The language the app chose (ui.SetLocale): Windows has no per-app
// language, so the host keeps it, in the app's data directory, and reports it
// first in the environment's Languages from the next launch on.

const localeName = "locale"

// localeFile is where the app's language is kept, or "" if it isn't: not for
// a test (go test) or an automation run (ASUKA_SNAPSHOT, ASUKA_SCRIPT), as the
// window's placement isn't.
func localeFile() string {
	if testing.Testing() || snapshotting() || os.Getenv("ASUKA_SCRIPT") != "" {
		return ""
	}
	d := os.Getenv("ASUKA_DATA") // as store.Dir: state.go sets it
	if d == "" {
		return ""
	}
	return filepath.Join(d, localeName)
}

// readSavedLocale reads the kept language: "" if there's none, or what's
// kept names no language.
func readSavedLocale(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	tag := strings.TrimSpace(string(b))
	if i18n.Normalize(tag) == "" {
		return ""
	}
	return tag
}

// saveLocale is the SaveLocale command: the language the app chose, "" to
// follow the user's languages again.
func (h *Host) saveLocale(tag string) {
	if tag == h.savedLocale {
		return
	}
	h.savedLocale = tag
	if h.localePath != "" {
		if tag == "" {
			os.Remove(h.localePath)
		} else {
			os.MkdirAll(filepath.Dir(h.localePath), 0o755)
			os.WriteFile(h.localePath, []byte(tag), 0o644)
		}
	}
	h.environmentChanged()
}

// intl is what the environment says of the user's languages and formats,
// read once and again as they change (WM_SETTINGCHANGE "intl"), not as the
// window resizes.
type intl struct {
	locale       string
	languages    []string
	firstWeekday uint8
	hourCycle    uint8
}

func readIntl() intl {
	return intl{
		locale:       strings.ReplaceAll(LocaleName(), "-", "_"),
		languages:    PreferredLanguages(),
		firstWeekday: FirstWeekday(),
		hourCycle:    HourCycle(),
	}
}

// joinLanguages is the environment's Languages: the app's own language
// first, then the user's without it.
func joinLanguages(own string, user []string) string {
	if own == "" {
		return strings.Join(user, ",")
	}
	list := make([]string, 0, len(user)+1)
	list = append(list, own)
	for _, t := range user {
		if !strings.EqualFold(t, own) {
			list = append(list, t)
		}
	}
	return strings.Join(list, ",")
}
