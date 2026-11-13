//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/secure on Linux: the user's keyring through libsecret (the Secret
// Service: GNOME Keyring, KWallet, KeePassXC), each key an item of the
// app's own schema (its ID), labeled with the app's name and the key.
// Without libsecret, or a keyring, the calls fail saying so.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["secureStore"] = h.secureModule()
	})
}

func (h *Host) secureModule() map[string]method {
	attrs := func(key string) map[string]string { return map[string]string{"key": key} }
	return map[string]method{
		// args: the key; result: u8 found, then the value.
		"get": func(args []byte, reply func([]byte, error)) {
			SecretLookup(h.appID, attrs(string(args)), func(v []byte, found bool, err error) {
				switch {
				case err != nil:
					reply(nil, err)
				case !found:
					reply([]byte{0}, nil)
				default:
					reply(append([]byte{1}, v...), nil)
				}
			})
		},
		// args: u32 key length, the key, the value.
		"set": func(args []byte, reply func([]byte, error)) {
			if len(args) < 4 || int(binary.LittleEndian.Uint32(args)) > len(args)-4 {
				reply(nil, errors.New("secureStore.set: bad arguments"))
				return
			}
			n := int(binary.LittleEndian.Uint32(args))
			key := string(args[4 : 4+n])
			SecretStore(h.appID, attrs(key), h.appName+": "+key, args[4+n:], func(err error) { reply(nil, err) })
		},
		"delete": func(args []byte, reply func([]byte, error)) {
			SecretClear(h.appID, attrs(string(args)), func(err error) { reply(nil, err) })
		},
	}
}
