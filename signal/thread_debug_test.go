//go:build nui_debug

package signal

import "testing"

func TestSetOffMainThreadPanics(t *testing.T) {
	BindThread()
	defer func() { mainGoroutine = 0 }()
	s := New(0)
	done := make(chan any)
	go func() {
		defer func() { done <- recover() }()
		s.Set(1)
	}()
	if <-done == nil {
		t.Fatal("no panic")
	}
}
