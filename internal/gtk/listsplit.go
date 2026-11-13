//go:build linux && !android

package gtk

// What host/linux's lists, split views and popovers use: the sidebar's
// GtkListBox, libadwaita's split view, timed animations of a property
// (a list scrolling itself).

var (
	ListBoxNew                        func() Ptr
	ListBoxAppend                     func(b, row Ptr)
	ListBoxRemove                     func(b, row Ptr)
	ListBoxSelectRow                  func(b, row Ptr)
	ListBoxUnselectAll                func(b Ptr)
	ListBoxGetSelectedRow             func(b Ptr) Ptr
	ListBoxGetRowAtIndex              func(b Ptr, i int32) Ptr
	ListBoxSetSelectionMode           func(b Ptr, mode int32)
	ListBoxRowNew                     func() Ptr
	ListBoxRowSetChild                func(r, child Ptr)
	ListBoxRowSetSelectable           func(r Ptr, s bool)
	ListBoxRowSetActivatable          func(r Ptr, a bool)
	ListBoxRowGetIndex                func(r Ptr) int32
	WidgetSetMarginTop                func(w Ptr, m int32)
	WidgetSetMarginBottom             func(w Ptr, m int32)
	WidgetSetMarginStart              func(w Ptr, m int32)
	WidgetSetMarginEnd                func(w Ptr, m int32)
	ScrolledWindowSetMinContentHeight func(s Ptr, h int32)

	AdwNavigationSplitViewGetCollapsed            func(v Ptr) bool
	AdwNavigationSplitViewSetSidebarWidthFraction func(v Ptr, f float64)
	AdwNavigationSplitViewGetShowContent          func(v Ptr) bool

	AdwPropertyAnimationTargetNew func(object Ptr, property string) Ptr
	AdwTimedAnimationNew          func(widget Ptr, from, to float64, duration uint32, target Ptr) Ptr
	AdwTimedAnimationSetEasing    func(a Ptr, easing int32)
	AdwAnimationPlay              func(a Ptr)
	AdwAnimationSkip              func(a Ptr)
	AdwAnimationGetState          func(a Ptr) int32

	FnPopoverPresent uintptr
	popoverType      GType
)

// GtkSelectionMode.
const (
	SelectionNone     = 0
	SelectionSingle   = 1
	SelectionBrowse   = 2
	SelectionMultiple = 3
)

// AdwEasing's ease-out cubic.
const EaseOutCubic = 2

// GtkShortcutScope.
const (
	ShortcutScopeLocal   = 0
	ShortcutScopeManaged = 1
	ShortcutScopeGlobal  = 2
)

func init() {
	bindings = append(bindings,
		binding{&ListBoxNew, "gtk_list_box_new"},
		binding{&ListBoxAppend, "gtk_list_box_append"},
		binding{&ListBoxRemove, "gtk_list_box_remove"},
		binding{&ListBoxSelectRow, "gtk_list_box_select_row"},
		binding{&ListBoxUnselectAll, "gtk_list_box_unselect_all"},
		binding{&ListBoxGetSelectedRow, "gtk_list_box_get_selected_row"},
		binding{&ListBoxGetRowAtIndex, "gtk_list_box_get_row_at_index"},
		binding{&ListBoxSetSelectionMode, "gtk_list_box_set_selection_mode"},
		binding{&ListBoxRowNew, "gtk_list_box_row_new"},
		binding{&ListBoxRowSetChild, "gtk_list_box_row_set_child"},
		binding{&ListBoxRowSetSelectable, "gtk_list_box_row_set_selectable"},
		binding{&ListBoxRowSetActivatable, "gtk_list_box_row_set_activatable"},
		binding{&ListBoxRowGetIndex, "gtk_list_box_row_get_index"},
		binding{&WidgetSetMarginTop, "gtk_widget_set_margin_top"},
		binding{&WidgetSetMarginBottom, "gtk_widget_set_margin_bottom"},
		binding{&WidgetSetMarginStart, "gtk_widget_set_margin_start"},
		binding{&WidgetSetMarginEnd, "gtk_widget_set_margin_end"},
		binding{&ScrolledWindowSetMinContentHeight, "gtk_scrolled_window_set_min_content_height"},
		binding{&AdwNavigationSplitViewGetCollapsed, "adw_navigation_split_view_get_collapsed"},
		binding{&AdwNavigationSplitViewSetSidebarWidthFraction, "adw_navigation_split_view_set_sidebar_width_fraction"},
		binding{&AdwNavigationSplitViewGetShowContent, "adw_navigation_split_view_get_show_content"},
		binding{&AdwPropertyAnimationTargetNew, "adw_property_animation_target_new"},
		binding{&AdwTimedAnimationNew, "adw_timed_animation_new"},
		binding{&AdwTimedAnimationSetEasing, "adw_timed_animation_set_easing"},
		binding{&AdwAnimationPlay, "adw_animation_play"},
		binding{&AdwAnimationSkip, "adw_animation_skip"},
		binding{&AdwAnimationGetState, "adw_animation_get_state"},
	)
	raws = append(raws, struct {
		p    *uintptr
		name string
	}{&FnPopoverPresent, "gtk_popover_present"})
}

// IsPopover reports whether w is a GtkPopover (a native its parent
// presents as it's allocated, rather than allocating it).
func IsPopover(w Ptr) bool {
	if popoverType == 0 {
		popoverType = Call(Sym("gtk_popover_get_type"))
	}
	return TypeCheckInstanceIsA(w, popoverType)
}
