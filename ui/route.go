package ui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/asuka/signal"
)

// Href is a screen's address in the app: a path and a query, as a web
// address has them ("/chat/7", "/search?q=go"), which a Router resolves
// to its route. Every screen of a Router has one: the web shows it in the
// address bar, a link of the app's scheme or domains opens it, and it's
// what comes back after a reload.
type Href string

// Path is the href's path, without its query: "/chat/7".
func (h Href) Path() string {
	p, _, _ := strings.Cut(string(h), "?")
	p, _, _ = strings.Cut(p, "#")
	return p
}

// Query is the href's query parameters.
func (h Href) Query() url.Values {
	_, q, _ := strings.Cut(string(h), "?")
	q, _, _ = strings.Cut(q, "#")
	v, _ := url.ParseQuery(q)
	return v
}

// With returns the href with the query parameter key set to value
// (removed when value is empty): ui.Href("/login").With("next", "/chat/7").
func (h Href) With(key, value string) Href {
	q := h.Query()
	if value == "" {
		q.Del(key)
	} else {
		q.Set(key, value)
	}
	return joinHref(h.Path(), q.Encode())
}

func joinHref(path, query string) Href {
	if path == "" {
		path = "/"
	}
	if query == "" {
		return Href(path)
	}
	return Href(path + "?" + query)
}

// HrefOf fills a route's pattern with values, in order: each parameter
// (":id") takes one, path-escaped, and a catch-all ("*path") the rest of
// the path ("a/b/c", its segments escaped one by one). A value is a
// string, an integer, a fmt.Stringer, or anything fmt formats.
//
//	ui.HrefOf("/chat/:id", 7)              // "/chat/7"
//	ui.HrefOf("/files/*path", "docs/a.md") // "/files/docs/a.md"
//
// It panics when the number of values isn't the pattern's number of
// parameters; asuka vet checks the pattern is a route's, and the count,
// before the app runs.
func HrefOf(pattern string, values ...any) Href {
	segs := parsePattern(pattern)
	var b strings.Builder
	n := 0
	for _, s := range segs {
		b.WriteByte('/')
		switch s.kind {
		case segStatic:
			b.WriteString(url.PathEscape(s.text))
		case segParam, segRest:
			if n >= len(values) {
				panic(fmt.Sprintf("ui: HrefOf(%q): %d values for %d parameters", pattern, len(values), countParams(segs)))
			}
			v := formatValue(values[n])
			n++
			if s.kind == segParam {
				b.WriteString(url.PathEscape(v))
				continue
			}
			b.WriteString(escapeRest(v))
		}
	}
	if n != len(values) {
		panic(fmt.Sprintf("ui: HrefOf(%q): %d values for %d parameters", pattern, len(values), countParams(segs)))
	}
	s := strings.TrimSuffix(b.String(), "/")
	if s == "" {
		s = "/"
	}
	return Href(s)
}

func countParams(segs []seg) int {
	n := 0
	for _, s := range segs {
		if s.kind != segStatic {
			n++
		}
	}
	return n
}

func formatValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case Href:
		return strings.TrimPrefix(string(x), "/")
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

// escapeRest escapes a catch-all's value segment by segment, keeping its
// slashes.
func escapeRest(v string) string {
	parts := strings.Split(strings.Trim(v, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// A pattern's segments: "chat" (static), ":id" (a parameter: one
// segment), "*path" (a catch-all: the rest, none or more segments).
type segKind uint8

const (
	segStatic segKind = iota
	segParam
	segRest
)

type seg struct {
	kind segKind
	text string // the static text, or the parameter's name
}

// parsePattern splits a pattern into its segments; empty ones ("//", a
// trailing slash) are dropped.
func parsePattern(p string) []seg {
	p = Href(p).Path()
	var out []seg
	for part := range strings.SplitSeq(p, "/") {
		switch {
		case part == "":
		case part[0] == ':' && len(part) > 1:
			out = append(out, seg{segParam, part[1:]})
		case part[0] == '*':
			name := part[1:]
			if name == "" {
				name = "rest"
			}
			out = append(out, seg{segRest, name})
		default:
			out = append(out, seg{segStatic, part})
		}
	}
	return out
}

// patternText is a pattern's segments as text: "/chat/:id".
func patternText(segs []seg) string {
	if len(segs) == 0 {
		return "/"
	}
	var b strings.Builder
	for _, s := range segs {
		b.WriteByte('/')
		switch s.kind {
		case segParam:
			b.WriteByte(':')
		case segRest:
			b.WriteByte('*')
		}
		b.WriteString(s.text)
	}
	return b.String()
}

// splitPath is a path's segments, unescaped (an escaped one stays as it
// is).
func splitPath(p string) []string {
	var out []string
	for part := range strings.SplitSeq(p, "/") {
		if part == "" {
			continue
		}
		if u, err := url.PathUnescape(part); err == nil {
			part = u
		}
		out = append(out, part)
	}
	return out
}

// match matches a path's segments against a pattern's, returning its
// parameters (nil for a pattern with none).
func match(segs []seg, path []string) (map[string]string, bool) {
	var params map[string]string
	set := func(k, v string) {
		if params == nil {
			params = map[string]string{}
		}
		params[k] = v
	}
	i := 0
	for _, s := range segs {
		switch s.kind {
		case segStatic:
			if i >= len(path) || path[i] != s.text {
				return nil, false
			}
			i++
		case segParam:
			if i >= len(path) {
				return nil, false
			}
			set(s.text, path[i])
			i++
		case segRest:
			set(s.text, strings.Join(path[i:], "/"))
			i = len(path)
		}
	}
	if i != len(path) {
		return nil, false
	}
	return params, true
}

// fillPattern is the path of segs with params filled in; ok is false
// when one is missing.
func fillPattern(segs []seg, params map[string]string) (string, bool) {
	if len(segs) == 0 {
		return "/", true
	}
	var b strings.Builder
	for _, s := range segs {
		switch s.kind {
		case segStatic:
			b.WriteByte('/')
			b.WriteString(url.PathEscape(s.text))
		case segParam:
			v, ok := params[s.text]
			if !ok || v == "" {
				return "", false
			}
			b.WriteByte('/')
			b.WriteString(url.PathEscape(v))
		case segRest:
			v, ok := params[s.text]
			if !ok {
				return "", false
			}
			if v = escapeRest(v); v != "" {
				b.WriteByte('/')
				b.WriteString(v)
			}
		}
	}
	if b.Len() == 0 {
		return "/", true
	}
	return b.String(), true
}

// rankLess reports whether pattern a is more specific than b, for paths
// both match, as expo-router ranks its routes: segment by segment, a
// static one before a parameter before a catch-all; where one ends, it's
// before the other's catch-all (which matches nothing there).
func rankLess(a, b []seg) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].kind != b[i].kind {
			return a[i].kind < b[i].kind
		}
	}
	switch {
	case len(a) < len(b):
		return b[len(a)].kind == segRest
	case len(a) > len(b):
		return a[len(b)].kind != segRest
	}
	return false
}

// Params are a route's parameters, given to its Screen: the path's (":id",
// "*path") and the query's.
type Params struct{ e *entry }

// Get returns the parameter name: the path's, else the query's (its first
// value). In an effect, it follows the query as SetQuery changes it; the
// path's parameters never change for a screen.
func (p Params) Get(name string) string {
	if p.e == nil {
		return ""
	}
	if v, ok := p.e.params[name]; ok {
		return v
	}
	return p.Query().Get(name)
}

// Int returns the parameter name as an integer (0 if it isn't one).
func (p Params) Int(name string) int {
	n, _ := strconv.Atoi(p.Get(name))
	return n
}

// Segments returns a catch-all's ("*path") segments: ["docs", "a.md"].
func (p Params) Segments(name string) []string {
	v := p.Get(name)
	if v == "" {
		return nil
	}
	return strings.Split(v, "/")
}

// Query returns the screen's query parameters; in an effect, it follows
// them as SetQuery changes them.
func (p Params) Query() url.Values {
	if p.e == nil {
		return url.Values{}
	}
	v, _ := url.ParseQuery(p.e.query.Get())
	return v
}

// Href returns the screen's own href; in an effect, it follows its query.
func (p Params) Href() Href {
	if p.e == nil {
		return ""
	}
	p.e.query.Get()
	return p.e.href()
}

// queryEqual is the query signal's equality: the same parameters, in any
// order.
func queryEqual(a, b string) bool {
	if a == b {
		return true
	}
	qa, _ := url.ParseQuery(a)
	qb, _ := url.ParseQuery(b)
	return qa.Encode() == qb.Encode()
}

func newQuery(q string) *signal.Signal[string] { return signal.NewFunc(q, queryEqual) }

// hrefOf parses an href into its path's segments and its query.
func hrefParts(h Href) (path []string, query string) {
	s := string(h)
	s, _, _ = strings.Cut(s, "#")
	p, q, _ := strings.Cut(s, "?")
	return splitPath(p), q
}

// toHref is the href a URL the app was opened with stands for: a link of
// its scheme (myapp://chat/7, myapp:///chat/7: "/chat/7", the host the
// first segment, as expo-router reads them), a universal or app link's
// path and query (https://example.com/chat/7), or a path itself. ok is
// false for what isn't a URL or a path.
func toHref(raw string) (Href, bool) {
	if strings.HasPrefix(raw, "/") {
		return Href(raw), true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Scheme == "file" {
		return "", false
	}
	p := u.EscapedPath()
	if u.Scheme != "http" && u.Scheme != "https" {
		if u.Opaque != "" { // myapp:chat/7
			p = "/" + strings.TrimPrefix(u.Opaque, "/")
		} else if u.Host != "" {
			p = "/" + u.Host + p
		}
	}
	if p == "" {
		p = "/"
	}
	return joinHref(p, u.RawQuery), true
}
