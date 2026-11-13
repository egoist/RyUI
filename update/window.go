package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/markdown"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Window is the updater of a desktop app, Sparkle's, in a window of its
// own. Put it in the app's root (the node takes no space): while it's
// mounted, it checks for updates in the background (soon after launch,
// then every Options.Interval, unless AutomaticChecks is off), and when a
// version is out the window shows its release notes and offers Install
// Update, Skip This Version (background checks don't offer it again, but
// offer the next) and Remind Me Later (the next check offers it again),
// with "Automatically download and install updates in the future"
// (AutomaticDownloads: checks in the background then install what they
// find, which runs at the next launch). Installing shows the download's
// progress, then offers Relaunch Now. CheckForUpdates checks as the user
// asked, and the window says when the app is up to date, or can't update
// itself (Supported's error) or failed.
//
// Where the app can't update itself (a development build, an app a
// package manager installed, a phone), it never checks in the background.
// Its words are Asuka's, in the app's language (internal/l10n: an app
// changes them with "asuka.<key>" in its catalogs, as
// "asuka.updateInstall"). Main thread, in a component.
func Window(o Options) ui.Node {
	c := &ctl
	c.start(o)
	signal.OnCleanup(c.end)
	return ui.Window(ui.WindowProps{
		Open:            c.open,
		Title:           say("softwareUpdate", nil),
		Width:           statusWidth,
		Center:          true,
		DisableMinimize: true,
		DisableMaximize: true,
		OnClose:         c.close,
		Content:         func() ui.Node { return c.content() },
	})
}

// The window's sizes: a status's, and a release's, with its notes.
const (
	statusWidth, statusHeight   = 440, 150
	releaseWidth, releaseHeight = 620, 460
)

// MenuItem is "Check for Updates…", which calls CheckForUpdates: put it in
// the app's menu (AppMenu), or a tray icon's.
func MenuItem() ui.MenuItem {
	return ui.MenuItem{Title: l10n.Catalog.Format(ui.Locale(), "checkForUpdates", nil), OnPress: CheckForUpdates}
}

// AppMenu is the menu MenuItem goes in, for ui.MenuBar: on the Mac, the
// app's menu, after About (a menu titled as the app adds its items
// there); elsewhere Help, as Windows and GNOME apps have it.
func AppMenu() ui.MenuBarMenu {
	title := "Help"
	if runtime.GOOS == "darwin" {
		title = appName()
	}
	return ui.MenuBarMenu{Title: title, Items: []ui.MenuItem{MenuItem()}}
}

// say is the word of key in the app's language, which it follows.
func say(key string, args func() map[string]any) signal.Accessor[string] {
	return signal.Func(func() string {
		var a map[string]any
		if args != nil {
			a = args()
		}
		return l10n.Catalog.Format(ui.Locale(), key, a)
	})
}

// appName is the app's name, asuka.json's.
func appName() string {
	if boot.Name != "" {
		return boot.Name
	}
	return strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
}

// content is the window's: the view of the updater's phase.
func (c *controller) content() ui.Node {
	win := ui.UseWindow()
	phase := signal.NewMemo(func() Phase { return c.state.Get().Phase })
	signal.Effect(func() {
		if p := phase.Get(); p == Available {
			win.SetSize(releaseWidth, releaseHeight)
		} else {
			win.SetSize(statusWidth, statusHeight)
		}
		win.Center()
	})
	// A view of its own (Clip): a window holds one.
	return ui.Column(ui.Style{Grow: 1, Clip: true}, ui.Dynamic(func() ui.Node {
		switch phase.Get() {
		case Checking:
			return c.progressView(say("updateChecking", nil), nil, "cancel", c.close)
		case Downloading:
			return c.progressView(say("updateDownloading", nil), c.fraction(), "cancel", c.close)
		case Installing:
			return c.progressView(say("updateInstalling", nil), nil, "", nil)
		case Available:
			return c.releaseView()
		case Ready:
			return c.statusView(say("updateReady", nil), say("updateReadyMessage", c.releaseArgs), "",
				button{"updateNotNow", ui.ButtonGray, c.close}, button{"updateRelaunch", ui.ButtonFilled, c.relaunch})
		case UpToDate:
			return c.statusView(say("updateUpToDate", nil), say("updateUpToDateMessage", func() map[string]any {
				return map[string]any{"app": appName(), "version": Version()}
			}), "", button{"ok", ui.ButtonFilled, c.close})
		case Failed:
			s := c.state.Peek()
			key := "updateCheckError"
			if s.Installing {
				key = "updateInstallError"
			}
			return c.statusView(say("updateError", nil), say(key, nil), errText(s.Err), button{"ok", ui.ButtonFilled, c.close})
		case Unavailable:
			return c.statusView(say("updateUnavailable", nil), say("updateUnavailableMessage", func() map[string]any {
				return map[string]any{"app": appName()}
			}), errText(c.state.Peek().Err), button{"ok", ui.ButtonFilled, c.close})
		}
		return ui.View(ui.ViewProps{})
	}))
}

// releaseArgs are the arguments of a message of the release.
func (c *controller) releaseArgs() map[string]any {
	v := ""
	if r := c.state.Get().Release; r != nil {
		v = r.Version
	}
	return map[string]any{"app": appName(), "version": v, "current": Version()}
}

// errText is the detail of an error, for the curious, without the
// package's prefix.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(err.Error(), "update: ")
}

// fraction is the download's part done, 0 to 1.
func (c *controller) fraction() signal.Accessor[float64] {
	return signal.Func(func() float64 {
		s := c.state.Get()
		if s.Total <= 0 {
			return 0
		}
		return min(float64(s.Downloaded)/float64(s.Total), 1)
	})
}

type button struct {
	key     string
	variant ui.ButtonVariant
	press   func()
}

func (b button) node() ui.Node {
	return ui.Button(ui.ButtonProps{Title: say(b.key, nil), Variant: b.variant, OnPress: b.press})
}

// icon is the app's image, when Options has one.
func (c *controller) icon(size float32) []ui.Node {
	if c.opts.Icon == "" {
		return nil
	}
	return []ui.Node{ui.Image(ui.ImageProps{Source: ui.Val(c.opts.Icon), Style: ui.Style{Width: ui.Pt(size), Height: ui.Pt(size), Shrink: -1}})}
}

// statusView is a title, a message, a detail (an error's) and buttons.
func (c *controller) statusView(title, message signal.Accessor[string], detail string, buttons ...button) ui.Node {
	text := []ui.Node{
		ui.Text(title, ui.Style{Font: ui.Headline}),
		ui.Text(message, ui.Style{Font: ui.Callout}),
	}
	if detail != "" {
		text = append(text, ui.StaticText(detail, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel, Lines: 3}))
	}
	var bs []ui.Node
	for _, b := range buttons {
		bs = append(bs, b.node())
	}
	body := append(c.icon(48), ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 6}, text...))
	return ui.Column(ui.Style{Padding: ui.All(20), Gap: 16, Grow: 1},
		ui.Row(ui.Style{Gap: 16, Align: ui.AlignStart}, body...),
		ui.Row(ui.Style{Gap: 8, Justify: ui.JustifyEnd}, bs...),
	)
}

// progressView is a title, a progress bar (nil: under way, how far
// unknown), and Cancel.
func (c *controller) progressView(title signal.Accessor[string], value signal.Accessor[float64], cancelKey string, cancel func()) ui.Node {
	col := []ui.Node{
		ui.Text(title, ui.Style{Font: ui.Headline}),
		ui.Progress(value, ui.Style{Width: ui.Pct(100)}),
	}
	if value != nil {
		col = append(col, ui.Text(signal.Func(func() string {
			s := c.state.Get()
			return l10n.Catalog.Format(ui.Locale(), "updateProgress", map[string]any{"done": formatSize(s.Downloaded), "total": formatSize(s.Total)})
		}), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}))
	}
	rows := []ui.Node{ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter}, append(c.icon(48), ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 8}, col...))...)}
	if cancel != nil {
		rows = append(rows, ui.Row(ui.Style{Justify: ui.JustifyEnd}, button{cancelKey, ui.ButtonGray, cancel}.node()))
	}
	return ui.Column(ui.Style{Padding: ui.All(20), Gap: 16, Grow: 1}, rows...)
}

// releaseView offers the release: its notes, Skip This Version, Remind Me
// Later and Install Update, and the choice to install the next ones
// without asking.
func (c *controller) releaseView() ui.Node {
	automatic := signal.New(AutomaticDownloads())
	notes := ""
	if r := c.state.Peek().Release; r != nil {
		notes = r.Notes
	}
	text := []ui.Node{
		ui.Text(say("updateAvailable", func() map[string]any { return map[string]any{"app": appName()} }), ui.Style{Font: ui.Headline}),
		ui.Text(say("updateAvailableMessage", c.releaseArgs), ui.Style{Font: ui.Callout}),
	}
	col := []ui.Node{ui.Row(ui.Style{Gap: 16, Align: ui.AlignStart}, append(c.icon(64), ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 6}, text...))...)}
	if strings.TrimSpace(notes) != "" {
		col = append(col,
			ui.Text(say("updateReleaseNotes", nil), ui.Style{Font: ui.Footnote.Weighted(ui.Semibold)}),
			ui.Scroll(ui.ScrollProps{
				Style:        ui.Style{Grow: 1, Shrink: 1, Basis: ui.Pt(0), BorderWidth: 1, BorderColor: ui.Separator, CornerRadius: 6, Background: ui.SecondaryBackground},
				ContentStyle: ui.Style{Padding: ui.All(12)},
			}, markdown.View(markdown.Props{Text: ui.Val(notes)})),
		)
	} else {
		col = append(col, ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}))
	}
	col = append(col,
		ui.Row(ui.Style{Gap: 6, Align: ui.AlignCenter},
			ui.Toggle(ui.ToggleProps{Value: automatic, Checkbox: true}),
			ui.Text(say("updateAutomatically", nil), ui.Style{Font: ui.Callout, Shrink: 1}),
		),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter},
			button{"updateSkip", ui.ButtonGray, func() { c.skip(automatic.Peek()) }}.node(),
			ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}),
			button{"updateRemindLater", ui.ButtonGray, func() { c.remindLater(automatic.Peek()) }}.node(),
			button{"updateInstall", ui.ButtonFilled, func() { c.installOffered(automatic.Peek()) }}.node(),
		),
	)
	return ui.Column(ui.Style{Padding: ui.All(20), Gap: 12, Grow: 1}, col...)
}

// formatSize is a size in megabytes, as "12.3 MB" (kilobytes below one).
func formatSize(n int64) string {
	if n < 1<<20 {
		return strconv.FormatInt((n+1023)/1024, 10) + " KB"
	}
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
}
