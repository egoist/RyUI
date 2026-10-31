//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

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

// A module's method answers once, now or later (a panel the user
// closes), on the main thread.
type method func(args []byte, reply func([]byte, error))

// module is a native module's methods (ui.Call), by name: the ones the
// macOS host has.
func (h *Host) module(name string) map[string]method {
	if h.modules == nil {
		h.modules = h.builtinModules()
	}
	return h.modules[name]
}

func (h *Host) builtinModules() map[string]map[string]method {
	return map[string]map[string]method{
		"secureStore": {
			// args: the key; result: u8 found, then the value.
			"get": func(args []byte, reply func([]byte, error)) {
				v, err := keychainGet(string(args))
				if errors.Is(err, errNotFound) {
					reply([]byte{0}, nil)
					return
				}
				reply(append([]byte{1}, v...), err)
			},
			// args: u32 key length, the key, the value.
			"set": func(args []byte, reply func([]byte, error)) {
				if len(args) < 4 || int(binary.LittleEndian.Uint32(args)) > len(args)-4 {
					reply(nil, errors.New("secureStore.set: bad arguments"))
					return
				}
				n := int(binary.LittleEndian.Uint32(args))
				reply(nil, keychainSet(string(args[4:4+n]), args[4+n:]))
			},
			"delete": func(args []byte, reply func([]byte, error)) {
				reply(nil, keychainDelete(string(args)))
			},
		},
		"device": {
			// result: JSON: model, system, systemVersion, appVersion, build.
			"info": func(args []byte, reply func([]byte, error)) {
				pi := Send(Class("NSProcessInfo"), "processInfo")
				version := strings.TrimPrefix(GoString(Send(pi, "operatingSystemVersionString")), "Version ")
				if i := strings.Index(version, " ("); i > 0 {
					version = version[:i]
				}
				info := Send(Send(Class("NSBundle"), "mainBundle"), "infoDictionary")
				get := func(k string) string {
					if info == 0 {
						return ""
					}
					return GoString(Send(info, "objectForKey:", uintptr(String(k))))
				}
				b, err := json.Marshal(map[string]string{
					"model": "Mac", "system": "macOS", "systemVersion": version,
					"appVersion": get("CFBundleShortVersionString"), "build": get("CFBundleVersion"),
				})
				reply(b, err)
			},
			// result: u8 has, then the clipboard's text.
			"clipboard": func(args []byte, reply func([]byte, error)) {
				pb := Send(Class("NSPasteboard"), "generalPasteboard")
				s := Send(pb, "stringForType:", uintptr(Const(AppKit(), "NSPasteboardTypeString")))
				if s == 0 {
					reply([]byte{0}, nil)
					return
				}
				reply(append([]byte{1}, GoString(s)...), nil)
			},
			// args: u8 on: the display doesn't sleep while on.
			"keepAwake": func(args []byte, reply func([]byte, error)) {
				h.keepAwake(len(args) > 0 && args[0] == 1)
				reply(nil, nil)
			},
		},
		"browser": {
			// A Mac opens pages in the user's browser.
			"open": func(args []byte, reply func([]byte, error)) {
				if u := URL(string(args)); u != 0 {
					Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "openURL:", uintptr(u))
				}
				reply(nil, nil)
			},
		},
		"photos": {
			// args: u8 limit; result: the picked images' paths, a line each.
			"pick": func(args []byte, reply func([]byte, error)) { h.pickPhotos(args, reply) },
		},
	}
}

var activity ID

// keepAwake holds an activity that keeps the display awake.
func (h *Host) keepAwake(on bool) {
	pi := Send(Class("NSProcessInfo"), "processInfo")
	if activity != 0 {
		Send(pi, "endActivity:", uintptr(activity))
		Release(activity)
		activity = 0
	}
	if on {
		const idleDisplaySleepDisabled = 1 << 40
		const userInitiated = 0x00FFFFFF
		activity = Retain(Send(pi, "beginActivityWithOptions:reason:", idleDisplaySleepDisabled|userInitiated, uintptr(String("nui: KeepAwake"))))
	}
}

// pickPhotos asks for images with an open panel over the window.
func (h *Host) pickPhotos(args []byte, reply func([]byte, error)) {
	limit := 1
	if len(args) > 0 {
		limit = int(args[0])
	}
	p := Send(Class("NSOpenPanel"), "openPanel")
	Send(p, "setCanChooseFiles:", 1)
	Send(p, "setCanChooseDirectories:", 0)
	Send(p, "setAllowsMultipleSelection:", Bool(limit > 1))
	Dlopen("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers")
	if HasClass("UTType") {
		img := Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.image")))
		Send(p, "setAllowedContentTypes:", uintptr(Array(img)))
	}
	Retain(p)
	done := NewBlock(func(_ objc.Block, resp int) {
		defer Release(p)
		if resp != 1 { // NSModalResponseOK
			reply(nil, nil)
			return
		}
		var paths []string
		for _, u := range ArrayItems(Send(p, "URLs")) {
			if len(paths) < limit {
				paths = append(paths, GoString(Send(u, "path")))
			}
		}
		reply([]byte(strings.Join(paths, "\n")), nil)
	})
	Send(p, "beginSheetModalForWindow:completionHandler:", uintptr(h.keyWin().window), uintptr(done))
}

// The Keychain: generic passwords of the app's service (its bundle ID,
// or name), in the file-based keychain, which an app signed ad hoc can
// use (the data protection one needs an entitlement).

var errNotFound = errors.New("not found")

type keychainAPI struct {
	err                                error
	add, copy, update, delete, message uintptr
	class, genericPassword             ID
	service, account                   ID
	valueData, returnData              ID
	matchLimit, matchLimitOne, cfTrue  ID
}

var keychain = sync.OnceValue(func() *keychainAPI {
	lib := Dlopen("/System/Library/Frameworks/Security.framework/Security")
	if lib == 0 {
		return &keychainAPI{err: errors.New("keychain: can't load the Security framework")}
	}
	sym := func(name string) uintptr {
		p, _ := purego.Dlsym(lib, name)
		return p
	}
	k := &keychainAPI{add: sym("SecItemAdd"), copy: sym("SecItemCopyMatching"), update: sym("SecItemUpdate"),
		delete: sym("SecItemDelete"), message: sym("SecCopyErrorMessageString")}
	k.class, k.genericPassword = Const(lib, "kSecClass"), Const(lib, "kSecClassGenericPassword")
	k.service, k.account = Const(lib, "kSecAttrService"), Const(lib, "kSecAttrAccount")
	k.valueData, k.returnData = Const(lib, "kSecValueData"), Const(lib, "kSecReturnData")
	k.matchLimit, k.matchLimitOne = Const(lib, "kSecMatchLimit"), Const(lib, "kSecMatchLimitOne")
	k.cfTrue = Const(CoreFoundation(), "kCFBooleanTrue")
	return k
})

func keychainService() string {
	if id := GoString(Send(Send(Class("NSBundle"), "mainBundle"), "bundleIdentifier")); id != "" {
		return id
	}
	return theHost.appName
}

func (k *keychainAPI) query(key string) ID {
	q := Send(Class("NSMutableDictionary"), "dictionary")
	Send(q, "setObject:forKey:", uintptr(k.genericPassword), uintptr(k.class))
	Send(q, "setObject:forKey:", uintptr(String(keychainService())), uintptr(k.service))
	Send(q, "setObject:forKey:", uintptr(String(key)), uintptr(k.account))
	return q
}

func (k *keychainAPI) status(op string, s int32) error {
	switch s {
	case 0:
		return nil
	case -25300: // errSecItemNotFound
		return errNotFound
	}
	msg := "unknown error"
	if str, _, _ := purego.SyscallN(k.message, uintptr(s), 0); str != 0 {
		msg = GoString(ID(str))
		CFRelease(str)
	}
	return fmt.Errorf("keychain: %s: %s (OSStatus %d)", op, msg, s)
}

func osStatus(r uintptr) int32 { return int32(uint32(r)) }

func keychainGet(key string) (v []byte, err error) {
	k := keychain()
	if k.err != nil {
		return nil, k.err
	}
	q := k.query(key)
	Send(q, "setObject:forKey:", uintptr(k.cfTrue), uintptr(k.returnData))
	Send(q, "setObject:forKey:", uintptr(k.matchLimitOne), uintptr(k.matchLimit))
	var data uintptr
	r, _, _ := purego.SyscallN(k.copy, uintptr(q), uintptr(unsafe.Pointer(&data)))
	if err := k.status("read", osStatus(r)); err != nil {
		return nil, err
	}
	v = Bytes(ID(data))
	CFRelease(data)
	return v, nil
}

func keychainSet(key string, value []byte) error {
	k := keychain()
	if k.err != nil {
		return k.err
	}
	data := Data(value)
	update := Send(Class("NSMutableDictionary"), "dictionary")
	Send(update, "setObject:forKey:", uintptr(data), uintptr(k.valueData))
	r, _, _ := purego.SyscallN(k.update, uintptr(k.query(key)), uintptr(update))
	if s := osStatus(r); s != -25300 {
		return k.status("update", s)
	}
	add := k.query(key)
	Send(add, "setObject:forKey:", uintptr(data), uintptr(k.valueData))
	r, _, _ = purego.SyscallN(k.add, uintptr(add), 0)
	return k.status("add", osStatus(r))
}

func keychainDelete(key string) error {
	k := keychain()
	if k.err != nil {
		return k.err
	}
	r, _, _ := purego.SyscallN(k.delete, uintptr(k.query(key)))
	if err := k.status("delete", osStatus(r)); err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	return nil
}
