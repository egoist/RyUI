//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego"

	. "github.com/egoist/nui/internal/darwin"
)

var msgShowPicker func(obj ID, sel SEL, r Rect, view ID, edge uint)

var postA11y func(element, name, info ID)

// announcer is NSAccessibilityPostNotificationWithUserInfo.
func announcer() func(element, name, info ID) {
	if postA11y == nil {
		purego.RegisterLibFunc(&postA11y, AppKit(), "NSAccessibilityPostNotificationWithUserInfo")
	}
	return postA11y
}

// module is a native module's methods (ui.Call), by name: the ones the
// macOS host has.
func (h *Host) module(name string) func(method string, args []byte) ([]byte, error) {
	if h.modules == nil {
		h.modules = builtinModules()
	}
	return h.modules[name]
}

func builtinModules() map[string]func(method string, args []byte) ([]byte, error) {
	return map[string]func(method string, args []byte) ([]byte, error){}
}
