package ui

import (
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/asuka/i18n"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// The locale model, as MyGo's: the app shows one language, which its
// messages, Asuka's own words and the host's views all follow, and the
// regional formats are a setting of their own. The language is the user's
// preferred languages (the environment's Languages), or the one the app
// chose (SetLocale), matched against the languages the app is written in
// (SetLanguages, else its catalogs'), and the first of those where none
// matches: an app written in English alone shows English to a reader of
// Chinese, as UIKit does. It's the process's, like time.Local: every root
// shows it, and a goroutine may ask for it.

// Region is the user's regional formats.
type Region = i18n.Region

// shownLocale is what the app shows. It's replaced whole on the main thread
// and read from any goroutine.
type shownLocale struct {
	locale    string
	rtl       bool
	preferred []string
	region    Region
}

var (
	shown atomic.Pointer[shownLocale]
	// All on the main thread:
	localeOverride  string   // SetLocale's, Lang's, or what dev keeps; "": the user's languages
	localeForced    bool     // Lang chose it: SetLocale keeps nothing and changes nothing before the first mount
	localeLoaded    bool     // the override was read
	appLanguages    []string // SetLanguages', or nil for the catalogs'
	lastEnv         ops.Environment
	pendingSave     *string // SetLocale before Mount
	directionForced string  // "rtl" or "ltr": the app's direction, whatever the language
)

func init() {
	i18n.SetLocaleFunc(Locale)
	i18n.SetFormatLocaleFunc(FormatLocale)
}

// Locale returns the language the app shows, as a BCP 47 tag of the
// languages it's written in, e.g. "zh-Hans". An effect, a memo or an
// accessor that calls it, or that says a message of the app's catalogs,
// which call it, runs again when the language changes. Safe from any
// goroutine.
func Locale() string {
	signal.Ambient()
	if s := shown.Load(); s != nil {
		return s.locale
	}
	return startingLocale()
}

// startingLocale is the language before anything is mounted: the one the
// CLI chose, else the app's own. Any goroutine.
func startingLocale() string {
	if tag := forcedLocale(); tag != "" {
		return tag
	}
	if langs := i18n.Languages(); len(langs) > 0 {
		return langs[0]
	}
	return "en"
}

func forcedLocale() string {
	tag := boot.Lang
	if tag == "" {
		tag = os.Getenv("ASUKA_LOCALE")
	}
	return i18n.Normalize(tag)
}

// SetLocale makes the app show the language of tag, a BCP 47 tag such as
// "de" or "zh-Hant-TW", of those it's written in, or "" to follow the user's
// languages again, which is the default. What follows the language changes
// at once: every message said in an accessor or an effect, the app's layout
// direction, Asuka's own words and the host's. It's kept for the next
// launches where the system keeps an app's language, which Settings shows:
// iOS's and macOS's AppleLanguages (UIKit and AppKit say their own words,
// as Cancel in a date picker, in it from the next launch) and Android 13's
// per-app language; Asuka keeps it elsewhere. asuka dev --lang chooses a
// language for one run, and then nothing is kept.
//
// The pseudo locales en-XA and ar-XB always show, to test what translations
// would do. SetLocale panics on a tag that names no language. Main thread;
// goroutines reach it with Post.
func SetLocale(tag string) {
	normalized := ""
	if strings.TrimSpace(tag) != "" {
		if normalized = i18n.Normalize(tag); normalized == "" {
			panic("ui: SetLocale: " + strings.TrimSpace(tag) + " is not a language tag")
		}
	}
	loadLocale()
	if !localeForced {
		switch {
		case boot.SaveLocale != nil:
			boot.SaveLocale(normalized)
		case len(roots) > 0:
			roots[0].batch.Command(roots[0].el.id, ops.CmdSaveLocale, []byte(normalized))
		default:
			pendingSave = &normalized
		}
	}
	localeOverride, localeForced = normalized, false
	updateLocale()
}

// SetLanguages sets the languages the app is written in, its own language
// first, which it falls back to: for an app that translates its text itself,
// without catalogs. By default they are the languages of its catalogs, and
// English alone without any. It panics on a tag that names no language.
// Main thread.
func SetLanguages(tags ...string) {
	for _, t := range tags {
		if i18n.Normalize(t) == "" {
			panic("ui: SetLanguages: " + strings.TrimSpace(t) + " is not a language tag")
		}
	}
	appLanguages = slices.Clone(tags)
	updateLocale()
}

// Languages returns the languages the app is written in, its own first: for
// a menu of languages, each named by i18n.Autonym. Main thread, or before
// the app runs.
func Languages() []string {
	if len(appLanguages) > 0 {
		return slices.Clone(appLanguages)
	}
	if langs := i18n.Languages(); len(langs) > 0 {
		return langs
	}
	return []string{"en"}
}

// PreferredLanguages returns the languages the user reads, most preferred
// first, as normalized BCP 47 tags: the system's, with the app's own first
// where the system keeps one. An effect that calls it follows them. Safe
// from any goroutine.
func PreferredLanguages() []string {
	signal.Ambient()
	if s := shown.Load(); s != nil {
		return slices.Clone(s.preferred)
	}
	return nil
}

// IsRTL reports whether the app is laid out right to left, as the language
// it shows is written, unless asuka dev --dir says otherwise. Row runs from
// the right then, Start is the right side, and the platform's views mirror.
// An effect that calls it follows it. Safe from any goroutine.
func IsRTL() bool {
	signal.Ambient()
	if s := shown.Load(); s != nil {
		return s.rtl
	}
	return rtlOf(startingLocale())
}

func rtlOf(locale string) bool {
	switch forcedDirection() {
	case "rtl":
		return true
	case "ltr":
		return false
	}
	return i18n.Direction(locale) == i18n.RightToLeft
}

func forcedDirection() string {
	if directionForced != "" {
		return directionForced
	}
	if boot.Direction != "" {
		return boot.Direction
	}
	return os.Getenv("ASUKA_DIRECTION")
}

// CurrentRegion returns the user's regional formats, with the changes they
// made in the system's settings: the locale of the formats, which may be
// another language's than the app's, the first day of the week and the hour
// cycle. What the system doesn't say comes from CLDR's data for the region.
// An effect that calls it follows them. Safe from any goroutine.
func CurrentRegion() Region {
	signal.Ambient()
	if s := shown.Load(); s != nil {
		return s.region
	}
	return i18n.RegionFor(startingLocale())
}

// FormatLocale returns the locale to format numbers and dates in with
// package i18n: the language the app shows, written the way the user set
// their formats (Region.FormatLocale), as en-GB-u-fw-mon-hc-h23 for English
// read with British formats:
//
//	ui.Text(signal.Func(func() string {
//		return i18n.FormatDate(ui.FormatLocale(), due.Get(), i18n.DateOptions{Date: i18n.MediumStyle})
//	}))
//
// An effect that calls it follows them. Safe from any goroutine.
func FormatLocale() string {
	signal.Ambient()
	if s := shown.Load(); s != nil {
		return s.region.FormatLocale(s.locale)
	}
	return startingLocale()
}

// loadLocale reads the language chosen for this run, or what dev keeps, once.
func loadLocale() {
	if localeLoaded {
		return
	}
	localeLoaded = true
	if tag := forcedLocale(); tag != "" {
		localeOverride, localeForced = tag, true
	} else if boot.SavedLocale != nil {
		localeOverride = i18n.Normalize(boot.SavedLocale())
	}
}

// environmentLocale tells the locale what the host reported.
func environmentLocale(env ops.Environment) {
	if env.Languages == lastEnv.Languages && env.Locale == lastEnv.Locale &&
		env.FirstWeekday == lastEnv.FirstWeekday && env.HourCycle == lastEnv.HourCycle && shown.Load() != nil {
		return
	}
	lastEnv = env
	updateLocale()
}

// updateLocale works out what the app shows and, if it changed, what follows
// it does.
func updateLocale() {
	loadLocale()
	next := &shownLocale{preferred: preferredOf(lastEnv)}
	next.locale = resolveLocale(localeOverride, next.preferred, Languages())
	next.rtl = rtlOf(next.locale)
	next.region = regionOf(lastEnv, next.preferred)
	before := shown.Swap(next)
	if before != nil && before.locale == next.locale && before.rtl == next.rtl && before.region == next.region &&
		slices.Equal(before.preferred, next.preferred) {
		return
	}
	if before == nil || before.locale != next.locale || before.rtl != next.rtl {
		for _, r := range roots {
			r.sendLocale(next, before == nil || before.rtl != next.rtl)
		}
	}
	signal.AmbientChanged()
}

// preferredOf returns the environment's languages, or its locale's for a
// host that reports none.
func preferredOf(env ops.Environment) []string {
	list := strings.Split(env.Languages, ",")
	if env.Languages == "" {
		list = []string{regionLocale(env.Locale)}
	}
	var out []string
	for _, t := range list {
		if n := i18n.Normalize(t); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// resolveLocale returns the language of supported to show a reader of
// preferred, or of override and then preferred when the app chose one: the
// pseudo locales as they are, the best match, else the app's own.
func resolveLocale(override string, preferred, supported []string) string {
	want := preferred
	if override != "" {
		if i18n.IsPseudo(override) {
			return override
		}
		want = append([]string{override}, preferred...)
	}
	if m := i18n.Match(want, supported); m != "" {
		return m
	}
	return supported[0]
}

// regionOf returns the environment's formats, with what the system doesn't
// say filled in from CLDR's week and time data for the locale's region (or
// its language's likely one).
func regionOf(env ops.Environment, preferred []string) Region {
	out := Region{Locale: regionLocale(env.Locale), FirstWeekday: time.Monday, HourCycle: int(env.HourCycle)}
	if out.Locale == "" && len(preferred) > 0 {
		out.Locale = preferred[0]
	}
	if env.FirstWeekday >= 1 && env.FirstWeekday <= 7 {
		out.FirstWeekday = time.Weekday(env.FirstWeekday - 1)
	} else if out.Locale != "" {
		out.FirstWeekday = i18n.FirstWeekday(out.Locale)
	}
	if out.HourCycle != 12 && out.HourCycle != 24 {
		out.HourCycle = 24
		if out.Locale != "" {
			out.HourCycle = i18n.HourCycle(out.Locale)
		}
	}
	return out
}

// regionLocale normalizes the locale of regional formats, whose region the
// "rg" key of Apple's locales changes: en_US@rg=gbzzzz has the formats of
// en-GB.
func regionLocale(locale string) string {
	if i := strings.IndexByte(locale, '@'); i >= 0 {
		var rg string
		for _, kw := range strings.Split(locale[i+1:], ";") {
			if k, v, ok := strings.Cut(kw, "="); ok && strings.EqualFold(k, "rg") {
				rg = v
			}
		}
		base := i18n.Normalize(locale[:i])
		if len(rg) >= 2 && base != "" {
			lang := base
			if r := regionCode(base); r != "" {
				lang = strings.TrimSuffix(base, "-"+r)
			}
			if n := i18n.Normalize(lang + "-" + strings.ToUpper(rg[:2])); n != "" {
				return n
			}
		}
		locale = locale[:i]
	}
	return i18n.Normalize(locale)
}

// regionCode returns the region of a normalized tag, as "GB" of "en-GB", or
// "" without one.
func regionCode(tag string) string {
	parts := strings.Split(tag, "-")
	for _, p := range parts[1:] {
		if len(p) == 2 && p[0] >= 'A' && p[0] <= 'Z' || len(p) == 3 && p[0] >= '0' && p[0] <= '9' {
			return p
		}
	}
	return ""
}

// sendLocale tells the host the language the root shows and, when the
// direction changed, lays everything out again in it.
func (r *Root) sendLocale(s *shownLocale, dirChanged bool) {
	var flags byte
	if s.rtl {
		flags = 1
	}
	args := make([]byte, 0, len(s.locale)+1)
	args = append(args, flags)
	args = append(args, s.locale...)
	r.batch.Command(r.el.id, ops.CmdLocale, args)
	if dirChanged {
		r.applyDirection()
	}
}

// rootDirection is the direction of a layout root.
func rootDirection(rtl bool) layout.Direction {
	if rtl {
		return layout.DirRTL
	}
	return layout.DirLTR
}

// layoutDirection is what e's box is told: only a layout root (it has no
// parent to inherit from) and a WithDirection's say.
func (e *element) layoutDirection() layout.Direction {
	switch e.dir {
	case LeftToRight:
		return layout.DirLTR
	case RightToLeft:
		return layout.DirRTL
	}
	if e == e.r.el || e.hostSized || e.cell || e.inline {
		return rootDirection(isRTLNow())
	}
	return layout.DirInherit
}

// isRTLNow is IsRTL, for the core, which follows it itself.
func isRTLNow() bool {
	if s := shown.Load(); s != nil {
		return s.rtl
	}
	return rtlOf(startingLocale())
}

// rtl reports whether e's logical sides are mirrored.
func (e *element) rtl() bool {
	switch e.dir {
	case LeftToRight:
		return false
	case RightToLeft:
		return true
	}
	return isRTLNow()
}

// applyDirection gives every layout root its direction, derives again the
// styles that depend on it (logical sides, a horizontal Scroll's), and
// throws the layout caches away, whose inputs don't say it.
func (r *Root) applyDirection() {
	var visit func(e *element)
	visit = func(e *element) {
		if e.box != nil && (e.adjust != nil || e.style.logical()) {
			e.box.SetStyle(e.layoutStyle(&e.style))
		}
		for _, k := range e.kids {
			if k.id == 0 || k.frag {
				visit(k)
			}
		}
	}
	for _, e := range elems {
		if e.r != r || e.dead {
			continue
		}
		visit(e)
		if e.box == nil {
			continue
		}
		if e == r.el || e.hostSized || e.cell || e.inline {
			// What's derived above already has it (and a Scroll's own say).
			if e.adjust == nil && !e.style.logical() {
				st := e.box.Style
				st.Direction = e.layoutDirection()
				e.box.SetStyle(st)
			}
			e.box.MarkSubtreeDirty()
		}
	}
}

// LayoutDirection is the way a subtree lays out its rows and aligns its text.
type LayoutDirection = ops.LayoutDirection

const (
	// InheritDirection is the app's: its language's.
	InheritDirection = ops.LayoutDirectionInherit
	LeftToRight      = ops.LayoutDirectionLeftToRight
	RightToLeft      = ops.LayoutDirectionRightToLeft
)

// dirCtx is the direction a WithDirection gives what's built in it.
var dirCtx = signal.NewContext[LayoutDirection](InheritDirection)

// dirOverrides counts the WithDirections so far: without any, elements don't
// look the context up.
var dirOverrides int

// WithDirection builds a subtree in direction d, whatever the app's: what
// reads one way in every language, as a phone number, a media player's
// controls or a code editor. It takes a function, as what's built in it must
// know as it's made: each view gets the direction, as a platform's view
// doesn't take it from its parent.
//
//	ui.WithDirection(ui.LeftToRight, func() ui.Node { return playerControls() })
func WithDirection(d LayoutDirection, build func() Node) Node {
	dirOverrides++
	var n Node
	dirCtx.Provide(d, func() {
		n = build()
		if n.e == nil || n.e.frag {
			n = Column(Style{}, n)
		}
	})
	return n
}
