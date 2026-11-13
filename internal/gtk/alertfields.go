//go:build linux && !android

package gtk

// What host/linux's alerts with text fields use: libadwaita's entry rows
// in a boxed list, as the alert's extra child.

var (
	AdwAlertDialogSetExtraChild    func(d, child Ptr)
	AdwEntryRowNew                 func() Ptr
	AdwPasswordEntryRowNew         func() Ptr
	AdwEntryRowSetActivatesDefault func(r Ptr, a bool)
	AdwEntryRowSetInputPurpose     func(r Ptr, p int32)
	AdwEntryRowSetInputHints       func(r Ptr, h int32)
	AdwDialogSetFocus              func(d, w Ptr)
)

func init() {
	bindings = append(bindings,
		binding{&AdwAlertDialogSetExtraChild, "adw_alert_dialog_set_extra_child"},
		binding{&AdwEntryRowNew, "adw_entry_row_new"},
		binding{&AdwPasswordEntryRowNew, "adw_password_entry_row_new"},
		binding{&AdwEntryRowSetActivatesDefault, "adw_entry_row_set_activates_default"},
		binding{&AdwEntryRowSetInputPurpose, "adw_entry_row_set_input_purpose"},
		binding{&AdwEntryRowSetInputHints, "adw_entry_row_set_input_hints"},
		binding{&AdwDialogSetFocus, "adw_dialog_set_focus"},
	)
}
