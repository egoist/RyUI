// Conformance shows package conformance's sheet, for its runners to
// capture (e2e's TestConformance*), or to look at:
//
//	go run ./cmd/asuka dev --platform ios conformance/app
package main

import (
	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/conformance"
	"github.com/egoist/asuka/ui"
)

func main() {
	app.Run(func() ui.Node {
		return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter},
			ui.StaticText("Conformance"),
			conformance.Sheet())
	})
}
