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

// FromArgs is the IncomingURL string of a launch's arguments (os.Args[1:]
// on Windows and Linux, a second launch's handed to the first): its
// files, as file:// URLs, and the URLs of the app's scheme, in order; ""
// if there are none. dir is the launch's working directory, which
// relative paths are taken from. A file is an argument that names one
// that exists (a folder too), or a file:// URL (the desktop file's %U);
// flags (-x, --x) and other words are the app's own (OnSecondInstance).
// stat reports whether a path exists (os.Stat's, tests replace it).
func FromArgs(args []string, dir, scheme string, stat func(string) bool) string {
	var b strings.Builder
	add := func(s string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}
	for _, a := range args {
		switch {
		case a == "" || strings.HasPrefix(a, "-"):
		case scheme != "" && len(a) > len(scheme) && strings.EqualFold(a[:len(scheme)], scheme) && a[len(scheme)] == ':':
			add(strings.NewReplacer("\r", "%0D", "\n", "%0A").Replace(a))
		case len(a) > 7 && strings.EqualFold(a[:7], "file://"):
			if p, ok := ToPath(a); ok {
				add(FromPath(p))
			}
		default:
			p := a
			if !filepath.IsAbs(p) && dir != "" && !(len(p) >= 2 && p[1] == ':') {
				p = filepath.Join(dir, p)
			}
			if stat(p) {
				add(FromPath(filepath.Clean(p)))
			}
		}
	}
	return b.String()
}
