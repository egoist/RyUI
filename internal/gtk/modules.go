//go:build linux && !android

package gtk

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"unsafe"

	"github.com/ebitengine/purego"
)

// What the native modules (host/linux's modules_*.go) call of GLib, GIO
// and GTK: files, file dialogs and launchers, notifications, the network
// monitor, the clipboard's formats, D-Bus and GVariant. They're in Sys,
// a namespace of their own, bound by Load like the rest.
var Sys struct {
	// GLib.
	Strdup              func(s string) Ptr
	HashTableNewFull    func(hash, equal, keyFree, valueFree Ptr) Ptr
	HashTableInsert     func(h, k, v Ptr) bool
	HashTableUnref      func(h Ptr)
	GetUserSpecialDir   func(dir int32) Ptr
	GetUserCacheDir     func() Ptr
	ValueSetDouble      func(v *Value, d float64)
	ValueSetBoxed       func(v *Value, p Ptr)
	ValueSetInt         func(v *Value, i int32)
	ValueSetObject      func(v *Value, o Ptr)
	ValueSetUint64      func(v *Value, i uint64)
	ValueSetInt64       func(v *Value, i int64)
	ValueGetInt64       func(v *Value) int64
	ValueGetUint64      func(v *Value) uint64
	ErrorMatches        func(e Ptr, domain uint32, code int32) bool
	DBusErrorQuark      func() uint32
	DBusErrorGetRemote  func(e Ptr) Ptr
	DBusErrorIsRemote   func(e Ptr) bool
	FileGetTypeOfObject func() GType

	// GIO: files, apps.
	FileNewForPath              func(path string) Ptr
	FileNewForURI               func(uri string) Ptr
	FileGetPath                 func(f Ptr) Ptr
	FileGetURI                  func(f Ptr) Ptr
	FileIconNew                 func(f Ptr) Ptr
	ThemedIconNew               func(name string) Ptr
	ListStoreNew                func(t GType) Ptr
	ListStoreAppend             func(s, item Ptr)
	ListModelGetNItems          func(m Ptr) uint32
	ListModelGetItem            func(m Ptr, i uint32) Ptr
	AppInfoGetDefaultForScheme  func(scheme string) Ptr
	AppInfoGetDefaultForType    func(contentType string, mustSupportURIs bool) Ptr
	ContentTypeFromMIME         func(mime string) Ptr
	NetworkMonitorGetDefault    func() Ptr
	NetworkMonitorAvailable     func(m Ptr) bool
	NetworkMonitorMetered       func(m Ptr) bool
	NetworkMonitorConnectivity  func(m Ptr) int32
	NotificationNew             func(title string) Ptr
	NotificationSetBody         func(n Ptr, body string)
	NotificationSetIcon         func(n, icon Ptr)
	NotificationSetPriority     func(n Ptr, p int32)
	NotificationSetCategory     func(n Ptr, c string)
	NotificationSetDefaultTV    func(n Ptr, action string, target Ptr)
	NotificationAddButtonTV     func(n Ptr, label, action string, target Ptr)
	ApplicationSendNotification func(app Ptr, id string, n Ptr)
	ApplicationWithdrawNotif    func(app Ptr, id string)
	ApplicationGetIsRegistered  func(app Ptr) bool
	ApplicationGetID            func(app Ptr) Ptr
	ApplicationGetDBusConn      func(app Ptr) Ptr
	SimpleActionNew             func(name string, paramType Ptr) Ptr
	UnixFDListNewFromArray      func(fds *int32, n int32) Ptr

	// GTK: file dialogs, launchers.
	FileDialogNew              func() Ptr
	FileDialogSetTitle         func(d Ptr, t string)
	FileDialogSetModal         func(d Ptr, m bool)
	FileDialogSetFilters       func(d, filters Ptr)
	FileDialogSetDefaultFilter func(d, filter Ptr)
	FileDialogOpen             func(d, parent, cancellable, cb, data Ptr)
	FileDialogOpenFinish       func(d, res Ptr, err *Ptr) Ptr
	FileDialogOpenMultiple     func(d, parent, cancellable, cb, data Ptr)
	FileDialogOpenMultipleFin  func(d, res Ptr, err *Ptr) Ptr
	FileFilterNew              func() Ptr
	FileFilterSetName          func(f Ptr, name string)
	FileFilterAddMIMEType      func(f Ptr, mime string)
	FileFilterAddSuffix        func(f Ptr, suffix string)
	FileFilterGetType          func() GType
	FileLauncherNew            func(f Ptr) Ptr
	FileLauncherLaunch         func(l, parent, cancellable, cb, data Ptr)
	FileLauncherLaunchFinish   func(l, res Ptr, err *Ptr) bool
	URILauncherLaunchFinish    func(l, res Ptr, err *Ptr) bool
	ClipboardGetFormats        func(c Ptr) Ptr
	ContentFormatsContainGType func(f Ptr, t GType) bool
	ContentFormatsContainMIME  func(f Ptr, mime string) bool
	ClipboardReadTextureAsync  func(c, cancellable, cb, data Ptr)
	ClipboardReadTextureFinish func(c, res Ptr, err *Ptr) Ptr
	ClipboardSetTexture        func(c, texture Ptr)
	ClipboardSetValue          func(c Ptr, v *Value)
	TextureGetType             func() GType
	TextureSaveToPNGBytes      func(t Ptr) Ptr
	FileListGetType            func() GType
	FileListNewFromArray       func(files *Ptr, n uintptr) Ptr

	// D-Bus.
	BusGetSync           func(kind int32, cancellable Ptr, err *Ptr) Ptr
	ConnectionGetUnique  func(c Ptr) Ptr
	ConnectionCallFinish func(c, res Ptr, err *Ptr) Ptr
	ConnectionCallFDFin  func(c Ptr, outFDs *Ptr, res Ptr, err *Ptr) Ptr

	// GVariant.
	VariantNewString     func(s string) Ptr
	VariantNewObjectPath func(s string) Ptr
	VariantNewBytestring func(s string) Ptr
	VariantNewBoolean    func(b bool) Ptr
	VariantNewByte       func(b uint8) Ptr
	VariantNewInt32      func(i int32) Ptr
	VariantNewUint32     func(i uint32) Ptr
	VariantNewInt64      func(i int64) Ptr
	VariantNewUint64     func(i uint64) Ptr
	VariantNewDouble     func(d float64) Ptr
	VariantNewHandle     func(i int32) Ptr
	VariantNewVariant    func(v Ptr) Ptr
	VariantNewTuple      func(children *Ptr, n uintptr) Ptr
	VariantNewArray      func(childType Ptr, children *Ptr, n uintptr) Ptr
	VariantNewDictEntry  func(k, v Ptr) Ptr
	VariantRefSink       func(v Ptr) Ptr
	VariantUnref         func(v Ptr)
	VariantClassify      func(v Ptr) int32
	VariantNChildren     func(v Ptr) uintptr
	VariantGetChildValue func(v Ptr, i uintptr) Ptr
	VariantGetString     func(v Ptr, n *uintptr) Ptr
	VariantGetBoolean    func(v Ptr) bool
	VariantGetByte       func(v Ptr) uint8
	VariantGetInt16      func(v Ptr) int16
	VariantGetUint16     func(v Ptr) uint16
	VariantGetInt32      func(v Ptr) int32
	VariantGetUint32     func(v Ptr) uint32
	VariantGetInt64      func(v Ptr) int64
	VariantGetUint64     func(v Ptr) uint64
	VariantGetDouble     func(v Ptr) float64
	VariantGetHandle     func(v Ptr) int32
	VariantGetVariant    func(v Ptr) Ptr
	VariantGetTypeString func(v Ptr) Ptr
	VariantPrint         func(v Ptr, annotate bool) Ptr
	VariantParse         func(t Ptr, text string, limit, endptr Ptr, err *Ptr) Ptr
	VariantTypeNew       func(t string) Ptr
	VariantTypeFree      func(t Ptr)
}

// Raw: called through Call (string or NULL arguments).
var (
	FnStrHash, FnStrEqual, FnFree                  uintptr
	FnConnectionCall, FnConnectionCallWithFDs      uintptr
	FnSignalSubscribe, FnSignalUnsubscribe, FnEmit uintptr
)

func init() {
	raws = append(raws,
		struct {
			p    *uintptr
			name string
		}{&FnStrHash, "g_str_hash"},
		struct {
			p    *uintptr
			name string
		}{&FnStrEqual, "g_str_equal"},
		struct {
			p    *uintptr
			name string
		}{&FnFree, "g_free"},
		struct {
			p    *uintptr
			name string
		}{&FnConnectionCall, "g_dbus_connection_call"},
		struct {
			p    *uintptr
			name string
		}{&FnConnectionCallWithFDs, "g_dbus_connection_call_with_unix_fd_list"},
		struct {
			p    *uintptr
			name string
		}{&FnSignalSubscribe, "g_dbus_connection_signal_subscribe"},
		struct {
			p    *uintptr
			name string
		}{&FnSignalUnsubscribe, "g_dbus_connection_signal_unsubscribe"},
		struct {
			p    *uintptr
			name string
		}{&FnEmit, "g_dbus_connection_emit_signal"},
	)
	s := &Sys
	bindings = append(bindings,
		binding{&s.Strdup, "g_strdup"},
		binding{&s.HashTableNewFull, "g_hash_table_new_full"},
		binding{&s.HashTableInsert, "g_hash_table_insert"},
		binding{&s.HashTableUnref, "g_hash_table_unref"},
		binding{&s.GetUserSpecialDir, "g_get_user_special_dir"},
		binding{&s.GetUserCacheDir, "g_get_user_cache_dir"},
		binding{&s.ValueSetDouble, "g_value_set_double"},
		binding{&s.ValueSetBoxed, "g_value_set_boxed"},
		binding{&s.ValueSetInt, "g_value_set_int"},
		binding{&s.ValueSetObject, "g_value_set_object"},
		binding{&s.ValueSetUint64, "g_value_set_uint64"},
		binding{&s.ValueSetInt64, "g_value_set_int64"},
		binding{&s.ValueGetInt64, "g_value_get_int64"},
		binding{&s.ValueGetUint64, "g_value_get_uint64"},
		binding{&s.ErrorMatches, "g_error_matches"},
		binding{&s.DBusErrorQuark, "g_dbus_error_quark"},
		binding{&s.DBusErrorGetRemote, "g_dbus_error_get_remote_error"},
		binding{&s.DBusErrorIsRemote, "g_dbus_error_is_remote_error"},
		binding{&s.FileGetTypeOfObject, "g_file_get_type"},

		binding{&s.FileNewForPath, "g_file_new_for_path"},
		binding{&s.FileNewForURI, "g_file_new_for_uri"},
		binding{&s.FileGetPath, "g_file_get_path"},
		binding{&s.FileGetURI, "g_file_get_uri"},
		binding{&s.FileIconNew, "g_file_icon_new"},
		binding{&s.ThemedIconNew, "g_themed_icon_new"},
		binding{&s.ListStoreNew, "g_list_store_new"},
		binding{&s.ListStoreAppend, "g_list_store_append"},
		binding{&s.ListModelGetNItems, "g_list_model_get_n_items"},
		binding{&s.ListModelGetItem, "g_list_model_get_item"},
		binding{&s.AppInfoGetDefaultForScheme, "g_app_info_get_default_for_uri_scheme"},
		binding{&s.AppInfoGetDefaultForType, "g_app_info_get_default_for_type"},
		binding{&s.ContentTypeFromMIME, "g_content_type_from_mime_type"},
		binding{&s.NetworkMonitorGetDefault, "g_network_monitor_get_default"},
		binding{&s.NetworkMonitorAvailable, "g_network_monitor_get_network_available"},
		binding{&s.NetworkMonitorMetered, "g_network_monitor_get_network_metered"},
		binding{&s.NetworkMonitorConnectivity, "g_network_monitor_get_connectivity"},
		binding{&s.NotificationNew, "g_notification_new"},
		binding{&s.NotificationSetBody, "g_notification_set_body"},
		binding{&s.NotificationSetIcon, "g_notification_set_icon"},
		binding{&s.NotificationSetPriority, "g_notification_set_priority"},
		binding{&s.NotificationSetCategory, "g_notification_set_category"},
		binding{&s.NotificationSetDefaultTV, "g_notification_set_default_action_and_target_value"},
		binding{&s.NotificationAddButtonTV, "g_notification_add_button_with_target_value"},
		binding{&s.ApplicationSendNotification, "g_application_send_notification"},
		binding{&s.ApplicationWithdrawNotif, "g_application_withdraw_notification"},
		binding{&s.ApplicationGetIsRegistered, "g_application_get_is_registered"},
		binding{&s.ApplicationGetID, "g_application_get_application_id"},
		binding{&s.ApplicationGetDBusConn, "g_application_get_dbus_connection"},
		binding{&s.SimpleActionNew, "g_simple_action_new"},
		binding{&s.UnixFDListNewFromArray, "g_unix_fd_list_new_from_array"},

		binding{&s.FileDialogNew, "gtk_file_dialog_new"},
		binding{&s.FileDialogSetTitle, "gtk_file_dialog_set_title"},
		binding{&s.FileDialogSetModal, "gtk_file_dialog_set_modal"},
		binding{&s.FileDialogSetFilters, "gtk_file_dialog_set_filters"},
		binding{&s.FileDialogSetDefaultFilter, "gtk_file_dialog_set_default_filter"},
		binding{&s.FileDialogOpen, "gtk_file_dialog_open"},
		binding{&s.FileDialogOpenFinish, "gtk_file_dialog_open_finish"},
		binding{&s.FileDialogOpenMultiple, "gtk_file_dialog_open_multiple"},
		binding{&s.FileDialogOpenMultipleFin, "gtk_file_dialog_open_multiple_finish"},
		binding{&s.FileFilterNew, "gtk_file_filter_new"},
		binding{&s.FileFilterSetName, "gtk_file_filter_set_name"},
		binding{&s.FileFilterAddMIMEType, "gtk_file_filter_add_mime_type"},
		binding{&s.FileFilterAddSuffix, "gtk_file_filter_add_suffix"},
		binding{&s.FileFilterGetType, "gtk_file_filter_get_type"},
		binding{&s.FileLauncherNew, "gtk_file_launcher_new"},
		binding{&s.FileLauncherLaunch, "gtk_file_launcher_launch"},
		binding{&s.FileLauncherLaunchFinish, "gtk_file_launcher_launch_finish"},
		binding{&s.URILauncherLaunchFinish, "gtk_uri_launcher_launch_finish"},
		binding{&s.ClipboardGetFormats, "gdk_clipboard_get_formats"},
		binding{&s.ContentFormatsContainGType, "gdk_content_formats_contain_gtype"},
		binding{&s.ContentFormatsContainMIME, "gdk_content_formats_contain_mime_type"},
		binding{&s.ClipboardReadTextureAsync, "gdk_clipboard_read_texture_async"},
		binding{&s.ClipboardReadTextureFinish, "gdk_clipboard_read_texture_finish"},
		binding{&s.ClipboardSetTexture, "gdk_clipboard_set_texture"},
		binding{&s.ClipboardSetValue, "gdk_clipboard_set_value"},
		binding{&s.TextureGetType, "gdk_texture_get_type"},
		binding{&s.TextureSaveToPNGBytes, "gdk_texture_save_to_png_bytes"},
		binding{&s.FileListGetType, "gdk_file_list_get_type"},
		binding{&s.FileListNewFromArray, "gdk_file_list_new_from_array"},

		binding{&s.BusGetSync, "g_bus_get_sync"},
		binding{&s.ConnectionGetUnique, "g_dbus_connection_get_unique_name"},
		binding{&s.ConnectionCallFinish, "g_dbus_connection_call_finish"},
		binding{&s.ConnectionCallFDFin, "g_dbus_connection_call_with_unix_fd_list_finish"},

		binding{&s.VariantNewString, "g_variant_new_string"},
		binding{&s.VariantNewObjectPath, "g_variant_new_object_path"},
		binding{&s.VariantNewBytestring, "g_variant_new_bytestring"},
		binding{&s.VariantNewBoolean, "g_variant_new_boolean"},
		binding{&s.VariantNewByte, "g_variant_new_byte"},
		binding{&s.VariantNewInt32, "g_variant_new_int32"},
		binding{&s.VariantNewUint32, "g_variant_new_uint32"},
		binding{&s.VariantNewInt64, "g_variant_new_int64"},
		binding{&s.VariantNewUint64, "g_variant_new_uint64"},
		binding{&s.VariantNewDouble, "g_variant_new_double"},
		binding{&s.VariantNewHandle, "g_variant_new_handle"},
		binding{&s.VariantNewVariant, "g_variant_new_variant"},
		binding{&s.VariantNewTuple, "g_variant_new_tuple"},
		binding{&s.VariantNewArray, "g_variant_new_array"},
		binding{&s.VariantNewDictEntry, "g_variant_new_dict_entry"},
		binding{&s.VariantRefSink, "g_variant_ref_sink"},
		binding{&s.VariantUnref, "g_variant_unref"},
		binding{&s.VariantClassify, "g_variant_classify"},
		binding{&s.VariantNChildren, "g_variant_n_children"},
		binding{&s.VariantGetChildValue, "g_variant_get_child_value"},
		binding{&s.VariantGetString, "g_variant_get_string"},
		binding{&s.VariantGetBoolean, "g_variant_get_boolean"},
		binding{&s.VariantGetByte, "g_variant_get_byte"},
		binding{&s.VariantGetInt16, "g_variant_get_int16"},
		binding{&s.VariantGetUint16, "g_variant_get_uint16"},
		binding{&s.VariantGetInt32, "g_variant_get_int32"},
		binding{&s.VariantGetUint32, "g_variant_get_uint32"},
		binding{&s.VariantGetInt64, "g_variant_get_int64"},
		binding{&s.VariantGetUint64, "g_variant_get_uint64"},
		binding{&s.VariantGetDouble, "g_variant_get_double"},
		binding{&s.VariantGetHandle, "g_variant_get_handle"},
		binding{&s.VariantGetVariant, "g_variant_get_variant"},
		binding{&s.VariantGetTypeString, "g_variant_get_type_string"},
		binding{&s.VariantPrint, "g_variant_print"},
		binding{&s.VariantParse, "g_variant_parse"},
		binding{&s.VariantTypeNew, "g_variant_type_new"},
		binding{&s.VariantTypeFree, "g_variant_type_free"},
	)
}

// GError is a GError as a Go error, and frees it (0: nil).
func GError(gerr Ptr) error {
	if gerr == 0 {
		return nil
	}
	// GError: GQuark domain, gint code, gchar *message.
	msg := GoString(*At[Ptr](gerr + 8))
	if Sys.DBusErrorIsRemote != nil && Sys.DBusErrorIsRemote(gerr) {
		if name := GoStringFree(Sys.DBusErrorGetRemote(gerr)); name != "" {
			msg = name + ": " + msg
		}
	}
	ErrorFree(gerr)
	if msg == "" {
		msg = "unknown error"
	}
	return errors.New(msg)
}

// GErrorCode is a GError's domain and code (it isn't freed).
func GErrorCode(gerr Ptr) (domain uint32, code int32) {
	if gerr == 0 {
		return 0, 0
	}
	return *At[uint32](gerr), *At[int32](gerr + 4)
}

// IOErrorQuark is G_IO_ERROR's domain; a cancelled dialog's code is
// IOErrorCancelled, a dismissed GTK dialog's DialogErrorDismissed.
var ioErrorQuark, dialogErrorQuark uint32

const (
	IOErrorCancelled     = 19
	DialogErrorCancelled = 1
	DialogErrorDismissed = 2
)

// Cancelled reports whether a GError says the user cancelled (a dialog
// dismissed, an operation cancelled); it doesn't free it.
func Cancelled(gerr Ptr) bool {
	if gerr == 0 {
		return false
	}
	if ioErrorQuark == 0 {
		ioErrorQuark = uint32(Call(Sym("g_io_error_quark")))
		if p := Sym("gtk_dialog_error_quark"); p != 0 {
			dialogErrorQuark = uint32(Call(p))
		}
	}
	d, c := GErrorCode(gerr)
	return (d == ioErrorQuark && c == IOErrorCancelled) ||
		(d == dialogErrorQuark && (c == DialogErrorCancelled || c == DialogErrorDismissed))
}

// D-Bus.

// Buses.
const (
	BusSystem  = 1
	BusSession = 2
)

var modBuses = map[int32]Ptr{}

// Bus is the system's or the session's bus, connected once (0 and the
// error when there's none).
func Bus(kind int32) (Ptr, error) {
	if c := modBuses[kind]; c != 0 {
		return c, nil
	}
	var gerr Ptr
	c := Sys.BusGetSync(kind, 0, &gerr)
	if c == 0 {
		return 0, GError(gerr)
	}
	modBuses[kind] = c
	return c, nil
}

// UniqueName is the connection's unique name (":1.42").
func UniqueName(conn Ptr) string { return GoString(Sys.ConnectionGetUnique(conn)) }

// DBusCall calls a method, asynchronously: done gets the reply (a
// tuple, valid during done) or the error, on the main thread. params is
// a floating tuple (VTuple), or 0.
func DBusCall(conn Ptr, dest, path, iface, method string, params Ptr, timeoutMS int32, done func(reply Ptr, err error)) {
	d, p, i, m := CString(dest), CString(path), CString(iface), CString(method)
	Async(func(a AsyncCall) {
		Call(FnConnectionCall, conn, Addr(d), Addr(p), Addr(i), Addr(m), params, 0, 0, uintptr(timeoutMS), 0, a.Callback, a.Data)
		runtime.KeepAlive(d)
		runtime.KeepAlive(p)
		runtime.KeepAlive(i)
		runtime.KeepAlive(m)
	}, func(_, res Ptr) {
		var gerr Ptr
		r := Sys.ConnectionCallFinish(conn, res, &gerr)
		if r == 0 {
			done(0, GError(gerr))
			return
		}
		done(r, nil)
		Sys.VariantUnref(r)
	})
}

// DBusCallFDs is DBusCall passing file descriptors (the params' handles
// index them); they're the call's (dup them to keep them).
func DBusCallFDs(conn Ptr, dest, path, iface, method string, params Ptr, fds []int32, timeoutMS int32, done func(reply Ptr, err error)) {
	d, p, i, m := CString(dest), CString(path), CString(iface), CString(method)
	var list Ptr
	if len(fds) > 0 {
		list = Sys.UnixFDListNewFromArray(&fds[0], int32(len(fds)))
	}
	Async(func(a AsyncCall) {
		Call(FnConnectionCallWithFDs, conn, Addr(d), Addr(p), Addr(i), Addr(m), params, 0, 0, uintptr(timeoutMS), list, 0, a.Callback, a.Data)
		runtime.KeepAlive(d)
		runtime.KeepAlive(p)
		runtime.KeepAlive(i)
		runtime.KeepAlive(m)
	}, func(_, res Ptr) {
		if list != 0 {
			ObjectUnref(list)
		}
		var gerr Ptr
		r := Sys.ConnectionCallFDFin(conn, nil, res, &gerr)
		if r == 0 {
			done(0, GError(gerr))
			return
		}
		done(r, nil)
		Sys.VariantUnref(r)
	})
}

// DBusSignal is a signal a subscription receives.
type DBusSignal struct {
	Sender, Path, Iface, Name string
	Params                    Ptr // a tuple, valid during the call
}

var cbDBusSignal uintptr

// DBusSubscribe calls f with the signals that match (empty strings match
// any), on the main thread; it returns the subscription's ID.
func DBusSubscribe(conn Ptr, sender, iface, member, path string, f func(DBusSignal)) uint32 {
	if cbDBusSignal == 0 {
		cbDBusSignal = purego.NewCallback(func(conn, sender, path, iface, name, params, data uintptr) uintptr {
			if f, ok := handlers[data].(func(DBusSignal)); ok {
				f(DBusSignal{Sender: GoString(sender), Path: GoString(path), Iface: GoString(iface), Name: GoString(name), Params: params})
			}
			return 0
		})
	}
	opt := func(s string) (*byte, Ptr) {
		if s == "" {
			return nil, 0
		}
		b := CString(s)
		return b, Addr(b)
	}
	sb, sp := opt(sender)
	ib, ip := opt(iface)
	mb, mp := opt(member)
	pb, pp := opt(path)
	id := register(f)
	r := Call(FnSignalSubscribe, conn, sp, ip, mp, pp, 0, 0, cbDBusSignal, id, cbDestroy)
	runtime.KeepAlive(sb)
	runtime.KeepAlive(ib)
	runtime.KeepAlive(mb)
	runtime.KeepAlive(pb)
	return uint32(r)
}

// DBusUnsubscribe ends a subscription.
func DBusUnsubscribe(conn Ptr, id uint32) {
	if id != 0 {
		Call(FnSignalUnsubscribe, conn, uintptr(id))
	}
}

// DBusEmit emits a signal to any listener (params: a floating tuple).
func DBusEmit(conn Ptr, path, iface, name string, params Ptr) error {
	p, i, n := CString(path), CString(iface), CString(name)
	var gerr Ptr
	ok := Call(FnEmit, conn, 0, Addr(p), Addr(i), Addr(n), params, Addr(&gerr))
	runtime.KeepAlive(p)
	runtime.KeepAlive(i)
	runtime.KeepAlive(n)
	if ok == 0 {
		return GError(gerr)
	}
	return nil
}

// GVariant builders: each returns a floating reference, which the
// variant (or call) it's passed to takes.

func VStr(s string) Ptr     { return Sys.VariantNewString(s) }
func VPath(s string) Ptr    { return Sys.VariantNewObjectPath(s) }
func VBytes(s string) Ptr   { return Sys.VariantNewBytestring(s) }
func VBool(b bool) Ptr      { return Sys.VariantNewBoolean(b) }
func VU32(i uint32) Ptr     { return Sys.VariantNewUint32(i) }
func VI32(i int32) Ptr      { return Sys.VariantNewInt32(i) }
func VI64(i int64) Ptr      { return Sys.VariantNewInt64(i) }
func VDouble(d float64) Ptr { return Sys.VariantNewDouble(d) }
func VHandle(i int32) Ptr   { return Sys.VariantNewHandle(i) }
func VVariant(v Ptr) Ptr    { return Sys.VariantNewVariant(v) }
func VEntry(k, v Ptr) Ptr   { return Sys.VariantNewDictEntry(k, v) }
func VTuple(vs ...Ptr) Ptr  { return Sys.VariantNewTuple(modPtrs(vs), uintptr(len(vs))) }
func modPtrs(vs []Ptr) *Ptr {
	if len(vs) == 0 {
		return nil
	}
	return &vs[0]
}

// VArray is an array of elemType ("s", "{sv}"...) of the items.
func VArray(elemType string, items ...Ptr) Ptr {
	t := Sys.VariantTypeNew(elemType)
	v := Sys.VariantNewArray(t, modPtrs(items), uintptr(len(items)))
	Sys.VariantTypeFree(t)
	return v
}

// VStrings is an "as".
func VStrings(ss []string) Ptr {
	items := make([]Ptr, len(ss))
	for i, s := range ss {
		items[i] = VStr(s)
	}
	return VArray("s", items...)
}

// VDict is an "a{sv}" of the values (variants made of them: VStr...), in
// the keys' order.
func VDict(m map[string]Ptr) Ptr {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]Ptr, len(keys))
	for i, k := range keys {
		items[i] = VEntry(VStr(k), VVariant(m[k]))
	}
	return VArray("{sv}", items...)
}

// VariantGo is a GVariant as Go values: bool, uint8, int16, uint16,
// int32, uint32, int64, uint64, float64, string (also object paths and
// signatures), []any (tuples, arrays), map[string]any (dictionaries with
// string keys; other keys: their fmt form), a variant's content.
func VariantGo(v Ptr) any {
	if v == 0 {
		return nil
	}
	switch Sys.VariantClassify(v) {
	case 'b':
		return Sys.VariantGetBoolean(v)
	case 'y':
		return Sys.VariantGetByte(v)
	case 'n':
		return Sys.VariantGetInt16(v)
	case 'q':
		return Sys.VariantGetUint16(v)
	case 'i':
		return Sys.VariantGetInt32(v)
	case 'u':
		return Sys.VariantGetUint32(v)
	case 'x':
		return Sys.VariantGetInt64(v)
	case 't':
		return Sys.VariantGetUint64(v)
	case 'd':
		return Sys.VariantGetDouble(v)
	case 'h':
		return Sys.VariantGetHandle(v)
	case 's', 'o', 'g':
		var n uintptr
		p := Sys.VariantGetString(v, &n)
		if p == 0 {
			return ""
		}
		return string(unsafe.Slice(At[byte](p), n))
	case 'v':
		c := Sys.VariantGetVariant(v)
		defer Sys.VariantUnref(c)
		return VariantGo(c)
	case 'm':
		if Sys.VariantNChildren(v) == 0 {
			return nil
		}
		c := Sys.VariantGetChildValue(v, 0)
		defer Sys.VariantUnref(c)
		return VariantGo(c)
	case 'a':
		n := Sys.VariantNChildren(v)
		ts := GoString(Sys.VariantGetTypeString(v))
		if len(ts) > 2 && ts[1] == '{' {
			m := make(map[string]any, n)
			for i := uintptr(0); i < n; i++ {
				e := Sys.VariantGetChildValue(v, i)
				k, val := Sys.VariantGetChildValue(e, 0), Sys.VariantGetChildValue(e, 1)
				key := VariantGo(k)
				ks, ok := key.(string)
				if !ok {
					ks = modFmt(key)
				}
				m[ks] = VariantGo(val)
				Sys.VariantUnref(k)
				Sys.VariantUnref(val)
				Sys.VariantUnref(e)
			}
			return m
		}
		fallthrough
	case '(', '{':
		n := Sys.VariantNChildren(v)
		out := make([]any, n)
		for i := uintptr(0); i < n; i++ {
			c := Sys.VariantGetChildValue(v, i)
			out[i] = VariantGo(c)
			Sys.VariantUnref(c)
		}
		return out
	}
	return nil
}

func modFmt(v any) string { return fmt.Sprint(v) }
