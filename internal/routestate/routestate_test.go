package routestate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeepsEachWindowsAddress(t *testing.T) {
	delay = time.Millisecond
	p := filepath.Join(t.TempDir(), "data", FileName)
	s := Open(p)
	if _, _, ok := s.Location("main"); ok {
		t.Fatal("an address in a new store")
	}
	s.Set("main", "/chat/7", []byte(`{"e":[]}`))
	s.Set("inspector", "/props", nil)
	s.Set("", "/ignored", nil)
	s.Flush()
	again := Open(p)
	if h, st, ok := again.Location("main"); !ok || h != "/chat/7" || string(st) != `{"e":[]}` {
		t.Fatalf("main: %q %q %v", h, st, ok)
	}
	if h, _, ok := again.Location("inspector"); !ok || h != "/props" {
		t.Fatalf("inspector: %q %v", h, ok)
	}
	// Written a moment later on its own.
	again.Set("main", "/chat/8", nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if h, _, _ := Open(p).Location("main"); h == "/chat/8" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never written")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestKeepsNoneInTestsAndDev(t *testing.T) {
	t.Setenv("ASUKA_DATA", t.TempDir())
	for _, k := range []string{"ASUKA_TEST", "ASUKA_STATE", "ASUKA_SCRIPT"} {
		t.Setenv(k, "1")
		if Path() != "" {
			t.Fatalf("%s: a path", k)
		}
		os.Unsetenv(k)
	}
	if Path() == "" {
		t.Fatal("no path")
	}
	s := Open("")
	s.Set("main", "/x", nil)
	s.Flush()
	if h, _, ok := s.Location("main"); !ok || h != "/x" {
		t.Fatal("an in-memory store forgot")
	}
}
