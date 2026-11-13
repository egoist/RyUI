//go:build linux && !android

package gtk

// What asuka/dialog and asuka/shell call of GTK and GIO (host/linux's
// modules_dialog.go): GtkFileDialog's save and folder methods and its
// start, name and button, GIO's trash, GDK's beep. Bound by Load.
var Dialogs struct {
	FileDialogSetInitialFolder func(d, folder Ptr)
	FileDialogSetInitialName   func(d Ptr, name string)
	FileDialogSetInitialFile   func(d, file Ptr)
	FileDialogSetAcceptLabel   func(d Ptr, label string)
	FileDialogSave             func(d, parent, cancellable, cb, data Ptr)
	FileDialogSaveFinish       func(d, res Ptr, err *Ptr) Ptr
	FileDialogSelectFolder     func(d, parent, cancellable, cb, data Ptr)
	FileDialogSelectFolderFin  func(d, res Ptr, err *Ptr) Ptr
	FileDialogSelectFolders    func(d, parent, cancellable, cb, data Ptr)
	FileDialogSelectFoldersFin func(d, res Ptr, err *Ptr) Ptr
	FileFilterAddPattern       func(f Ptr, pattern string)

	FileTrashAsync  func(f Ptr, priority int32, cancellable, cb, data Ptr)
	FileTrashFinish func(f, res Ptr, err *Ptr) bool

	DisplayBeep func(display Ptr)
}

func init() {
	d := &Dialogs
	bindings = append(bindings,
		binding{&d.FileDialogSetInitialFolder, "gtk_file_dialog_set_initial_folder"},
		binding{&d.FileDialogSetInitialName, "gtk_file_dialog_set_initial_name"},
		binding{&d.FileDialogSetInitialFile, "gtk_file_dialog_set_initial_file"},
		binding{&d.FileDialogSetAcceptLabel, "gtk_file_dialog_set_accept_label"},
		binding{&d.FileDialogSave, "gtk_file_dialog_save"},
		binding{&d.FileDialogSaveFinish, "gtk_file_dialog_save_finish"},
		binding{&d.FileDialogSelectFolder, "gtk_file_dialog_select_folder"},
		binding{&d.FileDialogSelectFolderFin, "gtk_file_dialog_select_folder_finish"},
		binding{&d.FileDialogSelectFolders, "gtk_file_dialog_select_multiple_folders"},
		binding{&d.FileDialogSelectFoldersFin, "gtk_file_dialog_select_multiple_folders_finish"},
		binding{&d.FileFilterAddPattern, "gtk_file_filter_add_pattern"},

		binding{&d.FileTrashAsync, "g_file_trash_async"},
		binding{&d.FileTrashFinish, "g_file_trash_finish"},

		binding{&d.DisplayBeep, "gdk_display_beep"},
	)
}
