package network

import "testing"

func TestWire(t *testing.T) {
	for _, s := range []State{{}, {Connected: true, Type: WiFi}, {Connected: true, Type: Cellular, Metered: true, Constrained: true}} {
		got, err := decode(encode(s), nil)
		if err != nil || got != s {
			t.Errorf("%+v: %+v %v", s, got, err)
		}
	}
	if _, err := decode([]byte{1}, nil); err == nil {
		t.Error("short: no error")
	}
	if Cellular.String() != "cellular" || Type(9).String() != "other" {
		t.Error(Cellular.String(), Type(9).String())
	}
}
