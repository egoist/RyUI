//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/egoist/ryui/internal/boot"
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/native"
	"github.com/egoist/ryui/ops"
)

// method is a native module's method: it answers with reply, at once or
// later (on the main thread).
type method func(args []byte, reply func([]byte, error))

func readString(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(le.Uint32(b))
	if len(b) < 4+n {
		return "", nil
	}
	return string(b[4 : 4+n]), b[4+n:]
}

// call runs a native module's method (ui.Call) and answers with Result.
func (h *Host) call(root *node, args []byte) {
	if len(args) < 4 {
		return
	}
	callID := binary.LittleEndian.Uint32(args)
	module, rest := readString(args[4:])
	name, rest := readString(rest)
	id := root.id
	answer := func(res []byte, err error) {
		out := binary.LittleEndian.AppendUint32(nil, callID)
		if err != nil {
			out = append(out, 0)
			out = append(out, err.Error()...)
		} else {
			out = append(out, 1)
			out = append(out, res...)
		}
		ev := ops.Event{ID: id, Kind: ops.EventResult, Bytes: out}
		// Answered after the batch, as a host's modules do.
		h.Post(func() { h.dispatch(ev) })
	}
	m := h.module(module)[name]
	if m == nil {
		answer(nil, errors.New("ryui: "+module+"."+name+" isn't on Linux yet"))
		return
	}
	m(rest, answer)
}

func (h *Host) module(name string) map[string]method {
	if h.modules == nil {
		h.modules = h.builtinModules()
	}
	if m := h.modules[name]; m != nil {
		return m
	}
	// The app's own (package native).
	if am := native.ModuleNamed(name); am != nil {
		m := make(map[string]method, len(am))
		for k, f := range am {
			m[k] = f
		}
		return m
	}
	return nil
}

func (h *Host) builtinModules() map[string]map[string]method {
	mods := map[string]map[string]method{
		"device": {
			// result: JSON: model, system, systemVersion, appVersion, build.
			"info": func(args []byte, reply func([]byte, error)) {
				b, err := json.Marshal(map[string]string{
					"model": "PC", "system": "Linux", "systemVersion": osVersion(),
					"appVersion": boot.Version, "build": boot.Build,
				})
				reply(b, err)
			},
			// result: u8 has, then the clipboard's text.
			"clipboard": func(args []byte, reply func([]byte, error)) {
				h.readClipboardText(func(s string, ok bool) {
					if !ok {
						reply([]byte{0}, nil)
						return
					}
					reply(append([]byte{1}, s...), nil)
				})
			},
			// args: u8 on: the session doesn't go idle while on.
			"keepAwake": func(args []byte, reply func([]byte, error)) {
				h.keepAwake(len(args) > 0 && args[0] == 1)
				reply(nil, nil)
			},
		},
		"browser": {
			// args: the URL: the user's browser opens it.
			"open": func(args []byte, reply func([]byte, error)) {
				h.openURL(string(args))
				reply(nil, nil)
			},
		},
	}
	h.moreModules(mods)
	return mods
}

// osVersion is the distribution and its version (os-release).
func osVersion() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	name, ver := "", ""
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "NAME":
			name = v
		case "VERSION_ID":
			ver = v
		}
	}
	return strings.TrimSpace(name + " " + ver)
}

// openURL opens a URL with the user's app for it (through the portal in
// a sandbox).
func (h *Host) openURL(u string) {
	l := UriLauncherNew(u)
	var parent Ptr
	if h.main != nil {
		parent = h.main.window
	}
	UriLauncherLaunch(l, parent, 0, 0, 0)
	ObjectUnref(l)
}

// share shares text: Linux has no share sheet, so it's copied, as on
// Windows, with a toast saying so.
func (h *Host) share(n *node, text string) {
	ClipboardSetText(DisplayGetClipboard(DisplayGetDefault()), text)
	h.toast("Copied to clipboard")
}

// keepAwake inhibits the session's idling (the screen blanking) while on.
func (h *Host) keepAwake(on bool) {
	if on == (h.inhibit != 0) {
		return
	}
	if on {
		var win Ptr
		if h.main != nil {
			win = h.main.window
		}
		h.inhibit = ApplicationInhibit(h.app, win, 8, "Keeping the screen on") // GTK_APPLICATION_INHIBIT_IDLE
		return
	}
	ApplicationUninhibit(h.app, h.inhibit)
	h.inhibit = 0
}

// readClipboardText reads the clipboard's text (asynchronously, as GDK
// does).
func (h *Host) readClipboardText(done func(string, bool)) {
	cb := DisplayGetClipboard(DisplayGetDefault())
	Async(func(cb2 AsyncCall) {
		ClipboardReadTextAsync(cb, 0, cb2.Callback, cb2.Data)
	}, func(source, res Ptr) {
		var gerr Ptr
		p := ClipboardReadTextFinish(cb, res, &gerr)
		if gerr != 0 {
			ErrorFree(gerr)
		}
		if p == 0 {
			done("", false)
			return
		}
		done(GoStringFree(p), true)
	})
}

// watchDesktop follows GNOME's settings that GTK doesn't: the screen
// reader, the clock's format.
func (h *Host) watchDesktop() {
	if s := settingsIfAny("org.gnome.desktop.a11y.applications", "screen-reader-enabled"); s != 0 {
		h.screenReader = SettingsGetBoolean(s, "screen-reader-enabled")
		Connect(s, "changed::screen-reader-enabled", func(uintptr) {
			h.screenReader = SettingsGetBoolean(s, "screen-reader-enabled")
			h.envChanged()
		})
	}
	if s := settingsIfAny("org.gnome.desktop.interface", "clock-format"); s != 0 {
		read := func() {
			h.hourCycle = 24
			if GoStringFree(SettingsGetString(s, "clock-format")) == "12h" {
				h.hourCycle = 12
			}
		}
		read()
		Connect(s, "changed::clock-format", func(uintptr) { read(); h.envChanged() })
	}
}

// settingsIfAny is a GSettings of a schema the system has, with the key
// (0 when it hasn't: not every desktop is GNOME's).
func settingsIfAny(schema, key string) Ptr {
	src := SettingsSchemaSourceGetDefault()
	if src == 0 {
		return 0
	}
	sc := SettingsSchemaSourceLookup(src, schema, true)
	if sc == 0 {
		return 0
	}
	defer SettingsSchemaUnref(sc)
	if !SettingsSchemaHasKey(sc, key) {
		return 0
	}
	return SettingsNew(schema)
}
