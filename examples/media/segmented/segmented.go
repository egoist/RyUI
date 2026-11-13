// Package segmented is a native view: a segmented control, each
// platform's own (asuka native): UISegmentedControl on iOS,
// NSSegmentedControl on macOS (swift/), Material's segmented buttons on
// Android (android/), radio buttons on the web (web/), a tab control on
// Windows and GNOME's linked toggle buttons on Linux (Go).
package segmented

import (
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// SegmentedProps are a Segmented's: its items, which is selected, and
// what the user selects.
//
//asuka:view
type SegmentedProps struct {
	Items    []string
	Selected signal.Accessor[int]
	Changed  func(index int)
	Style    ui.Style
}
