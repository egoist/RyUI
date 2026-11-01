package ops

import (
	"slices"
	"testing"
)

func TestMatchMIME(t *testing.T) {
	for _, c := range []struct {
		patterns []string
		mime     string
		want     bool
	}{
		{[]string{"*/*"}, "application/x-card", true},
		{[]string{"*"}, "image/png", true},
		{[]string{"image/*"}, "image/png", true},
		{[]string{"image/*"}, "IMAGE/HEIC", true},
		{[]string{"image/*"}, "imagery/png", false},
		{[]string{"image/*"}, "image", false},
		{[]string{"text/plain"}, "text/plain; charset=utf-8", true},
		{[]string{"Text/Plain"}, "text/plain", true},
		{[]string{"text/plain"}, "text/uri-list", false},
		{[]string{"video/*", "text/uri-list"}, "text/uri-list", true},
		{nil, "text/plain", false},
	} {
		if got := MatchMIME(c.patterns, c.mime); got != c.want {
			t.Errorf("MatchMIME(%q, %q) = %v", c.patterns, c.mime, got)
		}
	}
}

func TestItemsRoundTrip(t *testing.T) {
	items := []Item{
		{Kind: ShareKindImage, Path: "/a/b.png", Name: "b.png", MIME: "image/png", Size: 12},
		{Kind: ShareKindText, Text: "hi", MIME: "text/plain", Size: -1},
	}
	if got := DecodeItems(AppendItems(nil, items)); !slices.Equal(got, items) {
		t.Fatalf("items: %+v", got)
	}
	drag := []Item{
		{Kind: ShareKindLink, Text: "https://ryui.dev", MIME: "text/uri-list", Size: -1, Value: true},
		{Kind: ShareKindFile, Path: "/x.pdf", Name: "x.pdf", MIME: "application/pdf", Size: -1},
	}
	if got := DecodeDragItems(AppendDragItems(nil, drag)); !slices.Equal(got, drag) {
		t.Fatalf("drag items: %+v", got)
	}
	d := Drop{X: 1.5, Y: 2, Source: 7, Copied: true, Items: items}
	got, ok := DecodeDrop(AppendDrop(nil, &d))
	if !ok || got.X != 1.5 || got.Y != 2 || got.Source != 7 || !got.Copied || !slices.Equal(got.Items, items) {
		t.Fatalf("drop: %+v", got)
	}
	if _, ok := DecodeDrop([]byte{1, 2, 3}); ok {
		t.Fatal("a cut header decoded")
	}
	s := []string{"image/*", "", "text/plain"}
	if got := DecodeStrings(AppendStrings(nil, s)); !slices.Equal(got, s) {
		t.Fatalf("strings: %q", got)
	}
}

// A cut list ends where it's cut, and a corrupt length allocates nothing
// like what it claims.
func TestItemsCut(t *testing.T) {
	b := AppendItems(nil, []Item{{Text: "one", Size: -1}, {Text: "two", Size: -1}})
	if got := DecodeItems(b[:len(b)-3]); len(got) != 1 || got[0].Text != "one" {
		t.Fatalf("cut: %+v", got)
	}
	huge := []byte{1, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0x7f}
	allocs := testing.AllocsPerRun(10, func() { DecodeItems(huge) })
	if got := DecodeItems(huge); len(got) != 0 || allocs > 4 {
		t.Fatalf("corrupt: %+v, %v allocs", got, allocs)
	}
}
