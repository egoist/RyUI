//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
)

// ColorPicker's well: the color panel is the app's one (NSColorPanel's
// shared panel), so a well's title (ColorPicker.Title) and whether it
// shows opacity (Alpha) go to the panel as the well activates (a click
// opens the panel for it), and at once if it's the active one.

type colorWellProps struct {
	title string
	alpha bool
}

var (
	colorWells       = map[ID]*colorWellProps{} // by their RyUIColorWell
	colorWellClassOK bool
)

// newColorWell makes a ColorPicker's view.
func newColorWell() ID {
	if !colorWellClassOK {
		colorWellClassOK = true
		DefineClass("RyUIColorWell", "NSColorWell", nil, []objc.MethodDef{
			Method("activate:", func(self ID, _ SEL, exclusive bool) {
				SendSuper(self, "RyUIColorWell", "activate:", Bool(exclusive))
				applyColorWell(self)
			}),
		})
	}
	v := initView("RyUIColorWell")
	colorWells[v] = &colorWellProps{}
	return v
}

// setColorWell sets a well's title or opacity, applied to the panel if
// the well is the one it's for.
func setColorWell(v ID, f func(p *colorWellProps)) {
	p := colorWells[v]
	if p == nil {
		return
	}
	f(p)
	if SendBool(v, "isActive") {
		applyColorWell(v)
	}
}

func applyColorWell(v ID) {
	p := colorWells[v]
	if p == nil {
		return
	}
	panel := Send(Class("NSColorPanel"), "sharedColorPanel")
	Send(panel, "setShowsAlpha:", Bool(p.alpha))
	title := p.title
	if title == "" {
		title = colorPanelTitle()
	}
	Send(panel, "setTitle:", uintptr(String(title)))
}

var defaultColorPanelTitle string

// colorPanelTitle is the panel's own title ("Colors", in the user's
// language), as it was before a well named it.
func colorPanelTitle() string {
	if defaultColorPanelTitle == "" {
		defaultColorPanelTitle = GoString(Send(Send(Class("NSColorPanel"), "sharedColorPanel"), "title"))
	}
	return defaultColorPanelTitle
}

// forgetColorWell drops a released well's props.
func forgetColorWell(v ID) {
	if p := colorWells[v]; p != nil {
		if SendBool(v, "isActive") {
			Send(v, "deactivate")
		}
		delete(colorWells, v)
	}
}
