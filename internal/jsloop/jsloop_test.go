package jsloop

import "testing"

// InCallback is about the goroutine that entered: another one, running
// meanwhile, may wait.
func TestInCallback(t *testing.T) {
	if InCallback() {
		t.Fatal("in a callback before Enter")
	}
	Enter()
	if !InCallback() {
		t.Fatal("not in the callback it entered")
	}
	other := make(chan bool)
	go func() { other <- InCallback() }()
	if <-other && current() != 0 {
		t.Fatal("another goroutine is in the callback")
	}
	Enter() // nested
	Leave()
	if !InCallback() {
		t.Fatal("not in the callback after a nested one left")
	}
	Leave()
	if InCallback() {
		t.Fatal("in a callback after Leave")
	}
}
