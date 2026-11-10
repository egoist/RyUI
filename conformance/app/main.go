// Conformance shows package conformance's sheet, for its runners to
// capture (e2e's TestConformance*), or to look at:
//
//	go run ./cmd/ryui dev -platform ios conformance/app
package main

import (
	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/conformance"
	"github.com/egoist/ryui/ui"
)

func main() {
	app.Run(func() ui.Node {
		return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter},
			ui.StaticText("Conformance"),
			conformance.Sheet())
	})
}
