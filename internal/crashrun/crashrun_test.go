package crashrun

import (
	"os"
	"path/filepath"
	"testing"
)

// An app that doesn't report crashes keeps nothing: no files, no crash
// output, and the hosts install nothing (Start's "").
func TestNotEnabled(t *testing.T) {
	d := t.TempDir()
	Reset(d)
	t.Cleanup(func() { Reset("") })
	Enabled = false
	if dir := Start(Info{System: "iOS"}); dir != "" {
		t.Fatalf("started in %q", dir)
	}
	if _, err := os.Stat(filepath.Join(d, "crashes")); err == nil {
		t.Fatal("files made")
	}
	Breadcrumb("x", 10)
	if Started() {
		t.Fatal("started")
	}
}

// Started, the run has its files, its crash output locked while it lives,
// and the device's info as its context's first lines.
func TestStart(t *testing.T) {
	d := t.TempDir()
	Reset(d)
	t.Cleanup(func() { Reset(""); Enabled = false })
	Enabled = true
	dir := Start(Info{System: "Android"})
	if dir != filepath.Join(d, "crashes") || Start(Info{AppVersion: "2.0"}) != dir {
		t.Fatalf("dir %q", dir)
	}
	base := filepath.Join(dir, "runs", ID())
	if !Alive(base + ".crash") {
		t.Fatal("not alive")
	}
	b, err := os.ReadFile(base + ".ctx")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"run":{"pid":` // then the device, as it's learnt
	if string(b[:len(want)]) != want {
		t.Fatalf("context %s", b)
	}
	_, info, _, _, _ := Snapshot()
	if info != (Info{System: "Android", AppVersion: "2.0"}) {
		t.Fatalf("info %+v", info)
	}
	Reset(d)
	if Alive(base + ".crash") {
		t.Fatal("alive once ended")
	}
}
