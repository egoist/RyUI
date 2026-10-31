//go:build ios && cgo

// Package shareddir is where an iOS app with a share sheet
// (app.ShareSheet) keeps its data: in the App Group its share extension
// shares, which only the host can find. store asks for it as packages
// initialize, before the host has started the Go code, so it's a call
// into the host, not a value the host sets.
package shareddir

/*
#include <stdlib.h>
extern char *ryui_ios_shared_dir(void);
*/
import "C"

import "unsafe"

// Dir is the App Group's Library/Application Support, "" if the app has
// no share sheet.
func Dir() string {
	p := C.ryui_ios_shared_dir()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}
