// Package agerange asks for the user's age range, as their platform
// account knows it, without their birth date (expo-age-range): Apple's
// Declared Age Range (iOS 26) and Google Play's Age Signals. Laws (Texas,
// Utah, Louisiana; Australia's) ask apps to know whether a user is a
// child, and to have a parent approve; the platforms ask the user (or
// their parent) whether to share.
//
// iOS: an app that imports it is given the Declared Age Range
// entitlement; its App ID needs the capability for a release build.
// Android: Play's library, which answers where Play's age checks apply,
// for an app installed from Play.
package agerange

import (
	"encoding/binary"
	"errors"

	"github.com/egoist/ryui/ui"
)

// Status is whether the age range came.
type Status uint8

const (
	// Declined: the user (or their parent) chose not to share it.
	Declined Status = iota
	// Shared: Range is the user's.
	Shared
	// VerificationRequired: Play needs the user to verify their age first
	// (Android).
	VerificationRequired
)

// Source is how the platform knows the age.
type Source uint8

const (
	Unknown          Source = iota
	SelfDeclared            // the user said
	GuardianDeclared        // their parent said, in Family Sharing
	Confirmed               // checked: an ID, a payment card, another way
)

// Result is the answer.
type Result struct {
	Status Status
	// Lower and Upper are the range, in years, inclusive: 0 is open (a
	// Lower of 18 and an Upper of 0 is 18 or older). iOS answers with the
	// age gates the app gave.
	Lower, Upper int
	Source       Source
}

// ErrNotAvailable: the platform can't answer (iOS before 26, no Play
// Store, the user's region without age checks).
var ErrNotAvailable = errors.New("agerange: not available")

// Request asks for the user's age range; the platform may ask them (or
// their parent) first. gates are the ages the app's rules change at (13,
// 16, 18: up to three; iOS answers which side of each the user is; Play
// answers its own ranges). Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request(gates ...int) (Result, error) {
	var args []byte
	for i, g := range gates {
		if i == 3 {
			break
		}
		args = binary.LittleEndian.AppendUint32(args, uint32(g))
	}
	b, err := ui.CallWait("ageRange", "request", args)
	if err != nil {
		if err.Error() == "not available" {
			return Result{}, ErrNotAvailable
		}
		return Result{}, err
	}
	return decode(b)
}

// decode: u8 status, u8 source, u32 lower, u32 upper.
func decode(b []byte) (Result, error) {
	if len(b) < 10 {
		return Result{}, errors.New("agerange: a bad reply")
	}
	le := binary.LittleEndian
	return Result{Status: Status(b[0]), Source: Source(b[1]), Lower: int(le.Uint32(b[2:])), Upper: int(le.Uint32(b[6:]))}, nil
}
