//go:build windows

package windows

import (
	"encoding/binary"
	"runtime"
	"syscall"
	"unsafe"
)

// TaskButton is a task dialog's button.
type TaskButton struct {
	ID       int
	Title    string
	Disabled bool // dimmed, and not pressed (TDM_ENABLE_BUTTON)
}

const (
	IDCANCEL = 2

	TDF_ALLOW_DIALOG_CANCELLATION   = 0x0008
	TDF_USE_COMMAND_LINKS           = 0x0010
	TDF_POSITION_RELATIVE_TO_WINDOW = 0x1000
	TDF_RTL_LAYOUT                  = 0x2000
	TDF_SIZE_TO_CONTENT             = 0x01000000
)

// TaskDialog shows a task dialog (TaskDialogIndirect, common controls 6)
// and returns the ID of the button the user picked, IDCANCEL when they
// closed it. It runs a modal loop: posted work and the owner's messages
// go on meanwhile. onWindow, if not nil, is called with the dialog's
// window once it's made (EnableTaskButton takes it), and with 0 as it
// goes. Its disabled buttons are disabled as it's made.
func TaskDialog(owner uintptr, title, instruction, content string, buttons []TaskButton, defaultID int, flags uint32, onWindow func(hwnd uintptr)) int {
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
	// The callback, for the buttons to disable and the window.
	if taskCallback == 0 {
		taskCallback = syscall.NewCallback(taskDialogEvent)
	}
	taskNext++
	ref := taskNext
	taskDialogs[ref] = &taskState{buttons: buttons, onWindow: onWindow}
	defer delete(taskDialogs, ref)
	le.PutUint64(cfg[140:], uint64(taskCallback))
	le.PutUint64(cfg[148:], uint64(ref))
	var pressed int32
	syscall.SyscallN(p, uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&pressed)), 0, 0)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(btns)
	return int(pressed)
}

// The task dialogs showing (nested modal loops can show more than one),
// by their callback's reference data.
var (
	taskCallback uintptr
	taskNext     uintptr
	taskDialogs  = map[uintptr]*taskState{}
)

type taskState struct {
	buttons  []TaskButton
	onWindow func(hwnd uintptr)
}

const (
	tdnCreated      = 0
	tdnDestroyed    = 5
	tdmEnableButton = WM_USER + 111
)

// taskDialogEvent is a task dialog's callback (PFTASKDIALOGCALLBACK).
func taskDialogEvent(hwnd, msg, wp, lp, ref uintptr) uintptr {
	st := taskDialogs[ref]
	if st == nil {
		return 0
	}
	switch msg {
	case tdnCreated:
		for _, b := range st.buttons {
			if b.Disabled {
				EnableTaskButton(hwnd, b.ID, false)
			}
		}
		if st.onWindow != nil {
			st.onWindow(hwnd)
		}
	case tdnDestroyed:
		if st.onWindow != nil {
			st.onWindow(0)
		}
	}
	return 0 // S_OK
}

// EnableTaskButton enables or disables a task dialog's button, by its ID.
func EnableTaskButton(dialog uintptr, id int, on bool) {
	if dialog != 0 {
		SendMessage(dialog, tdmEnableButton, uintptr(id), b2u(on))
	}
}
