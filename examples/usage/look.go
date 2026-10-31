package main

import (
	"strconv"
	"strings"

	"github.com/egoist/ryui/device"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// The app's controls are the platform's own; what it draws around them
// (cards, grouped rows, headings, meters) follows each platform's
// design: UIKit's inset grouped lists on iOS, Material 3's on Android.
var (
	ios bool
	// iosMajor is iOS's version: iOS 26 rounds its cells more.
	iosMajor = signal.New(26)
	// hairline is a separator's width: a pixel.
	hairline float32 = 0.5
)

// useLook notes the platform, from the root's environment.
func useLook() {
	env := signal.Untrack(ui.UseEnvironment().Get)
	ios = env.Platform == ui.PlatformIOS
	if env.Scale > 0 {
		hairline = 1 / env.Scale
	}
	if ios {
		go func() {
			info, err := device.GetInfo()
			if err != nil {
				return
			}
			major, _, _ := strings.Cut(info.SystemVersion, ".")
			if v, err := strconv.Atoi(major); err == nil {
				ui.Post(func() { iosMajor.Set(v) })
			}
		}()
	}
}

// radius is a card's or a group's corner radius.
func radius() float32 {
	switch {
	case !ios:
		return 28
	case iosMajor.Get() >= 26:
		return 26
	}
	return 10
}

// page is a screen's background: iOS's grouped one; Android's surface.
func page() ui.Color {
	if ios {
		return ui.GroupedBackground
	}
	return 0
}

// card is a box of content on the page.
func card(style ui.Style, kids ...ui.Node) ui.Node {
	return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		s := style
		s.Background, s.CornerRadius = ui.SecondaryGroupedBackground, radius()
		return s
	})}, kids...)
}

// section is a group's heading: small capitals on iOS, the accent on
// Android.
func section(title string) ui.Node {
	if ios {
		return ui.StaticText(strings.ToUpper(title), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel,
			Margin: ui.Edges{Top: 18, Left: 16, Right: 16, Bottom: -4}})
	}
	return ui.StaticText(title, ui.Style{Font: ui.Subheadline.Weighted(ui.Semibold), Color: ui.Accent,
		Margin: ui.Edges{Top: 16, Left: 4}})
}

// note is a group's footnote.
func note(text string) ui.Node {
	m := ui.Symmetric(0, 4)
	if ios {
		m = ui.Edges{Left: 16, Right: 16, Top: -4}
	}
	return ui.StaticText(text, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel, Margin: m})
}

// group is rows together: on iOS one rounded box, its rows apart by
// separators; on Android Material 3's segmented list, rows apart by a
// gap, rounded at the ends.
func group(rows ...ui.Node) ui.Node {
	if !ios {
		return ui.Column(ui.Style{Gap: 2, CornerRadius: 28, Clip: true}, rows...)
	}
	// Each row's separator is at its bottom: the last one's is pulled
	// out of the box, which clips it.
	return card(ui.Style{Clip: true}, ui.Column(ui.Style{Margin: ui.Edges{Bottom: -1}}, rows...))
}

// cell is a row of a group, its content laid out by style.
func cell(style ui.Style, kids ...ui.Node) ui.Node {
	return cellWith(ui.ViewProps{Style: style}, kids...)
}

// cellWith is a cell with p's events and accessibility; p.Style is a
// Style.
func cellWith(p ui.ViewProps, kids ...ui.Node) ui.Node {
	if !ios {
		s := p.Style.(ui.Style)
		s.Background = ui.SecondaryBackground
		p.Style = s
		return ui.View(p, kids...)
	}
	return ui.View(p, append(kids, separator())...)
}

// separator is a cell's bottom line, inset as UIKit's are.
func separator() ui.Node {
	return ui.View(ui.ViewProps{Decorative: true, Style: ui.Style{Absolute: true, Left: ui.Pt(16), Right: ui.Pt(0), Bottom: ui.Pt(0),
		Height: ui.Pt(hairline), Background: ui.Separator}})
}

// row is a cell with its content side by side.
func row(kids ...ui.Node) ui.Node {
	if ios {
		return cell(ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter, Padding: ui.Symmetric(11, 16), MinHeight: ui.Pt(48)}, kids...)
	}
	return cell(ui.Style{Direction: ui.RowDir, Gap: 16, Align: ui.AlignCenter, Padding: ui.Symmetric(10, 20), MinHeight: ui.Pt(60)}, kids...)
}

// stacked is a cell with its content stacked: a control under its
// label.
func stacked(kids ...ui.Node) ui.Node {
	if ios {
		return cell(ui.Style{Gap: 10, Padding: ui.Symmetric(12, 16)}, kids...)
	}
	return cell(ui.Style{Gap: 14, Padding: ui.Symmetric(18, 20)}, kids...)
}

// chevron says a row opens another screen.
func chevron() ui.Node {
	if ios {
		return ui.Symbol("chevron.right", ui.Style{Font: ui.Footnote.Weighted(ui.Semibold), Color: ui.TertiaryLabel})
	}
	return ui.Symbol("chevron.right", ui.Style{Color: ui.SecondaryLabel})
}
