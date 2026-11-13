//go:build linux && !android

package gtk

// What host/linux's Keys routing calls: a key event's layout and level,
// the key's keyval without modifiers, a widget's object data (whether an
// input method composes in it).

var Key struct {
	EventControllerGetCurrentEvent func(c Ptr) Ptr
	KeyEventGetLevel               func(ev Ptr) uint32
	KeyEventGetLayout              func(ev Ptr) uint32
	KeyEventGetConsumedModifiers   func(ev Ptr) uint32
	KeyEventIsModifier             func(ev Ptr) bool
	DisplayTranslateKey            func(d Ptr, keycode, state uint32, group int32, keyval *uint32, effGroup, level *int32, consumed *uint32) bool
	KeyvalToLower                  func(k uint32) uint32
	ObjectSetData                  func(o Ptr, key string, data Ptr)
	ObjectGetData                  func(o Ptr, key string) Ptr
}

// The modifiers GDK 4 has besides ModShift, ModControl, ModAlt, ModSuper
// and ModMeta.
const (
	ModLock  = 1 << 1
	ModHyper = 1 << 27
)

func init() {
	bindings = append(bindings,
		binding{&Key.EventControllerGetCurrentEvent, "gtk_event_controller_get_current_event"},
		binding{&Key.KeyEventGetLevel, "gdk_key_event_get_level"},
		binding{&Key.KeyEventGetLayout, "gdk_key_event_get_layout"},
		binding{&Key.KeyEventGetConsumedModifiers, "gdk_key_event_get_consumed_modifiers"},
		binding{&Key.KeyEventIsModifier, "gdk_key_event_is_modifier"},
		binding{&Key.DisplayTranslateKey, "gdk_display_translate_key"},
		binding{&Key.KeyvalToLower, "gdk_keyval_to_lower"},
		binding{&Key.ObjectSetData, "g_object_set_data"},
		binding{&Key.ObjectGetData, "g_object_get_data"},
	)
}
