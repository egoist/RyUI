package watch

import (
	"fmt"
	"path"
	"strings"
)

// Patterns returns an Options.Ignore that ignores the files and
// directories matching any of patterns, written as a .gitignore's lines:
//
//   - A pattern without a slash matches a name at any depth: "node_modules",
//     "*.log", ".*".
//   - A pattern with a slash matches the path from the watched directory:
//     "build/cache", "docs/*.tmp"; a leading slash anchors one without
//     another ("/dist").
//   - A trailing slash matches only directories: "tmp/".
//   - "**" matches any number of directories: "**/testdata",
//     "src/**/gen", "logs/**".
//
// Within a name, path.Match's syntax: *, ?, [a-z] and \ escapes. What is
// in an ignored directory is ignored with it, and never looked at.
// Patterns panics on a malformed pattern.
func Patterns(patterns ...string) func(path string, isDir bool) bool {
	type pattern struct {
		parts   []string // a path's, split at slashes; one for a name
		anyName bool     // a name at any depth
		dirOnly bool
	}
	var ps []pattern
	for _, raw := range patterns {
		p := raw
		var c pattern
		if strings.HasSuffix(p, "/") {
			c.dirOnly = true
			p = strings.TrimSuffix(p, "/")
		}
		c.anyName = !strings.Contains(p, "/")
		p = strings.TrimPrefix(p, "/")
		if p == "" {
			panic(fmt.Sprintf("watch: empty pattern %q", raw))
		}
		c.parts = strings.Split(p, "/")
		for _, part := range c.parts {
			if _, err := path.Match(part, ""); err != nil || part == "" {
				panic(fmt.Sprintf("watch: malformed pattern %q", raw))
			}
		}
		ps = append(ps, c)
	}
	return func(p string, isDir bool) bool {
		var parts []string
		for _, c := range ps {
			if c.dirOnly && !isDir {
				continue
			}
			if c.anyName {
				if ok, _ := path.Match(c.parts[0], path.Base(p)); ok {
					return true
				}
				continue
			}
			if parts == nil {
				parts = strings.Split(p, "/")
			}
			if matchParts(c.parts, parts) {
				return true
			}
		}
		return false
	}
}

// matchParts reports whether the parts of a path match those of a
// pattern, where "**" matches any number of them.
func matchParts(pattern, parts []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			rest := pattern[1:]
			if len(rest) == 0 {
				return len(parts) > 0 // "logs/**": what is in logs
			}
			for i := 0; i <= len(parts); i++ {
				if matchParts(rest, parts[i:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		if ok, _ := path.Match(pattern[0], parts[0]); !ok {
			return false
		}
		pattern, parts = pattern[1:], parts[1:]
	}
	return len(parts) == 0
}
