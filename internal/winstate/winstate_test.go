package winstate

import (
	"os"
	"path/filepath"
	"testing"
)

type place struct {
	X, Y, Width, Height int32
	Maximized           bool
}

func TestStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data", "asuka-window.json")
	s := Open(p)
	var got place
	if s.Load(Main, &got) {
		t.Fatal("a state in a missing file")
	}
	s.Save(Main, place{X: 1, Y: 2, Width: 800, Height: 600})
	s.Save("inspector", place{Width: 300, Height: 200, Maximized: true})
	info, _ := os.Stat(p)
	// Saving the same again doesn't write.
	s.Save("inspector", place{Width: 300, Height: 200, Maximized: true})
	if info2, _ := os.Stat(p); !info2.ModTime().Equal(info.ModTime()) {
		t.Fatal("rewrote an unchanged file")
	}
	s = Open(p)
	if !s.Load(Main, &got) || got != (place{X: 1, Y: 2, Width: 800, Height: 600}) {
		t.Fatalf("main %+v", got)
	}
	if !s.Load("inspector", &got) || got != (place{Width: 300, Height: 200, Maximized: true}) {
		t.Fatalf("inspector %+v", got)
	}
	s.Forget("inspector")
	if Open(p).Load("inspector", &got) {
		t.Fatal("forgot nothing")
	}
}

// A file an older host wrote holds the main window's fields alone.
func TestOlderFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "asuka-window.json")
	os.WriteFile(p, []byte(`{"X":10,"Y":20,"Width":640,"Height":480,"DPI":96,"Maximized":true}`), 0o644)
	s := Open(p)
	var got place
	if !s.Load(Main, &got) || got != (place{X: 10, Y: 20, Width: 640, Height: 480, Maximized: true}) {
		t.Fatalf("main %+v", got)
	}
	s.Save("prefs", place{Width: 1})
	if !Open(p).Load(Main, &got) || got.Width != 640 {
		t.Fatal("the older main state was lost on the first save")
	}
	if Open("").Load(Main, &got) {
		t.Fatal("a pathless store has a state")
	}
}
