//go:build windows && amd64

package windows

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"
)

// The Credential Manager (a user's generic credentials), the open file
// dialog and the system's execution state: what the host's native
// modules (ui.Call) need.

var (
	procCredWriteW              = advapi32.NewProc("CredWriteW")
	procCredReadW               = advapi32.NewProc("CredReadW")
	procCredDeleteW             = advapi32.NewProc("CredDeleteW")
	procCredFree                = advapi32.NewProc("CredFree")
	procGetOpenFileNameW        = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError    = comdlg32.NewProc("CommDlgExtendedError")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        [2]uint32 // FILETIME
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

const (
	credTypeGeneric       = 1
	credPersistLocal      = 2 // this machine, the user's logons
	errorNotFound         = 1168
	CredentialMaxBlobSize = 5 * 512
)

// ErrNoCredential is a credential that isn't there.
var ErrNoCredential = errors.New("not found")

// WriteCredential keeps value as the user's generic credential target.
func WriteCredential(target, user string, value []byte) error {
	if len(value) > CredentialMaxBlobSize {
		return errors.New("the value is longer than the Credential Manager keeps (2560 bytes)")
	}
	c := credential{Type: credTypeGeneric, TargetName: UTF16(target), UserName: UTF16(user),
		CredentialBlobSize: uint32(len(value)), Persist: credPersistLocal}
	if len(value) > 0 {
		c.CredentialBlob = &value[0]
	}
	r, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r == 0 {
		return err
	}
	return nil
}

// ReadCredential reads the user's generic credential target.
func ReadCredential(target string) ([]byte, error) {
	var p *credential
	r, _, err := procCredReadW.Call(Str(target), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == errorNotFound {
			return nil, ErrNoCredential
		}
		return nil, err
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(p)))
	if p.CredentialBlobSize == 0 {
		return []byte{}, nil
	}
	return append([]byte(nil), unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize)...), nil
}

// DeleteCredential deletes the user's generic credential target (none
// there is no error).
func DeleteCredential(target string) error {
	r, _, err := procCredDeleteW.Call(Str(target), credTypeGeneric, 0)
	if r == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == errorNotFound {
			return nil
		}
		return err
	}
	return nil
}

// FileFilter is one line of the open dialog's file types: a name, and
// patterns ("*.png;*.jpg").
type FileFilter struct{ Name, Patterns string }

// OpenFiles shows the open file dialog (modal to owner) and returns the
// files chosen; none: cancelled.
func OpenFiles(owner uintptr, title string, filters []FileFilter, multiple bool) ([]string, error) {
	type openFileName struct {
		Size          uint32
		Owner         uintptr
		Instance      uintptr
		Filter        *uint16
		CustomFilter  *uint16
		MaxCustFilter uint32
		FilterIndex   uint32
		File          *uint16
		MaxFile       uint32
		FileTitle     *uint16
		MaxFileTitle  uint32
		InitialDir    *uint16
		Title         *uint16
		Flags         uint32
		FileOffset    uint16
		FileExtension uint16
		DefExt        *uint16
		CustData      uintptr
		Hook          uintptr
		TemplateName  *uint16
		Reserved      uintptr
		Reserved2     uint32
		FlagsEx       uint32
	}
	var f []uint16
	for _, x := range filters {
		f = append(f, syscall.StringToUTF16(x.Name)...)
		f = append(f, syscall.StringToUTF16(x.Patterns)...)
	}
	f = append(f, 0)
	buf := make([]uint16, 64*1024)
	const (
		ofnAllowMultiSelect = 0x200
		ofnExplorer         = 0x80000
		ofnFileMustExist    = 0x1000
		ofnPathMustExist    = 0x800
		ofnNoChangeDir      = 0x8
	)
	o := openFileName{Owner: owner, File: &buf[0], MaxFile: uint32(len(buf)), FilterIndex: 1,
		Flags: ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir}
	o.Size = uint32(unsafe.Sizeof(o))
	if len(filters) > 0 {
		o.Filter = &f[0]
	}
	if title != "" {
		o.Title = UTF16(title)
	}
	if multiple {
		o.Flags |= ofnAllowMultiSelect
	}
	if r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&o))); r == 0 {
		if e, _, _ := procCommDlgExtendedError.Call(); e != 0 {
			return nil, errors.New("the open dialog failed")
		}
		return nil, nil // cancelled
	}
	// One file: its path. Several: the folder, then each name, NUL
	// separated, ending in two NULs.
	var parts []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			parts = append(parts, syscall.UTF16ToString(buf[start:i]))
			start = i + 1
		}
	}
	if len(parts) <= 1 {
		return parts, nil
	}
	dir := strings.TrimRight(parts[0], `\`)
	out := make([]string, 0, len(parts)-1)
	for _, name := range parts[1:] {
		out = append(out, dir+`\`+name)
	}
	return out, nil
}

// KeepAwake keeps the display (and the system) on while on.
func KeepAwake(on bool) {
	const esContinuous, esSystemRequired, esDisplayRequired = 0x80000000, 0x1, 0x2
	flags := uintptr(esContinuous)
	if on {
		flags |= esSystemRequired | esDisplayRequired
	}
	procSetThreadExecutionState.Call(flags)
}

// ClipboardText is the clipboard's text (ok false: none).
func ClipboardText() (string, bool) {
	data := OleGetClipboard()
	if data == 0 {
		return "", false
	}
	defer Release(data)
	return GetText(data, CF_UNICODETEXT, false)
}
