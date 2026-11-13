//go:build darwin && !ios

package darwin

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Carbon's global hot keys (HIToolbox): the way a Mac app gets a key
// pressed while another app is in front, with no Accessibility
// permission. Hot key events come through the application's event
// target, which AppKit's event loop dispatches, on the main thread.

// Carbon's modifier flags (Events.h).
const (
	CarbonCmd     = 1 << 8
	CarbonShift   = 1 << 9
	CarbonOption  = 1 << 11
	CarbonControl = 1 << 12
)

// Hot key registration failures (CarbonEvents.h).
const (
	EventHotKeyExistsErr  = -9878 // another app has it, exclusively
	EventHotKeyInvalidErr = -9879
)

const (
	hotKeySignature         = 0x41737561 // 'Asua'
	kEventClassKeyboard     = 0x6B657962 // 'keyb'
	kEventHotKeyPressed     = 5
	kEventParamDirectObject = 0x2D2D2D2D // '----'
	typeEventHotKeyID       = 0x686B6964 // 'hkid'
	kEventHotKeyExclusive   = 1 << 0
)

var carbon struct {
	once                                          sync.Once
	lib                                           uintptr
	ok                                            bool
	appTarget, register, unregister, install      uintptr
	getParam, setParam, sendToTarget, releaseEvnt uintptr
	copySymbolic                                  uintptr
	copyLayout, inputSourceProperty, kbdType      uintptr
	keyTranslate, dataBytes                       uintptr
	layoutDataKey                                 ID
	createEvent                                   func(alloc uintptr, class, kind uint32, when float64, flags uint32, out *uintptr) int32

	handler   uintptr // the event handler, installed once
	installed bool
	pressed   func(id uint32)
}

// LoadCarbon opens Carbon's hot key functions, once; false if they're
// missing.
func LoadCarbon() bool {
	carbon.once.Do(func() {
		Load()
		c := &carbon
		c.lib = Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon")
		if c.lib == 0 {
			return
		}
		sym := func(name string) uintptr {
			p, err := purego.Dlsym(c.lib, name)
			if err != nil {
				return 0
			}
			return p
		}
		c.appTarget = sym("GetApplicationEventTarget")
		c.register = sym("RegisterEventHotKey")
		c.unregister = sym("UnregisterEventHotKey")
		c.install = sym("InstallEventHandler")
		c.getParam = sym("GetEventParameter")
		c.setParam = sym("SetEventParameter")
		c.sendToTarget = sym("SendEventToEventTarget")
		c.releaseEvnt = sym("ReleaseEvent")
		c.copySymbolic = sym("CopySymbolicHotKeys")
		c.copyLayout = sym("TISCopyCurrentASCIICapableKeyboardLayoutInputSource")
		c.inputSourceProperty = sym("TISGetInputSourceProperty")
		c.kbdType = sym("LMGetKbdType")
		c.keyTranslate = sym("UCKeyTranslate")
		c.dataBytes, _ = purego.Dlsym(libCF, "CFDataGetBytePtr")
		c.layoutDataKey = Const(c.lib, "kTISPropertyUnicodeKeyLayoutData")
		if p := sym("CreateEvent"); p != 0 {
			purego.RegisterFunc(&c.createEvent, p)
		}
		c.ok = c.appTarget != 0 && c.register != 0 && c.unregister != 0 && c.install != 0 && c.getParam != 0
	})
	return carbon.ok
}

// OnHotKey sets the function hot key presses go to, with the id they
// were registered with, and installs the handler that calls it. Main
// thread.
func OnHotKey(f func(id uint32)) error {
	if !LoadCarbon() {
		return errors.New("Carbon's hot keys aren't there")
	}
	carbon.pressed = f
	if carbon.installed {
		return nil
	}
	carbon.handler = purego.NewCallback(func(next, event, data uintptr) uintptr {
		var hk [2]uint32 // EventHotKeyID: signature, id
		st := int32(cc(carbon.getParam, event, kEventParamDirectObject, typeEventHotKeyID, 0, unsafe.Sizeof(hk), 0, uintptr(unsafe.Pointer(&hk[0]))))
		if st != 0 || hk[0] != hotKeySignature {
			return notHandled
		}
		if carbon.pressed != nil {
			carbon.pressed(hk[1])
		}
		return 0
	})
	spec := [2]uint32{kEventClassKeyboard, kEventHotKeyPressed} // EventTypeSpec
	var ref uintptr
	st := int32(cc(carbon.install, appTarget(), carbon.handler, 1, uintptr(unsafe.Pointer(&spec[0])), 0, uintptr(unsafe.Pointer(&ref))))
	if st != 0 {
		return errors.New("InstallEventHandler failed")
	}
	carbon.installed = true
	return nil
}

var notHandled = uintptr(uint32(0xFFFFD96E)) // eventNotHandledErr (-9874): to the next handler

func appTarget() uintptr { return cc(carbon.appTarget) }

// RegisterHotKey registers a key code with Carbon modifiers as hot key
// id, exclusively (another app's exclusive registration of it fails it
// with EventHotKeyExistsErr, and this one keeps others from it). The
// status is Carbon's (0: registered).
func RegisterHotKey(code, mods, id uint32) (ref uintptr, status int32) {
	if !LoadCarbon() {
		return 0, EventHotKeyInvalidErr
	}
	// EventHotKeyID {signature, id} by value: eight bytes, one register.
	hkid := uintptr(hotKeySignature) | uintptr(id)<<32
	st := cc(carbon.register, uintptr(code), uintptr(mods), hkid, appTarget(), kEventHotKeyExclusive, uintptr(unsafe.Pointer(&ref)))
	return ref, int32(st)
}

// UnregisterHotKey lets a hot key go.
func UnregisterHotKey(ref uintptr) {
	if ref != 0 && LoadCarbon() {
		cc(carbon.unregister, ref)
	}
}

// SystemHotKey reports whether the system's own shortcuts (System
// Settings › Keyboard › Keyboard Shortcuts: Spotlight's, Mission
// Control's, the input sources') have a key code with Carbon modifiers,
// enabled.
func SystemHotKey(code, mods uint32) bool {
	if !LoadCarbon() || carbon.copySymbolic == 0 {
		return false
	}
	var arr uintptr
	if cc(carbon.copySymbolic, uintptr(unsafe.Pointer(&arr))) != 0 || arr == 0 {
		return false
	}
	defer CFRelease(arr)
	// The keys' constants aren't exported: their values.
	kCode, kMods, kOn := String("kHISymbolicHotKeyCode"), String("kHISymbolicHotKeyModifiers"), String("kHISymbolicHotKeyEnabled")
	const relevant = CarbonCmd | CarbonShift | CarbonOption | CarbonControl
	for _, d := range ArrayItems(ID(arr)) {
		c, m, on := Send(d, "objectForKey:", uintptr(kCode)), Send(d, "objectForKey:", uintptr(kMods)), Send(d, "objectForKey:", uintptr(kOn))
		if c == 0 || m == 0 || on == 0 || !SendBool(on, "boolValue") {
			continue
		}
		if uint32(SendInt(c, "unsignedIntValue")) == code && uint32(SendInt(m, "unsignedIntValue"))&relevant == mods&relevant {
			return true
		}
	}
	return false
}

// KeyCodeFor is the key code of the key that types ch on the current
// keyboard layout, or its ASCII-capable one (a Russian layout's Latin
// one), as menus' key equivalents are read: the key labelled K on an
// AZERTY keyboard for "k". shift: the key types ch only with Shift ("+"
// on a US keyboard). The main keys come before the keypad's.
func KeyCodeFor(ch rune) (code uint16, shift, ok bool) {
	if !LoadCarbon() || carbon.copyLayout == 0 || carbon.keyTranslate == 0 || carbon.layoutDataKey == 0 {
		return 0, false, false
	}
	src := cc(carbon.copyLayout)
	if src == 0 {
		return 0, false, false
	}
	defer CFRelease(src)
	data := cc(carbon.inputSourceProperty, src, uintptr(carbon.layoutDataKey))
	if data == 0 {
		return 0, false, false
	}
	layout := cc(carbon.dataBytes, data)
	kbd := cc(carbon.kbdType) & 0xff
	const keyActionDisplay, noDeadKeys = 3, 1
	keypad := func(c uintptr) bool {
		return c >= 0x41 && c <= 0x5C && c != 0x48 && c != 0x49 && c != 0x4A && c != 0x50 && c != 0x5A
	}
	type pass struct {
		state  uintptr // UCKeyTranslate's modifiers: Carbon's, shifted down 8
		keypad bool
	}
	for _, p := range []pass{{0, false}, {CarbonShift >> 8, false}, {0, true}} {
		for c := uintptr(0); c < 128; c++ {
			if keypad(c) != p.keypad {
				continue
			}
			var dead uint32
			var n uintptr
			var buf [4]uint16
			st := cc(carbon.keyTranslate, layout, c, keyActionDisplay, p.state, kbd, noDeadKeys,
				uintptr(unsafe.Pointer(&dead)), 4, uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
			if int32(st) == 0 && n == 1 && rune(buf[0]) == ch {
				return uint16(c), p.state != 0, true
			}
		}
	}
	return 0, false, false
}

// SendHotKeyEvent sends a hot key event for id to the application's
// event target, as the system does when its keys are pressed, for
// tests: it reaches OnHotKey's function through Carbon's dispatch.
func SendHotKeyEvent(id uint32) error {
	if !LoadCarbon() || carbon.createEvent == nil || carbon.setParam == 0 || carbon.sendToTarget == 0 {
		return errors.New("Carbon's events aren't there")
	}
	var ev uintptr
	if st := carbon.createEvent(0, kEventClassKeyboard, kEventHotKeyPressed, 0, 0, &ev); st != 0 {
		return errors.New("CreateEvent failed")
	}
	defer cc(carbon.releaseEvnt, ev)
	hk := [2]uint32{hotKeySignature, id}
	cc(carbon.setParam, ev, kEventParamDirectObject, typeEventHotKeyID, unsafe.Sizeof(hk), uintptr(unsafe.Pointer(&hk[0])))
	if st := int32(cc(carbon.sendToTarget, ev, appTarget())); st != 0 {
		return errors.New("SendEventToEventTarget failed")
	}
	return nil
}

// cc calls a C function with integer and pointer arguments.
func cc(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(fn, args...)
	return r
}
