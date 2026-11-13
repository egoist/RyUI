package agerange

import "testing"

func TestDecode(t *testing.T) {
	r, err := decode([]byte{1, 2, 13, 0, 0, 0, 15, 0, 0, 0})
	if err != nil || r != (Result{Status: Shared, Source: GuardianDeclared, Lower: 13, Upper: 15}) {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := decode([]byte{1}); err == nil {
		t.Fatal("short: no error")
	}
}
