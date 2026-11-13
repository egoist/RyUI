// Package fileurl carries the files an app is opened with from a host to
// the core: the IncomingURL event's string, as file:// URLs, one per
// line. A URL has no raw line break (it's percent-encoded), so the
// files opened together (a selection in the file manager, a second
// launch's arguments) arrive together, to ui.OnOpenFiles; any other URL
// is ui.OnOpenURL's, as before. The hosts build the string with Join
// (Go's) or the same rule (Swift, Kotlin, JavaScript).
package fileurl

import (
	"net/url"
	"path/filepath"
	"strings"
)

// FromPath is path's file:// URL: absolute, slashes forward, escaped
// (a Windows path's drive in the URL's path: file:///C:/a%20b.txt).
func FromPath(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/x
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// Join is the IncomingURL string of paths, opened together.
func Join(paths []string) string {
	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(FromPath(p))
	}
	return b.String()
}

// ToPath is the local path a file:// URL names, and whether it is one
// (file:///path or file://localhost/path; not another host's).
func ToPath(s string) (string, bool) {
	if len(s) < 7 || !strings.EqualFold(s[:7], "file://") {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || u.Path == "" || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) {
		return "", false
	}
	p := u.Path
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' { // /C:/x on Windows
		p = p[1:]
	}
	return filepath.FromSlash(p), true
}

// Split is what an IncomingURL string holds: the paths of its file
// URLs, and its other URLs, in order.
func Split(s string) (paths, urls []string) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if p, ok := ToPath(line); ok {
			paths = append(paths, p)
			continue
		}
		urls = append(urls, line)
	}
	return paths, urls
}
