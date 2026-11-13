//go:build windows

package windows

import (
	"errors"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"
)

// The shell's verbs on files (open, print), the app for a URL scheme,
// and Simple MAPI's mail: asuka/sdk/preview, asuka/sdk/print and asuka/sdk/compose.

var (
	shlwapi = systemDLL("shlwapi.dll")
	mapi32  = systemDLL("mapi32.dll")

	procShellExecuteExW   = shell32.NewProc("ShellExecuteExW")
	procAssocQueryStringW = shlwapi.NewProc("AssocQueryStringW")
	procMAPISendMailW     = mapi32.NewProc("MAPISendMailW")
)

// shellExecuteInfo is SHELLEXECUTEINFOW (x64).
type shellExecuteInfo struct {
	size       uint32
	mask       uint32
	hwnd       uintptr
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instApp    uintptr
	idList     uintptr
	class      *uint16
	hkeyClass  uintptr
	hotKey     uint32
	iconOrMon  uintptr
	process    uintptr
}

// ErrNoAssociation: no app is set for the file's type (or for the verb).
var ErrNoAssociation = errors.New("no app is set for this file")

// ShellExecute runs verb ("" for the default, "open", "print") on file,
// as Explorer does, with owner as the window of any UI it shows (the
// "How do you want to open this?" dialog for a type with no app).
func ShellExecute(owner uintptr, verb, file string) error {
	f := UTF16z(file)
	// SEE_MASK_NOASYNC: done before it returns. Without SEE_MASK_FLAG_NO_UI,
	// Windows asks which app to use for a type with none, as Explorer does.
	sei := shellExecuteInfo{mask: 0x00000100, hwnd: owner, file: &f[0], show: SW_SHOWNORMAL}
	var v []uint16
	if verb != "" {
		v = UTF16z(verb)
		sei.verb = &v[0]
	}
	sei.size = uint32(unsafe.Sizeof(sei))
	r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	runtime.KeepAlive(f)
	runtime.KeepAlive(v)
	if r != 0 {
		return nil
	}
	switch sei.instApp {
	case 31, 1155: // SE_ERR_NOASSOC, ERROR_NO_ASSOCIATION
		return ErrNoAssociation
	}
	if errno, ok := err.(syscall.Errno); ok && (errno == 1155 || errno == 31) {
		return ErrNoAssociation
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 1223 { // ERROR_CANCELLED: the user closed the dialog
		return nil
	}
	return err
}

// SchemeHasApp reports whether an app is set for URLs of scheme
// ("mailto").
func SchemeHasApp(scheme string) bool {
	if !Has(procAssocQueryStringW) {
		return false
	}
	var buf [syscall.MAX_PATH]uint16
	n := uint32(len(buf))
	// ASSOCF_IS_PROTOCOL, ASSOCSTR_EXECUTABLE.
	r, _, _ := procAssocQueryStringW.Call(0x1000, 2, Str(scheme), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	return r == 0 && n > 1
}

// MAPI's structures (x64).
type mapiRecip struct {
	reserved uint32
	class    uint32
	name     *uint16
	address  *uint16
	eidSize  uint32
	entryID  uintptr
}

type mapiFile struct {
	reserved uint32
	flags    uint32
	position uint32
	path     *uint16
	name     *uint16
	fileType uintptr
}

type mapiMessage struct {
	reserved       uint32
	subject        *uint16
	noteText       *uint16
	messageType    *uint16
	dateReceived   *uint16
	conversationID *uint16
	flags          uint32
	originator     *mapiRecip
	recipCount     uint32
	recips         *mapiRecip
	fileCount      uint32
	files          *mapiFile
}

// MailFile is a file attached to a mail: its path and the name the
// recipient sees.
type MailFile struct{ Path, Name string }

// The outcomes of MAPISendMail.
var (
	ErrMAPICancelled   = errors.New("cancelled")
	ErrMAPIUnavailable = errors.New("no Simple MAPI mail app")
)

// SendMail shows the user's Simple MAPI mail app's composer (Outlook,
// Thunderbird) with the mail. It waits for the composer: call it on a
// goroutine, never on the main thread; it runs on a thread of its own.
func SendMail(to, cc, bcc []string, subject, text string, files []MailFile) error {
	if !Has(procMAPISendMailW) {
		return ErrMAPIUnavailable
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED: the composer's own UI
	var keep [][]uint16
	str := func(s string) *uint16 {
		u := UTF16z(s)
		keep = append(keep, u)
		return &u[0]
	}
	var recips []mapiRecip
	add := func(class uint32, as []string) {
		for _, a := range as {
			recips = append(recips, mapiRecip{class: class, name: str(a), address: str("SMTP:" + a)})
		}
	}
	add(1, to) // MAPI_TO
	add(2, cc)
	add(3, bcc)
	var fs []mapiFile
	for _, f := range files {
		fs = append(fs, mapiFile{position: ^uint32(0), path: str(f.Path), name: str(f.Name)})
	}
	m := mapiMessage{subject: str(subject), noteText: str(text), recipCount: uint32(len(recips)), fileCount: uint32(len(fs))}
	if len(recips) > 0 {
		m.recips = &recips[0]
	}
	if len(fs) > 0 {
		m.files = &fs[0]
	}
	// MAPI_LOGON_UI | MAPI_DIALOG: the composer, signed in if need be.
	r, _, _ := procMAPISendMailW.Call(0, 0, uintptr(unsafe.Pointer(&m)), 0x1|0x8, 0)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(recips)
	runtime.KeepAlive(fs)
	switch r {
	case 0: // SUCCESS_SUCCESS
		return nil
	case 1: // MAPI_USER_ABORT
		return ErrMAPICancelled
	case 2, 3, 26: // MAPI_E_FAILURE, MAPI_E_LOGIN_FAILURE, MAPI_E_NOT_SUPPORTED: no client takes it
		return ErrMAPIUnavailable
	}
	return errors.New("MAPI error " + strconv.Itoa(int(r)))
}
