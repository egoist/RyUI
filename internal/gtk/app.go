//go:build linux && !android

package gtk

import (
	"runtime"
	"time"
)

// The application's command line (a launch's, or another's handed to
// the one that runs), its name, and the user's recently used files
// (GBookmarkFile: recently-used.xbel, what GtkRecentManager keeps).

// CommandLineArgs is a GApplicationCommandLine's arguments (argv, the
// program first) and working directory.
func CommandLineArgs(cl Ptr) (args []string, cwd string) {
	var argc int32
	argv := Call(Sym("g_application_command_line_get_arguments"), cl, Addr(&argc))
	for i := 0; i < int(argc); i++ {
		args = append(args, GoString(*At[Ptr](argv + uintptr(i)*8)))
	}
	if argv != 0 {
		Call(Sym("g_strfreev"), argv)
	}
	cwd = GoString(Call(Sym("g_application_command_line_get_cwd"), cl))
	return args, cwd
}

// SetApplicationName sets the app's human-readable name
// (g_set_application_name), which GTK records the files the user opens
// under. Once.
func SetApplicationName(name string) {
	s := CString(name)
	Call(Sym("g_set_application_name"), Addr(s))
	runtime.KeepAlive(s)
}

// ContentTypeMIME is a file's MIME type guessed from its name
// ("application/octet-stream" when unknown).
func ContentTypeMIME(name string) string {
	s := CString(name)
	ct := Call(Sym("g_content_type_guess"), Addr(s), 0, 0, 0)
	runtime.KeepAlive(s)
	if ct == 0 {
		return "application/octet-stream"
	}
	m := GoStringFree(Call(Sym("g_content_type_get_mime_type"), ct))
	Free(ct)
	if m == "" {
		return "application/octet-stream"
	}
	return m
}

// Bookmarks is a GBookmarkFile: an XBEL file of URIs, each with the
// applications that registered it.
type Bookmarks struct{ p Ptr }

// LoadBookmarks reads the XBEL file at path; a missing one is empty.
func LoadBookmarks(path string) (*Bookmarks, error) {
	b := &Bookmarks{p: Call(Sym("g_bookmark_file_new"))}
	s := CString(path)
	var gerr Ptr
	ok := Call(Sym("g_bookmark_file_load_from_file"), b.p, Addr(s), Addr(&gerr)) != 0
	runtime.KeepAlive(s)
	if !ok {
		domain, code := GErrorCode(gerr)
		fileErr := uint32(Call(Sym("g_file_error_quark")))
		const noEnt = 4 // G_FILE_ERROR_NOENT
		if domain == fileErr && code == noEnt {
			ErrorFree(gerr)
			return b, nil
		}
		b.Free()
		return nil, GError(gerr)
	}
	return b, nil
}

// Free frees the bookmarks.
func (b *Bookmarks) Free() {
	if b.p != 0 {
		Call(Sym("g_bookmark_file_free"), b.p)
		b.p = 0
	}
}

// Save writes the bookmarks to path (atomically).
func (b *Bookmarks) Save(path string) error {
	s := CString(path)
	var gerr Ptr
	ok := Call(Sym("g_bookmark_file_to_file"), b.p, Addr(s), Addr(&gerr)) != 0
	runtime.KeepAlive(s)
	if !ok {
		return GError(gerr)
	}
	return nil
}

// URIs are the bookmarks' URIs.
func (b *Bookmarks) URIs() []string {
	var n uintptr
	v := Call(Sym("g_bookmark_file_get_uris"), b.p, Addr(&n))
	out := make([]string, 0, n)
	for i := uintptr(0); i < n; i++ {
		out = append(out, GoString(*At[Ptr](v + i*8)))
	}
	if v != 0 {
		Call(Sym("g_strfreev"), v)
	}
	return out
}

// Add registers uri as opened by the application name (exec: its
// command, %u for the URI) now, with its MIME type.
func (b *Bookmarks) Add(uri, mime, name, exec string) {
	u, m, n, e := CString(uri), CString(mime), CString(name), CString(exec)
	Call(Sym("g_bookmark_file_set_mime_type"), b.p, Addr(u), Addr(m))
	Call(Sym("g_bookmark_file_add_application"), b.p, Addr(u), Addr(n), Addr(e))
	runtime.KeepAlive(u)
	runtime.KeepAlive(m)
	runtime.KeepAlive(n)
	runtime.KeepAlive(e)
}

// Applications are the names of the applications that registered uri.
func (b *Bookmarks) Applications(uri string) []string {
	u := CString(uri)
	var n uintptr
	var gerr Ptr
	v := Call(Sym("g_bookmark_file_get_applications"), b.p, Addr(u), Addr(&n), Addr(&gerr))
	runtime.KeepAlive(u)
	if gerr != 0 {
		ErrorFree(gerr)
	}
	out := make([]string, 0, n)
	for i := uintptr(0); i < n; i++ {
		out = append(out, GoString(*At[Ptr](v + i*8)))
	}
	if v != 0 {
		Call(Sym("g_strfreev"), v)
	}
	return out
}

// Stamp is when the application name last registered uri, and whether
// it did.
func (b *Bookmarks) Stamp(uri, name string) (time.Time, bool) {
	u, n := CString(uri), CString(name)
	var stamp, gerr Ptr
	ok := Call(Sym("g_bookmark_file_get_application_info"), b.p, Addr(u), Addr(n), 0, 0, Addr(&stamp), Addr(&gerr)) != 0
	runtime.KeepAlive(u)
	runtime.KeepAlive(n)
	if !ok {
		if gerr != 0 {
			ErrorFree(gerr)
		}
		return time.Time{}, false
	}
	if stamp == 0 {
		return time.Time{}, true
	}
	sec := int64(Call(Sym("g_date_time_to_unix"), stamp))
	usec := int64(int32(Call(Sym("g_date_time_get_microsecond"), stamp)))
	return time.Unix(sec, usec*1000), true
}

// RemoveApplication takes the application name off uri (the bookmark
// stays for the others).
func (b *Bookmarks) RemoveApplication(uri, name string) {
	u, n := CString(uri), CString(name)
	var gerr Ptr
	Call(Sym("g_bookmark_file_remove_application"), b.p, Addr(u), Addr(n), Addr(&gerr))
	runtime.KeepAlive(u)
	runtime.KeepAlive(n)
	if gerr != 0 {
		ErrorFree(gerr)
	}
}

// Remove removes uri's bookmark.
func (b *Bookmarks) Remove(uri string) {
	u := CString(uri)
	var gerr Ptr
	Call(Sym("g_bookmark_file_remove_item"), b.p, Addr(u), Addr(&gerr))
	runtime.KeepAlive(u)
	if gerr != 0 {
		ErrorFree(gerr)
	}
}
