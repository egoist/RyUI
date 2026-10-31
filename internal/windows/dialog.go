//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"runtime"
	"syscall"
	"unsafe"
)

// TaskButton is a task dialog's button.
type TaskButton struct {
	ID    int
	Title string
}

const (
	IDCANCEL = 2

	TDF_ALLOW_DIALOG_CANCELLATION   = 0x0008
	TDF_USE_COMMAND_LINKS           = 0x0010
	TDF_POSITION_RELATIVE_TO_WINDOW = 0x1000
	TDF_SIZE_TO_CONTENT             = 0x01000000
)

// TaskDialog shows a task dialog (TaskDialogIndirect, common controls 6)
// and returns the ID of the button the user picked, IDCANCEL when they
// closed it. It runs a modal loop: posted work and the owner's messages
// go on meanwhile.
func TaskDialog(owner uintptr, title, instruction, content string, buttons []TaskButton, defaultID int, flags uint32) int {
	p := CommonControl("TaskDialogIndirect")
	if p == 0 {
		return IDCANCEL
	}
	// TASKDIALOGCONFIG and TASKDIALOG_BUTTON are packed (pshpack1), so
	// they're written byte by byte.
	keep := [][]uint16{}
	str := func(s string) uint64 {
		if s == "" {
			return 0
		}
		u := UTF16z(s)
		keep = append(keep, u)
		return uint64(uintptr(unsafe.Pointer(&u[0])))
	}
	btns := make([]byte, 12*max(len(buttons), 1))
	for i, b := range buttons {
		binary.LittleEndian.PutUint32(btns[i*12:], uint32(int32(b.ID)))
		binary.LittleEndian.PutUint64(btns[i*12+4:], str(b.Title))
	}
	cfg := make([]byte, 160)
	le := binary.LittleEndian
	le.PutUint32(cfg[0:], 160)
	le.PutUint64(cfg[4:], uint64(owner))
	le.PutUint64(cfg[12:], uint64(Instance()))
	le.PutUint32(cfg[20:], flags|TDF_POSITION_RELATIVE_TO_WINDOW)
	le.PutUint32(cfg[24:], 0) // common buttons
	le.PutUint64(cfg[28:], str(title))
	le.PutUint64(cfg[44:], str(instruction))
	le.PutUint64(cfg[52:], str(content))
	le.PutUint32(cfg[60:], uint32(len(buttons)))
	if len(buttons) > 0 {
		le.PutUint64(cfg[64:], uint64(uintptr(unsafe.Pointer(&btns[0]))))
	}
	le.PutUint32(cfg[72:], uint32(int32(defaultID)))
	var pressed int32
	syscall.SyscallN(p, uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&pressed)), 0, 0)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(btns)
	return int(pressed)
}
