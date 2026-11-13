//go:build linux && !android

package gtk

// AdwToast's (libadwaita 1.0; use-markup 1.4): a snackbar's toast, its
// button and its time. Bound by Load, as funcs.go's.
var (
	AdwToastSetButtonLabel func(t Ptr, label string)
	AdwToastSetTimeout     func(t Ptr, seconds uint32)
	AdwToastSetUseMarkup   func(t Ptr, use bool)
	AdwToastSetTitle       func(t Ptr, title string)
	AdwToastDismiss        func(t Ptr)
	AdwGetMinorVersion     func() uint32
)

func init() {
	bindings = append(bindings,
		binding{&AdwToastSetButtonLabel, "adw_toast_set_button_label"},
		binding{&AdwToastSetTimeout, "adw_toast_set_timeout"},
		binding{&AdwToastSetUseMarkup, "adw_toast_set_use_markup"},
		binding{&AdwToastSetTitle, "adw_toast_set_title"},
		binding{&AdwToastDismiss, "adw_toast_dismiss"},
		binding{&AdwGetMinorVersion, "adw_get_minor_version"},
	)
}
