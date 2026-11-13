//go:build linux && !android

package gtk

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/ebitengine/purego"
)

// libsecret: the user's keyring (GNOME Keyring, KWallet, KeePassXC: the
// Secret Service), through the portal's in a sandbox. Loaded the first
// time it's used; a system without it answers an error.

var libsecretAPI struct {
	once sync.Once
	err  error

	schemaNewv   func(name string, flags int32, attrs Ptr) Ptr
	valueNew     func(data *byte, n int, contentType string) Ptr
	valueGet     func(v Ptr, n *uintptr) Ptr
	valueUnref   func(v Ptr)
	storev       func(schema, attrs, collection Ptr, label string, value, cancellable, cb, data Ptr)
	storeFinish  func(res Ptr, err *Ptr) bool
	lookupv      func(schema, attrs, cancellable, cb, data Ptr)
	lookupFinish func(res Ptr, err *Ptr) Ptr
	clearv       func(schema, attrs, cancellable, cb, data Ptr)
	clearFinish  func(res Ptr, err *Ptr) bool

	schemas map[string]Ptr
}

// LoadSecret opens libsecret, once.
func LoadSecret() error {
	libsecretAPI.once.Do(func() {
		var lib uintptr
		var err error
		for _, name := range []string{"libsecret-1.so.0", "libsecret-1.so"} {
			if lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
				break
			}
		}
		if lib == 0 {
			libsecretAPI.err = fmt.Errorf("ryui/secure: libsecret isn't installed (%v)", err)
			return
		}
		for _, f := range []struct {
			p    any
			name string
		}{
			{&libsecretAPI.schemaNewv, "secret_schema_newv"},
			{&libsecretAPI.valueNew, "secret_value_new"},
			{&libsecretAPI.valueGet, "secret_value_get"},
			{&libsecretAPI.valueUnref, "secret_value_unref"},
			{&libsecretAPI.storev, "secret_password_storev_binary"},
			{&libsecretAPI.storeFinish, "secret_password_store_finish"},
			{&libsecretAPI.lookupv, "secret_password_lookupv"},
			{&libsecretAPI.lookupFinish, "secret_password_lookup_binary_finish"},
			{&libsecretAPI.clearv, "secret_password_clearv"},
			{&libsecretAPI.clearFinish, "secret_password_clear_finish"},
		} {
			p, err := purego.Dlsym(lib, f.name)
			if err != nil {
				libsecretAPI.err = fmt.Errorf("ryui/secure: libsecret is too old (no %s): 0.19 or later is needed", f.name)
				return
			}
			purego.RegisterFunc(f.p, p)
		}
		libsecretAPI.schemas = map[string]Ptr{}
	})
	return libsecretAPI.err
}

// strTable is a GHashTable of strings to strings (or to 0, values nil),
// which owns copies of them.
func libsecretTable(m map[string]string, values bool) Ptr {
	var valueFree Ptr
	if values {
		valueFree = FnFree
	}
	t := Sys.HashTableNewFull(FnStrHash, FnStrEqual, FnFree, valueFree)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var v Ptr
		if values {
			v = Sys.Strdup(m[k])
		}
		Sys.HashTableInsert(t, Sys.Strdup(k), v)
	}
	return t
}

// schema is a schema of string attributes, made once per name.
func libsecretSchema(name string, attrs map[string]string) Ptr {
	if s := libsecretAPI.schemas[name]; s != 0 {
		return s
	}
	t := libsecretTable(attrs, false) // SECRET_SCHEMA_ATTRIBUTE_STRING is 0
	s := libsecretAPI.schemaNewv(name, 0, t)
	Sys.HashTableUnref(t)
	libsecretAPI.schemas[name] = s
	return s
}

// SecretStore stores value as the item of schema with the attributes
// (replacing the one there), labeled label, in the default collection.
// done runs on the main thread.
func SecretStore(schema string, attrs map[string]string, label string, value []byte, done func(error)) {
	if err := LoadSecret(); err != nil {
		done(err)
		return
	}
	sc := libsecretSchema(schema, attrs)
	t := libsecretTable(attrs, true)
	buf := append(append([]byte(nil), value...), 0)
	v := libsecretAPI.valueNew(&buf[0], len(value), "application/octet-stream")
	Async(func(a AsyncCall) {
		libsecretAPI.storev(sc, t, 0, label, v, 0, a.Callback, a.Data)
	}, func(_, res Ptr) {
		Sys.HashTableUnref(t)
		libsecretAPI.valueUnref(v)
		var gerr Ptr
		if !libsecretAPI.storeFinish(res, &gerr) {
			done(libsecretErr(gerr))
			return
		}
		done(nil)
	})
}

// SecretLookup finds the item of schema with the attributes: its value,
// or found false.
func SecretLookup(schema string, attrs map[string]string, done func(value []byte, found bool, err error)) {
	if err := LoadSecret(); err != nil {
		done(nil, false, err)
		return
	}
	sc := libsecretSchema(schema, attrs)
	t := libsecretTable(attrs, true)
	Async(func(a AsyncCall) {
		libsecretAPI.lookupv(sc, t, 0, a.Callback, a.Data)
	}, func(_, res Ptr) {
		Sys.HashTableUnref(t)
		var gerr Ptr
		v := libsecretAPI.lookupFinish(res, &gerr)
		if gerr != 0 {
			done(nil, false, libsecretErr(gerr))
			return
		}
		if v == 0 {
			done(nil, false, nil)
			return
		}
		var n uintptr
		p := libsecretAPI.valueGet(v, &n)
		b := GoBytes(p, int(n))
		if b == nil {
			b = []byte{}
		}
		libsecretAPI.valueUnref(v)
		done(b, true, nil)
	})
}

// SecretClear removes the items of schema with the attributes (none is
// no error).
func SecretClear(schema string, attrs map[string]string, done func(error)) {
	if err := LoadSecret(); err != nil {
		done(err)
		return
	}
	sc := libsecretSchema(schema, attrs)
	t := libsecretTable(attrs, true)
	Async(func(a AsyncCall) {
		libsecretAPI.clearv(sc, t, 0, a.Callback, a.Data)
	}, func(_, res Ptr) {
		Sys.HashTableUnref(t)
		var gerr Ptr
		libsecretAPI.clearFinish(res, &gerr)
		if gerr != 0 {
			done(libsecretErr(gerr))
			return
		}
		done(nil)
	})
}

func libsecretErr(gerr Ptr) error {
	err := GError(gerr)
	if err == nil {
		return nil
	}
	return errors.New("ryui/secure: the keyring: " + err.Error())
}
