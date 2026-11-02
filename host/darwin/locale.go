//go:build darwin && !ios

package darwin

import (
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/ryui/i18n"
	"github.com/egoist/ryui/internal/boot"
	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/ui"
)

// Languages and direction on a Mac.
//
// The language the app shows comes from the core (the Locale command), and
// frames are always physical: the host only mirrors what it draws itself.
//
// AppKit's own idea of the direction, NSApplication's
// userInterfaceLayoutDirection, is read once and never changes. It decides
// what the host can't set: the traffic lights, the title, the toolbar's order
// and NSTabView's. So the host works it out before the application starts,
// as AppKit's own -NSForceRightToLeftWritingDirection would, and an app whose
// language reads right to left is a right to left app from its first frame
// (appRTL). A language that changes direction later reaches what AppKit lets
// a view take (setUserInterfaceLayoutDirection:), and the host orders the
// toolbar and the tabs itself; the traffic lights stay until the next launch.

// NSWritingDirection.
const (
	writingNatural = ^uintptr(0) // -1
	writingLTR     = 0
	writingRTL     = 1
)

// NSTextAlignment: center and right swap places on Intel Macs.
var alignLeft, alignRight, alignCenter, alignJustified = func() (l, r, c, j uintptr) {
	if runtime.GOARCH == "amd64" {
		return 0, 1, 2, 3
	}
	return 0, 2, 1, 3
}()

// refreshLocale reads what the environment reports of the user's languages
// and formats. They're kept: the window reports its environment as it's
// resized, and they change rarely, with a notification.
func (h *Host) refreshLocale() {
	h.languages = strings.Join(preferredLanguages(), ",")
	h.firstWeekday, h.hourCycle = 0, 0
	if d := SendInt(Send(Class("NSCalendar"), "currentCalendar"), "firstWeekday"); d >= 1 && d <= 7 {
		h.firstWeekday = uint8(d)
	}
	format := Send(Class("NSDateFormatter"), "dateFormatFromTemplate:options:locale:",
		uintptr(String("j")), 0, uintptr(Send(Class("NSLocale"), "autoupdatingCurrentLocale")))
	h.hourCycle = hourCycle(GoString(format))
}

// preferredLanguages are the languages the user reads, most preferred first.
// NSLocale's are the app's own alone when its defaults have an AppleLanguages
// (SaveLocale's, or the language System Settings gives the app), which isn't
// all the user reads: the system's follow them.
func preferredLanguages() []string {
	var out []string
	add := func(list ID) {
		for _, s := range ArrayItems(list) {
			if t := GoString(s); t != "" && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	add(Send(Class("NSLocale"), "preferredLanguages"))
	defaults := Send(Class("NSUserDefaults"), "standardUserDefaults")
	if global := Send(defaults, "persistentDomainForName:", uintptr(String("NSGlobalDomain"))); global != 0 {
		if list := Send(global, "objectForKey:", uintptr(String("AppleLanguages"))); list != 0 && SendBool(list, "isKindOfClass:", uintptr(Class("NSArray"))) {
			add(list)
		}
	}
	return out
}

// hourCycle is that of a date format pattern: 12 for h and K, 24 for H and k,
// 0 for none. Quoted text isn't a field.
func hourCycle(pattern string) uint8 {
	quoted := false
	for _, c := range pattern {
		switch {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == 'h' || c == 'K':
			return 12
		case c == 'H' || c == 'k':
			return 24
		}
	}
	return 0
}

// launchRTL works out what the core will say of the first language the app
// shows, before there's a core to ask. It may be wrong (the app called
// SetLocale before it runs): that's a language changing later.
func launchRTL() bool {
	if boot.Lang != "" || boot.Direction != "" || os.Getenv("RYUI_LOCALE") != "" || os.Getenv("RYUI_DIRECTION") != "" {
		return ui.IsRTL()
	}
	var want []string
	for _, t := range preferredLanguages() {
		if n := i18n.Normalize(t); n != "" {
			want = append(want, n)
		}
	}
	have := ui.Languages()
	m := i18n.Match(want, have)
	if m == "" {
		m = have[0]
	}
	return i18n.Direction(m) == i18n.RightToLeft
}

var directionLatched bool

// latchDirection makes AppKit a right to left app if the app's language reads
// that way. AppKit reads it once, before the first view, from the defaults'
// argument domain among others, and the window's title bar reads it again as
// each window is made, so it stays there. Before the application is made.
func latchDirection() {
	if directionLatched {
		return
	}
	directionLatched = true
	if !launchRTL() {
		return
	}
	defaults := Send(Class("NSUserDefaults"), "standardUserDefaults")
	name := uintptr(Const(Foundation(), "NSArgumentDomain"))
	was := Send(defaults, "volatileDomainForName:", name)
	forced := Send(Class("NSMutableDictionary"), "dictionary")
	if was != 0 {
		Send(forced, "addEntriesFromDictionary:", uintptr(was))
		Send(defaults, "removeVolatileDomainForName:", name)
	}
	Send(forced, "setObject:forKey:", uintptr(NumberBool(true)), uintptr(String("AppleTextDirection")))
	Send(forced, "setObject:forKey:", uintptr(NumberBool(true)), uintptr(String("NSForceRightToLeftWritingDirection")))
	Send(defaults, "setVolatileDomain:forName:", uintptr(forced), name)
	SendInt(Send(Class("NSApplication"), "sharedApplication"), "userInterfaceLayoutDirection")
}

// setLocale is the Locale command: the language the app shows, and the way it
// reads.
func (h *Host) setLocale(args []byte) {
	if len(args) == 0 {
		return
	}
	rtl, tag := args[0]&1 != 0, string(args[1:])
	dirChanged, tagChanged := rtl != h.rtl, tag != h.locale
	h.rtl, h.locale = rtl, tag
	if dirChanged {
		for _, n := range h.nodes {
			h.applyDirection(n)
		}
	}
	if !dirChanged && !tagChanged {
		return
	}
	// The host's own words, the toolbar and the menus are made of them.
	h.makeMenu(Send(Class("NSApplication"), "sharedApplication"))
	h.updateWindowChrome()
}

// saveLocale is the SaveLocale command: AppKit reads AppleLanguages of the
// app's defaults at launch, as System Settings' language of an app writes it.
// Without it, NSLocale's are the system's again at once.
func (h *Host) saveLocale(tag string) {
	defaults := Send(Class("NSUserDefaults"), "standardUserDefaults")
	if tag == "" {
		Send(defaults, "removeObjectForKey:", uintptr(String("AppleLanguages")))
	} else {
		Send(defaults, "setObject:forKey:", uintptr(Array(String(tag))), uintptr(String("AppleLanguages")))
	}
	Send(defaults, "synchronize")
	h.refreshLocale()
	h.afterCore(h.environmentChanged)
}

// menuWords are the words of the standard menus that AppKit has in its own
// tables, in the languages macOS speaks: each one's table and key. RyUI's
// catalog has the others (internal/l10n).
var menuWords = map[string][2]string{
	"Services":           {"Services", "Services"},
	"Show All":           {"Common", "Show All"},
	"File":               {"MenuCommands", "File"},
	"Close Window":       {"MenuCommands", "Close Window"},
	"Edit":               {"Document", "Edit"},
	"Undo":               {"Undo", "&Undo"}, // marked for an access key
	"Redo":               {"Undo", "&Redo"},
	"Cut":                {"MenuCommands", "Cut"},
	"Copy":               {"MenuCommands", "Copy"},
	"Paste":              {"MenuCommands", "Paste"},
	"Select All":         {"MenuCommands", "Select All"},
	"View":               {"MenuCommands", "View"},
	"Enter Full Screen":  {"MenuCommands", "Enter Full Screen"},
	"Window":             {"MenuCommands", "Window"},
	"Minimize":           {"MenuCommands", "Minimize"},
	"Bring All to Front": {"MenuCommands", "Bring All to Front"},
	"Help":               {"MenuCommands", "Help"},
}

// menuWord says a word of the standard menus in the language shown, as AppKit
// does: it doesn't translate a main menu made at run time. English where its
// tables don't have the language or the word.
func (h *Host) menuWord(english string) string {
	if h.locale == "" {
		return english
	}
	if i18n.IsPseudo(h.locale) {
		return i18n.MustParseMessage(english).Format(h.locale, nil)
	}
	src, ok := menuWords[english]
	bundle := Send(Class("NSBundle"), "bundleForClass:", uintptr(Class("NSApplication")))
	if !ok || bundle == 0 || !RespondsTo(bundle, "localizedStringForKey:value:table:localizations:") {
		return english
	}
	s := Send(bundle, "localizedStringForKey:value:table:localizations:", uintptr(String(src[1])), uintptr(String(english)),
		uintptr(String(src[0])), uintptr(Array(String(h.locale))))
	return strings.ReplaceAll(GoString(s), "&", "")
}

// setWindowTitle is the WindowTitle command: the main window's title when no
// screen names it.
func (h *Host) setWindowTitle(title string) {
	if title == "" {
		title = h.appName
	}
	h.main.base = title
	h.updateWindowChrome()
}

// isRTL reports whether n reads right to left: its own direction (a
// WithDirection's), or the app's.
func (h *Host) isRTL(n *node) bool {
	switch n.dir {
	case ops.LayoutDirectionLeftToRight:
		return false
	case ops.LayoutDirectionRightToLeft:
		return true
	}
	return h.rtl
}

func direction(rtl bool) uintptr { return Bool(rtl) }

// applyDirection makes n's view read its way: what it lays out itself, and
// its text. A view doesn't take the direction from its parent: each has its
// own, which starts as the application's.
func (h *Host) applyDirection(n *node) {
	if n.view == 0 {
		return
	}
	rtl := h.isRTL(n)
	changed := rtl != n.rtl
	n.rtl = rtl
	Send(n.view, "setUserInterfaceLayoutDirection:", direction(rtl))
	switch n.kind {
	case ops.KindText, ops.KindTextField:
		h.applyTextAlign(n)
	case ops.KindButton:
		if n.textAlign != 0 {
			h.applyTextAlign(n)
		}
	case ops.KindWheel:
		setMenuDirection(Send(n.view, "menu"), rtl)
	case ops.KindTabs:
		h.orderTabs(n)
	case ops.KindRichText:
		if changed {
			n.rich.directionChanged(h, n)
		}
	case ops.KindSymbol:
		if changed {
			Send(n.view, "setNeedsDisplay:", 1)
		}
	case ops.KindScroll:
		if changed && n.horizontal {
			h.mirrorScroll(n, rtl)
		}
	case ops.KindList:
		if changed {
			n.list.rebind = true
			n.list.queue(h)
		}
	}
	if n.menu != 0 {
		setMenuDirection(n.menu, rtl)
	}
}

func setMenuDirection(m ID, rtl bool) {
	if m == 0 {
		return
	}
	Send(m, "setUserInterfaceLayoutDirection:", direction(rtl))
	for _, it := range ArrayItems(Send(m, "itemArray")) {
		setMenuDirection(Send(it, "submenu"), rtl)
	}
}

// alignment is the NSTextAlignment of n's TextAlign, whichever way the app
// reads. Natural isn't used: it follows the app's direction and its text, not
// the view's.
func (h *Host) alignment(n *node) uintptr {
	rtl := h.isRTL(n)
	switch ops.TextAlign(n.textAlign) {
	case ops.TextAlignCenter:
		return alignCenter
	case ops.TextAlignJustify:
		return alignJustified
	case ops.TextAlignEnd:
		rtl = !rtl
	}
	if rtl {
		return alignRight
	}
	return alignLeft
}

// writingDirection is the base writing direction of n's text: the text's own
// (its first strong character), so English in a right to left app reads
// "Hello, Ada!" at the right, unless a WithDirection says which.
func (h *Host) writingDirection(n *node) uintptr {
	switch n.dir {
	case ops.LayoutDirectionLeftToRight:
		return writingLTR
	case ops.LayoutDirectionRightToLeft:
		return writingRTL
	}
	return writingNatural
}

// applyTextAlign sets the alignment and the base writing direction of a text,
// a field or a multiline field's text view.
func (h *Host) applyTextAlign(n *node) {
	v := n.view
	if n.tracking != 0 {
		v = n.tracking
	}
	Send(v, "setAlignment:", h.alignment(n))
	if n.kind != ops.KindText && n.kind != ops.KindTextField {
		return
	}
	Send(v, "setBaseWritingDirection:", h.writingDirection(n))
	if n.tracking != 0 {
		// What's typed next takes the paragraph style of the text view's.
		para := New("NSMutableParagraphStyle")
		Send(para, "setAlignment:", h.alignment(n))
		Send(para, "setBaseWritingDirection:", h.writingDirection(n))
		Send(v, "setDefaultParagraphStyle:", uintptr(para))
		Release(para)
		Send(v, "setUserInterfaceLayoutDirection:", direction(h.isRTL(n)))
	}
}

// mirrorScroll is a horizontal Scroll going another way: once the core has
// laid it out again, what was the distance from one end is that from the
// other.
func (h *Host) mirrorScroll(n *node, rtl bool) {
	var gap float64
	if rtl {
		gap = MsgRect(Send(n.view, "contentView"), Sel("bounds")).Origin.X
	} else {
		gap = endGap(n)
	}
	h.afterCore(func() {
		if h.nodes[n.id] == n && n.view != 0 {
			WithPool(func() { h.scrollFromStart(n, gap) })
		}
	})
}

// horizontalScroll is the horizontal Scroll that a frame set on n changes the
// content or the width of (nil: none). A left to right one only matters where
// AppKit reads right to left: it would move to the content's end.
func (h *Host) horizontalScroll(n *node) *node {
	s := n
	if n.kind != ops.KindScroll {
		s = n.parent
	}
	if s == nil || s.kind != ops.KindScroll || !s.horizontal || !h.isRTL(s) && !h.appRTL {
		return nil
	}
	return s
}

// startGap is how far a horizontal Scroll is from the end it starts at: the
// right of its content reading right to left, else the left.
func (h *Host) startGap(s *node) float64 {
	if h.isRTL(s) {
		return endGap(s)
	}
	return MsgRect(Send(s.view, "contentView"), Sel("bounds")).Origin.X
}

// scrollFromStart scrolls it to that distance.
func (h *Host) scrollFromStart(s *node, gap float64) {
	if h.isRTL(s) {
		scrollFromEnd(s, gap)
	} else {
		scrollToX(s, gap)
	}
}

// endGap is how far a horizontal Scroll is from the end of its content (0:
// it shows the end).
func endGap(s *node) float64 {
	doc := Send(s.view, "documentView")
	if doc == 0 {
		return 0
	}
	clip := MsgRect(Send(s.view, "contentView"), Sel("bounds"))
	return max(MsgRect(doc, Sel("frame")).Size.Width-clip.Size.Width-clip.Origin.X, 0)
}

// scrollFromEnd scrolls a horizontal Scroll to gap from the end of its
// content.
func scrollFromEnd(s *node, gap float64) {
	doc := Send(s.view, "documentView")
	if doc == 0 {
		return
	}
	clip := MsgRect(Send(s.view, "contentView"), Sel("bounds"))
	scrollToX(s, max(MsgRect(doc, Sel("frame")).Size.Width-clip.Size.Width-gap, 0))
}

func scrollToX(s *node, x float64) {
	clip := Send(s.view, "contentView")
	b := MsgRect(clip, Sel("bounds"))
	if b.Origin.X == x {
		return
	}
	MsgScrollPoint(clip, Sel("scrollToPoint:"), Point{X: x, Y: b.Origin.Y})
	Send(s.view, "reflectScrolledClipView:", uintptr(clip))
}

// orderTabs puts a Tabs' items in the order they show. NSTabView follows the
// app's direction, not its own: where the two differ, the host reverses them.
func (h *Host) orderTabs(n *node) {
	rev := h.tabsReversed(n)
	if n.nav.reversed == rev {
		return
	}
	n.nav.reversed = rev
	if len(n.children) < 2 {
		return
	}
	n.nav.setting = true
	for _, c := range n.children {
		if c.nav != nil && c.nav.tabItem != 0 {
			Send(n.view, "removeTabViewItem:", uintptr(c.nav.tabItem))
		}
	}
	for i, c := range n.children {
		if c.nav != nil && c.nav.tabItem != 0 {
			Send(n.view, "insertTabViewItem:atIndex:", uintptr(c.nav.tabItem), uintptr(h.tabIndex(n, i, i+1)))
		}
	}
	h.selectTab(n)
	n.nav.setting = false
	h.layoutScreens(n)
}

func (h *Host) tabsReversed(n *node) bool { return h.isRTL(n) != h.appRTL }

// tabIndex is where the item of child i goes among count items of a Tabs.
func (h *Host) tabIndex(n *node, i, count int) int {
	if n.nav.reversed {
		return count - 1 - i
	}
	return i
}

// selectTab selects the selected child's item, wherever it is.
func (h *Host) selectTab(n *node) {
	if n.selected < len(n.children) {
		if c := n.children[n.selected]; c.nav != nil && c.nav.tabItem != 0 {
			Send(n.view, "selectTabViewItem:", uintptr(c.nav.tabItem))
		}
	}
}

// toolbarReversed reports whether the host orders the toolbar's items: AppKit
// orders them by the app's direction.
func (h *Host) toolbarReversed() bool { return h.rtl != h.appRTL }

// backSymbol is the back button's: it's on the side the app reads from, and
// points out of the window.
func (h *Host) backSymbol() string {
	if h.rtl {
		return "chevron.right"
	}
	return "chevron.left"
}

// barSymbol is the symbol a toolbar item shows of name. A forward or backward
// symbol mirrors with the app's direction, which AppKit's toolbar takes from
// the application: where the two differ, it's the other one.
func (h *Host) barSymbol(name string) ID {
	if h.toolbarReversed() {
		other := ""
		switch {
		case strings.Contains(name, "forward"):
			other = strings.Replace(name, "forward", "backward", 1)
		case strings.Contains(name, "backward"):
			other = strings.Replace(name, "backward", "forward", 1)
		}
		if other != "" {
			if img := symbolImage(other); img != 0 {
				return img
			}
		}
	}
	return symbolImage(name)
}
