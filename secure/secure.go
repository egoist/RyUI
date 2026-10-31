// Package secure keeps small secrets (tokens, passwords) encrypted by the
// platform: the Keychain on iOS; on Android, values sealed with a key in
// the Android Keystore. As Expo's SecureStore: for secrets, not data.
//
// Get, Set and Delete wait for the platform: call them from a goroutine.
// On the main thread, Load, Save and Remove take a callback.
package secure

import (
	"encoding/binary"

	"github.com/egoist/ryui/ui"
)

func keyValue(key, value string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(key)))
	return append(append(b, key...), value...)
}

func found(b []byte) (string, bool) {
	if len(b) == 0 || b[0] == 0 {
		return "", false
	}
	return string(b[1:]), true
}

// Get is key's value; ok is false if there's none. Not on the main thread.
func Get(key string) (value string, ok bool, err error) {
	b, err := ui.CallWait("secureStore", "get", []byte(key))
	if err != nil {
		return "", false, err
	}
	value, ok = found(b)
	return value, ok, nil
}

// Set keeps value as key's. Not on the main thread.
func Set(key, value string) error {
	_, err := ui.CallWait("secureStore", "set", keyValue(key, value))
	return err
}

// Delete forgets key. Not on the main thread.
func Delete(key string) error {
	_, err := ui.CallWait("secureStore", "delete", []byte(key))
	return err
}

// Load is Get with a callback, on the main thread.
func Load(key string, done func(value string, ok bool, err error)) {
	ui.Call("secureStore", "get", []byte(key), func(b []byte, err error) {
		v, ok := found(b)
		done(v, ok, err)
	})
}

// Save is Set with a callback (or nil), on the main thread.
func Save(key, value string, done func(error)) {
	ui.Call("secureStore", "set", keyValue(key, value), func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}

// Remove is Delete with a callback (or nil), on the main thread.
func Remove(key string, done func(error)) {
	ui.Call("secureStore", "delete", []byte(key), func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}
