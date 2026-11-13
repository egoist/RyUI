//go:build linux && !android

// The segmented control on Linux: Go, calling GTK 4 through purego. Not
// built for Android: GOOS=android satisfies linux, hence !android.

package segmented

import (
	"github.com/ebitengine/purego"
)

func init() {
	implementSegmented(newSegmented)
}

// GTK 4 and GObject, which the host has loaded.
var (
	boxNew            func(orientation int32, spacing int32) uintptr
	boxAppend         func(box, child uintptr)
	boxRemove         func(box, child uintptr)
	boxSetHomogeneous func(box uintptr, homogeneous bool)
	widgetAddCSSClass func(widget uintptr, class string)
	toggleNew         func(label string) uintptr
	toggleSetGroup    func(button, group uintptr)
	toggleSetActive   func(button uintptr, active bool)
	toggleGetActive   func(button uintptr) bool
	signalConnect     func(instance uintptr, signal string, handler, data, destroy uintptr, flags uint32) uint64
	onToggled         uintptr // the callback every button's toggled calls
)

func loadGTK() {
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	gobject, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&boxNew, gtk, "gtk_box_new")
	purego.RegisterLibFunc(&boxAppend, gtk, "gtk_box_append")
	purego.RegisterLibFunc(&boxRemove, gtk, "gtk_box_remove")
	purego.RegisterLibFunc(&boxSetHomogeneous, gtk, "gtk_box_set_homogeneous")
	purego.RegisterLibFunc(&widgetAddCSSClass, gtk, "gtk_widget_add_css_class")
	purego.RegisterLibFunc(&toggleNew, gtk, "gtk_toggle_button_new_with_label")
	purego.RegisterLibFunc(&toggleSetGroup, gtk, "gtk_toggle_button_set_group")
	purego.RegisterLibFunc(&toggleSetActive, gtk, "gtk_toggle_button_set_active")
	purego.RegisterLibFunc(&toggleGetActive, gtk, "gtk_toggle_button_get_active")
	purego.RegisterLibFunc(&signalConnect, gobject, "g_signal_connect_data")
	// One callback for every button: purego's callbacks are never freed.
	onToggled = purego.NewCallback(func(button, data uintptr) uintptr {
		if b, ok := buttons[button]; ok && !b.s.setting && toggleGetActive(button) {
			b.s.selected = b.index
			b.s.events.Changed(b.index)
		}
		return 0
	})
}

// segmented is GNOME's segmented buttons: a box of the "linked" style
// holding toggle buttons of one group.
type segmented struct {
	events   SegmentedEvents
	box      uintptr
	buttons  []uintptr
	selected int
	setting  bool // GTK emits toggled for a change made in code too
}

// segment is a button's control and its index there.
type segment struct {
	s     *segmented
	index int
}

// buttons are the buttons shown, by their GtkToggleButton.
var buttons = map[uintptr]segment{}

func newSegmented(events SegmentedEvents) SegmentedNative {
	if boxNew == nil {
		loadGTK()
	}
	box := boxNew(0, 0) // GTK_ORIENTATION_HORIZONTAL
	widgetAddCSSClass(box, "linked")
	boxSetHomogeneous(box, true) // the buttons share its width equally
	return &segmented{events: events, box: box}
}

func (s *segmented) Handle() uintptr { return s.box }

func (s *segmented) SetItems(items []string) {
	s.setting = true
	defer func() { s.setting = false }()
	s.forget()
	for i, t := range items {
		b := toggleNew(t)
		if i > 0 {
			toggleSetGroup(b, s.buttons[0])
		}
		signalConnect(b, "toggled", onToggled, 0, 0, 0)
		buttons[b] = segment{s, i}
		boxAppend(s.box, b)
		s.buttons = append(s.buttons, b)
	}
	s.selectNow()
}

func (s *segmented) SetSelected(selected int) {
	s.setting = true
	defer func() { s.setting = false }()
	s.selected = selected
	s.selectNow()
}

// selectNow selects: props come in no set order, items may follow
// selected.
func (s *segmented) selectNow() {
	if s.selected >= 0 && s.selected < len(s.buttons) {
		toggleSetActive(s.buttons[s.selected], true)
	}
}

// forget removes the buttons: the box held the only reference.
func (s *segmented) forget() {
	for _, b := range s.buttons {
		delete(buttons, b)
		boxRemove(s.box, b)
	}
	s.buttons = s.buttons[:0]
}

// Destroy forgets the buttons; the host destroys the box, and them with
// it.
func (s *segmented) Destroy() {
	for _, b := range s.buttons {
		delete(buttons, b)
	}
}
