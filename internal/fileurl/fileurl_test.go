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

// A launch's arguments: files that exist (relative to its directory),
// file URLs and the scheme's URLs, not flags nor other words.
func TestFromArgs(t *testing.T) {
	exists := map[string]bool{
		filepath.FromSlash("/home/a/notes/a b.md"): true,
		filepath.FromSlash("/tmp/x.md"):            true,
	}
	stat := func(p string) bool { return exists[p] }
	args := []string{"--new-window", "a b.md", "missing.md", "/tmp/x.md", "file:///srv/y%20z.md", "MyApp://note/1", "https://example.com", "-v"}
	got := FromArgs(args, filepath.FromSlash("/home/a/notes"), "myapp", stat)
	want := "file:///home/a/notes/a%20b.md\nfile:///tmp/x.md\nfile:///srv/y%20z.md\nMyApp://note/1"
	if runtime.GOOS == "windows" {
		return // paths with drives: covered by TestToPath
	}
	if got != want {
		t.Fatalf("FromArgs = %q\nwant %q", got, want)
	}
	if FromArgs([]string{"--flag", "word"}, "/", "", stat) != "" {
		t.Fatal("flags and words aren't files")
	}
}
