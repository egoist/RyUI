//go:build linux && !android

package linux

import "errors"

// The modules of a phone's (and its store's) that Linux has no
// equivalent of, answered as each package reads "not here": biometric
// (no system prompt for the owner: none enrolled, no passcode), review
// (no store prompt: handled, not shown), tracking (no tracking prompt:
// allowed, as on Android), integrity (no attestation: not supported),
// ageRange (not available) and push (no push service).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		unsupported := func(what string) method {
			return func(_ []byte, reply func([]byte, error)) {
				reply(nil, errors.New(what+" isn't supported on Linux"))
			}
		}
		m["biometric"] = map[string]method{
			// u8 kind (none), u8 enrolled, u8 passcode.
			"check":        func(_ []byte, reply func([]byte, error)) { reply([]byte{0, 0, 0}, nil) },
			"authenticate": func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not available")) },
		}
		m["review"] = map[string]method{
			"request": func(_ []byte, reply func([]byte, error)) { reply(nil, nil) },
		}
		allowed := func(_ []byte, reply func([]byte, error)) { reply([]byte{3}, nil) }
		m["tracking"] = map[string]method{"request": allowed, "status": allowed}
		m["integrity"] = map[string]method{
			"supported":   func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
			"generateKey": unsupported("asuka/sdk/integrity"),
			"attestKey":   unsupported("asuka/sdk/integrity"),
			"assert":      unsupported("asuka/sdk/integrity"),
			"prepare":     unsupported("asuka/sdk/integrity"),
			"token":       unsupported("asuka/sdk/integrity"),
		}
		m["ageRange"] = map[string]method{
			"request": func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not available")) },
		}
		m["push"] = map[string]method{
			"register":     unsupported("asuka/sdk/push"),
			"nextRewrite":  unsupported("asuka/sdk/push"),
			"nextReceived": unsupported("asuka/sdk/push"),
			"nextOpened":   unsupported("asuka/sdk/push"),
			"rewritten":    func(_ []byte, reply func([]byte, error)) { reply(nil, nil) },
			"stop":         func(_ []byte, reply func([]byte, error)) { reply(nil, nil) },
		}
	})
}
