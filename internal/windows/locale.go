//go:build windows

package windows

import (
	"strconv"
	"syscall"
	"unsafe"
)

// The user's languages and regional formats, as MyGo reads them: the
// display languages of Settings > Time & language, and the regional
// formats, which may be another language's (English Windows with the
// formats of China).

const (
	muiLanguageName = 0x8 // MUI_LANGUAGE_NAME

	localeITime           = 0x0023
	localeSTimeFormat     = 0x1003
	localeIFirstDayOfWeek = 0x100C
)

var (
	procGetUserPreferredUILanguages   = kernel32.NewProc("GetUserPreferredUILanguages")
	procSetThreadPreferredUILanguages = kernel32.NewProc("SetThreadPreferredUILanguages")
)

// PreferredLanguages is the user's display languages, most preferred first
// (BCP 47 tags); nil if Windows lists none.
func PreferredLanguages() []string {
	var count, size uint32
	if ok, _, _ := procGetUserPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&size))); ok == 0 || size == 0 {
		return nil
	}
	buf := make([]uint16, size)
	if ok, _, _ := procGetUserPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); ok == 0 {
		return nil
	}
	return splitMultiString(buf)
}

// splitMultiString reads a list of strings, each ending in a NUL and the
// list in another.
func splitMultiString(buf []uint16) []string {
	var out []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i == start {
				break
			}
			out = append(out, syscall.UTF16ToString(buf[start:i]))
			start = i + 1
		}
	}
	return out
}

// SetThreadLanguage makes tag the language this thread's system words are
// in (the color dialog's, a file dialog's), where Windows has it, and the
// user's languages after it. It reports whether Windows took it: a tag it
// doesn't know (a pseudo locale) changes nothing.
func SetThreadLanguage(tag string) bool {
	if tag == "" {
		return false
	}
	list := append(UTF16z(tag), 0)
	var n uint32
	ok, _, _ := procSetThreadPreferredUILanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&list[0])), uintptr(unsafe.Pointer(&n)))
	return ok != 0
}

// FirstWeekday is the day the user's weeks start on, 1 Sunday to 7
// Saturday; 0 when Windows doesn't say. Windows' is 0 Monday to 6 Sunday.
func FirstWeekday() uint8 { return firstWeekday(LocaleInfo("", localeIFirstDayOfWeek)) }

func firstWeekday(s string) uint8 {
	d, err := strconv.Atoi(s)
	if err != nil || d < 0 || d > 6 {
		return 0
	}
	return uint8((d+1)%7) + 1
}

// HourCycle is the user's hour cycle, 12 or 24, from their time format
// (with the changes they made to it); 0 when Windows doesn't say.
func HourCycle() uint8 {
	if c := timeFormatCycle(LocaleInfo("", localeSTimeFormat)); c != 0 {
		return c
	}
	switch LocaleInfo("", localeITime) {
	case "0":
		return 12
	case "1":
		return 24
	}
	return 0
}

// timeFormatCycle is the hour cycle of a Windows time format: 12 for h, 24
// for H, 0 for none. Quoted text isn't a field.
func timeFormatCycle(format string) uint8 {
	quoted := false
	for _, c := range format {
		switch {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == 'h':
			return 12
		case c == 'H':
			return 24
		}
	}
	return 0
}
