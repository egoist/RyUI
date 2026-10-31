// Package biometric confirms it's the device's owner with Face ID, Touch
// ID or a fingerprint (expo-local-authentication): to unlock the app, to
// confirm a payment, before showing a secret. iOS's LocalAuthentication;
// Android's BiometricPrompt. An app that imports it is given what it
// needs (iOS's NSFaceIDUsageDescription, which ryui.json's ios.usage
// says better; Android's USE_BIOMETRIC).
package biometric

import (
	"errors"

	"github.com/egoist/ryui/ui"
)

// Kind is the biometry a device has.
type Kind uint8

const (
	None        Kind = iota // none, or none enrolled
	Fingerprint             // Touch ID, a fingerprint sensor
	Face                    // Face ID, Android's face unlock (where it's strong)
	Iris                    // Android's iris
	Optic                   // Optic ID (Vision Pro)
)

func (k Kind) String() string {
	return [...]string{"none", "fingerprint", "face", "iris", "optic"}[min(int(k), 4)]
}

// State is what the device can do.
type State struct {
	// Kind is its biometry (on Android, the strongest kind it has, if it
	// says).
	Kind Kind
	// Enrolled: the user has set a face or a finger up, so Authenticate
	// can use it.
	Enrolled bool
	// Passcode: the device has a passcode, PIN or pattern, which
	// Authenticate falls back to.
	Passcode bool
}

// Errors Authenticate reports.
var (
	// ErrCanceled: the user, the app or the system (another app came
	// over) closed the prompt.
	ErrCanceled = errors.New("biometric: canceled")
	// ErrNotAvailable: the device has no biometry and no passcode, none
	// enrolled, or it's locked out after too many tries.
	ErrNotAvailable = errors.New("biometric: not available")
	// ErrFailed: it wasn't the owner.
	ErrFailed = errors.New("biometric: not recognized")
)

// Check is what the device can do. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Check() (State, error) {
	b, err := ui.CallWait("biometric", "check", nil)
	if err != nil {
		return State{}, err
	}
	if len(b) < 3 {
		return State{}, errors.New("biometric: a bad reply")
	}
	return State{Kind: Kind(b[0]), Enrolled: b[1] == 1, Passcode: b[2] == 1}, nil
}

// Options are the prompt's.
type Options struct {
	// Reason says why, in the prompt (Android's title; iOS's subtitle,
	// Touch ID's message).
	Reason string
	// Cancel is the cancel button's title (default: the system's).
	Cancel string
	// NoPasscode leaves the passcode out: biometry only (on Android, a
	// strong one: Class 3).
	NoPasscode bool
}

// Authenticate shows the system's prompt and reports whether it was the
// owner: nil, or ErrCanceled, ErrNotAvailable, ErrFailed. Not on the
// main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Authenticate(o Options) error {
	_, err := ui.CallWait("biometric", "authenticate", encode(o))
	return replyError(err)
}

// encode: u8 flags (1 no passcode), string reason, string cancel.
func encode(o Options) []byte {
	var b []byte
	if o.NoPasscode {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	b = appendString(b, o.Reason)
	return appendString(b, o.Cancel)
}

func appendString(b []byte, s string) []byte {
	n := len(s)
	b = append(b, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	return append(b, s...)
}

// replyError maps the hosts' error messages to the package's errors.
func replyError(err error) error {
	if err == nil {
		return nil
	}
	switch err.Error() {
	case "canceled":
		return ErrCanceled
	case "not available":
		return ErrNotAvailable
	case "failed":
		return ErrFailed
	}
	return err
}
