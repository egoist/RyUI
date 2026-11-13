package fileurl

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Paths go as file URLs, escaped, and come back the same; other URLs
// stay apart.
func TestRoundTrip(t *testing.T) {
	paths := []string{"/Users/a/Notes/a b.md", "/tmp/100%.txt", "/x/#y?.md", "/Users/a/日本語.txt"}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\Users\a\Notes\a b.md`, `D:\100%.txt`}
	}
	s := Join(paths)
	got, urls := Split(s)
	if !reflect.DeepEqual(got, paths) || urls != nil {
		t.Fatalf("Split(%q) = %q, %q; want %q", s, got, urls, paths)
	}
	got, urls = Split(s + "\nmyapp://note/1")
	if !reflect.DeepEqual(got, paths) || !reflect.DeepEqual(urls, []string{"myapp://note/1"}) {
		t.Fatalf("with a link: %q, %q", got, urls)
	}
}

func TestToPath(t *testing.T) {
	for _, c := range []struct {
		url  string
		path string
		ok   bool
	}{
		{"file:///a/b%20c.md", "/a/b c.md", true},
		{"FILE:///a", "/a", true},
		{"file://localhost/a/b", "/a/b", true},
		{"file://server/share/a", "", false}, // another machine's
		{"https://example.com/a.md", "", false},
		{"myapp://x", "", false},
		{"file://", "", false},
	} {
		p, ok := ToPath(c.url)
		if ok != c.ok || p != filepath.FromSlash(c.path) {
			t.Errorf("ToPath(%q) = %q, %v; want %q, %v", c.url, p, ok, c.path, c.ok)
		}
	}
	if p, ok := ToPath("file:///C:/Users/a%20b.txt"); !ok || p != filepath.FromSlash("C:/Users/a b.txt") {
		t.Errorf("a drive's path: %q, %v", p, ok)
	}
	if u := FromPath(`C:/Users/a b.txt`); u != "file:///C:/Users/a%20b.txt" {
		t.Errorf("FromPath of a drive's path: %q", u)
	}
}
