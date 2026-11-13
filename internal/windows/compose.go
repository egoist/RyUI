//go:build windows

package windows

// The desktop window manager's composition.

var procDwmFlush = dwmapi.NewProc("DwmFlush")

// DwmFlush waits until what was drawn has been composed to the screen.
func DwmFlush() { procDwmFlush.Call() }
