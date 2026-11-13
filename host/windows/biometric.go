//go:build windows

package windows

import (
	"errors"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/biometric on Windows: Windows Hello (UserConsentVerifier).
// "check" answers u8 kind (none, fingerprint, face: the sensor Windows
// Hello has, from the Windows Biometric Framework), u8 enrolled (a sensor,
// and Windows Hello set up), u8 passcode (Windows Hello set up: its PIN);
// "authenticate" (u8 flags: 1 no passcode; string reason; string cancel)
// shows Windows Hello's prompt over the window in use, with the reason,
// and fails with "canceled", "not available" or "failed". Windows
// Hello's prompt always offers the PIN, and names its own cancel button:
// with no passcode asked, it shows only where there's a sensor.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["biometric"] = h.biometricModule() })
}

// helloCheck and helloVerify ask Windows; a test replaces them.
var (
	helloCheck  = HelloCheck
	helloVerify = HelloVerify
	helloSensor = BiometricSensor
)

func (h *Host) biometricModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"check": func(_ []byte, reply func([]byte, error)) {
			kind := byte(helloSensor())
			helloCheck(func(a HelloAvailability, _ error) {
				h.Post(func() {
					set := a == HelloAvailable
					reply([]byte{kind, b2u(set && kind != 0), b2u(set)}, nil)
				})
			})
		},
		"authenticate": func(args []byte, reply func([]byte, error)) {
			if len(args) < 9 {
				reply(nil, errors.New("not available"))
				return
			}
			noPasscode := args[0]&1 != 0
			reason, _ := readString(args[1:])
			if reason == "" {
				reason = h.appName
			}
			if noPasscode && helloSensor() == 0 {
				reply(nil, errors.New("not available"))
				return
			}
			helloCheck(func(a HelloAvailability, _ error) {
				h.Post(func() {
					if a != HelloAvailable {
						reply(nil, errors.New("not available"))
						return
					}
					helloVerify(h.inUseWindow(), reason, func(r HelloResult, _ error) {
						h.Post(func() {
							switch r {
							case HelloVerified:
								reply(nil, nil)
							case HelloCanceled:
								reply(nil, errors.New("canceled"))
							case HelloNoDevice, HelloNotConfiguredForUser, HelloDisabled, HelloRetriesExhausted, HelloBusy:
								reply(nil, errors.New("not available"))
							default:
								reply(nil, errors.New("failed"))
							}
						})
					})
				})
			})
		},
	}
}
