// Package mobile adapts nui's core to the C ABI that the iOS and Android
// hosts call (see hosts/ios/Sources/CNui/include/nui.h). It builds only
// for ios and android, with cgo; elsewhere it is empty.
//
// The CLI links the app's main package with this package and sets Main
// to the app's main function, which the host calls once at launch.
package mobile

// Main is the app's main function; nui_start calls it.
var Main func()
