package biometric

import (
	"errors"
	"testing"
)

func TestWire(t *testing.T) {
	b := encode(Options{Reason: "Unlock", NoPasscode: true})
	if string(b) != "\x01\x06\x00\x00\x00Unlock\x00\x00\x00\x00" {
		t.Fatalf("%q", b)
	}
	for msg, want := range map[string]error{"canceled": ErrCanceled, "not available": ErrNotAvailable, "failed": ErrFailed} {
		if got := replyError(errors.New(msg)); got != want {
			t.Errorf("%s: %v", msg, got)
		}
	}
	if replyError(nil) != nil || replyError(errors.New("x")).Error() != "x" {
		t.Error("passthrough")
	}
	if Face.String() != "face" || Kind(9).String() != "optic" {
		t.Error(Face.String())
	}
}
