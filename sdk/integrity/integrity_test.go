package integrity

import (
	"crypto/sha256"
	"testing"
)

// A key's ID, then the data's SHA-256: App Attest's client data hash.
func TestKeyed(t *testing.T) {
	b := keyed("key", []byte("challenge"))
	h := sha256.Sum256([]byte("challenge"))
	if string(b[:7]) != "\x03\x00\x00\x00key" || string(b[7:]) != string(h[:]) {
		t.Fatalf("%x", b)
	}
}
