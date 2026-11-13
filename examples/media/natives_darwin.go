//go:build darwin && !ios

// The app's native views for ui.Native on the Mac: Go, through purego's
// objc package, as its Swift is in ios/ and its Kotlin in android/. Not
// built for iOS: GOOS=ios satisfies darwin, hence !ios.

package main

import (
	"strings"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/ryui/native"
)

func init() {
	native.RegisterView("Segmented", newSegmented)
}

var sel = objc.RegisterName

// segmented is an NSSegmentedControl: props "items" (comma-separated)
// and "selected"; event "changed" with "index".
type segmented struct {
	ctx      native.Context
	control  objc.ID
	selected int
}

// segmenteds are the controls shown, by their NSSegmentedControl: the
// action's sender.
var segmenteds = map[objc.ID]*segmented{}

// segmentedTarget is every control's target, an object of the app's own
// class, whose changed: is their action.
var segmentedTarget objc.ID

func newSegmented(ctx native.Context) native.View {
	if segmentedTarget == 0 {
		cls, err := objc.RegisterClass("MediaSegmentedTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{
			Cmd: sel("changed:"),
			Fn: func(self objc.ID, cmd objc.SEL, sender objc.ID) {
				if s := segmenteds[sender]; s != nil {
					s.selected = objc.Send[int](sender, sel("selectedSegment"))
					s.ctx.Emit("changed", map[string]any{"index": s.selected})
				}
			},
		}})
		if err != nil {
			panic(err)
		}
		segmentedTarget = objc.ID(cls).Send(sel("new"))
	}
	c := objc.ID(objc.GetClass("NSSegmentedControl")).Send(sel("new"))
	c.Send(sel("setTrackingMode:"), 0)        // NSSegmentSwitchTrackingSelectOne
	c.Send(sel("setSegmentDistribution:"), 2) // NSSegmentDistributionFillEqually: as wide as its view
	c.Send(sel("setTarget:"), segmentedTarget)
	c.Send(sel("setAction:"), sel("changed:"))
	s := &segmented{ctx: ctx, control: c}
	segmenteds[c] = s
	return s
}

func (s *segmented) Handle() uintptr { return uintptr(s.control) }

func (s *segmented) Set(name string, value any) {
	switch name {
	case "items":
		items := strings.Split(value.(string), ",")
		s.control.Send(sel("setSegmentCount:"), len(items))
		for i, t := range items {
			label := objc.ID(objc.GetClass("NSString")).Send(sel("alloc")).Send(sel("initWithUTF8String:"), t)
			s.control.Send(sel("setLabel:forSegment:"), label, i)
			label.Send(sel("release"))
		}
	case "selected":
		n, _ := value.(int64)
		s.selected = int(n)
	default:
		return
	}
	// Props come in no set order: items may follow selected.
	s.control.Send(sel("setSelectedSegment:"), s.selected)
}

// Destroy lets the control go: new made it, and its superview holds it
// until the host removes it.
func (s *segmented) Destroy() {
	delete(segmenteds, s.control)
	s.control.Send(sel("release"))
}
