// Package integrity proves to the app's server that a request comes from
// the app, unmodified, on a real device (expo-app-integrity): Apple's App
// Attest and Google's Play Integrity (its standard requests). The server
// checks what the device returns with Apple or Google; the app can't fake
// it.
//
// iOS: an app that imports it is given the App Attest entitlement; its
// App ID needs the capability for a release build. App Attest isn't on
// the simulator (Supported is false there). Android: the app's Google
// Cloud project number, from the Play Console (App integrity), and an app
// installed from Play.
package integrity

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"

	"github.com/egoist/ryui/ui"
)

// Supported reports whether the device can attest the app (iOS: App
// Attest on this device; Android: true, Play's answer comes with Token).
// Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Supported() bool {
	b, err := ui.CallWait("integrity", "supported", nil)
	return err == nil && len(b) > 0 && b[0] == 1
}

// GenerateKey makes a key pair in the Secure Enclave for App Attest and
// reports its ID: keep it (ryui/secure), it's the app's on this device.
// iOS. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func GenerateKey() (keyID string, err error) {
	b, err := ui.CallWait("integrity", "generateKey", nil)
	return string(b), err
}

// AttestKey has Apple attest the key, for the server's challenge (a
// one-time value it made), and returns the attestation (CBOR) for the
// server to check and keep the key's public part from. Once per key. iOS.
// Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func AttestKey(keyID string, challenge []byte) ([]byte, error) {
	return ui.CallWait("integrity", "attestKey", keyed(keyID, challenge))
}

// Assert signs a request's data (its body, with a challenge of the
// server's in it) with the attested key: the server checks the assertion
// (CBOR) with the key's public part. iOS. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Assert(keyID string, data []byte) ([]byte, error) {
	return ui.CallWait("integrity", "assert", keyed(keyID, data))
}

// keyed: string key ID, then the data's SHA-256 (App Attest's clientDataHash).
func keyed(keyID string, data []byte) []byte {
	h := sha256.Sum256(data)
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(keyID)))
	b = append(b, keyID...)
	return append(b, h[:]...)
}

// Prepare readies Play Integrity for Token, with the app's Google Cloud
// project number: at launch, a while before the first Token (it takes
// seconds). Android. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Prepare(cloudProjectNumber int64) error {
	_, err := ui.CallWait("integrity", "prepare", []byte(strconv.FormatInt(cloudProjectNumber, 10)))
	return err
}

// Token is Play Integrity's verdict on the app, the device and the
// request whose hash (a digest of the request's content, as the server
// computes it) is requestHash: an encrypted token for the server to
// decode with Google. After Prepare. Android. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Token(requestHash string) (string, error) {
	b, err := ui.CallWait("integrity", "token", []byte(requestHash))
	return string(b), err
}
