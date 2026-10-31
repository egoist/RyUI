package store

import (
	"path/filepath"
	"testing"

	"github.com/egoist/ryui/signal"
)

func TestStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "store.json")
	usePath(p)
	type prefs struct{ Theme string }
	if err := Set("prefs", prefs{"dark"}); err != nil {
		t.Fatal(err)
	}
	Set("count", 3)
	Set("gone", true)
	Delete("gone")
	Flush()

	// A new launch reads what the last wrote.
	usePath(p)
	var got prefs
	var n int
	if !Get("prefs", &got) || got.Theme != "dark" || !Get("count", &n) || n != 3 {
		t.Fatalf("got %+v %d", got, n)
	}
	var gone bool
	if Get("gone", &gone) {
		t.Fatal("a deleted key is still there")
	}
	if Get("count", &got) {
		t.Fatal("a number decoded into a struct")
	}
}

func TestSignal(t *testing.T) {
	signal.BindThread()
	p := filepath.Join(t.TempDir(), "store.json")
	usePath(p)
	theme := Signal("theme", "light")
	if theme.Peek() != "light" {
		t.Fatalf("default %q", theme.Peek())
	}
	theme.Set("dark")
	Flush()
	usePath(p)
	if again := Signal("theme", "light"); again.Peek() != "dark" {
		t.Fatalf("kept %q", again.Peek())
	}
}
