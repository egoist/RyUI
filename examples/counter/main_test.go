package main

import (
	"testing"

	"github.com/egoist/asuka/uitest"
)

func TestCounter(t *testing.T) {
	a := uitest.Mount(t, Counter)
	a.Expect("Count: 0")
	for range 10 {
		a.Tap("Add")
	}
	a.Expect("Count: 10", "That's a lot")
	a.Tap("Reset")
	a.Expect("Count: 0")
	a.ExpectNo("That's a lot")
}
