package open // want package:"routes \\[\\] open true links 2"

// Its routes can't all be known (a function makes some): no link is
// reported.

import "github.com/egoist/asuka/ui"

func more() []ui.Route { return nil }

func App() ui.Node {
	return ui.Router(ui.RouterProps{Initial: "/anything"},
		ui.Route{Path: "/a", Layout: ui.LayoutStack, Children: more()},
	)
}

func Link() { ui.Navigate("/unknown") }
