package crash

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// goReport is the report of Go's crash output: "panic: ..." or "fatal
// error: ...", then the goroutines' stacks.
func goReport(out string) *Report {
	r := &Report{Kind: Go, Fatal: true, Text: out, Message: firstLine(out)}
	r.Threads = goThreads(out)
	if len(r.Threads) > 0 {
		r.Threads[0].Crashed = true
	}
	return r
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// goThreads reads the goroutines in a Go traceback: each a header
// ("goroutine 1 [running]:", "runtime stack:"), then a function and its
// file a pair of lines each, until a blank line.
func goThreads(s string) []Thread {
	var out []Thread
	var cur *Thread
	var fn string // a frame's function, waiting for its file line
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "goroutine ") && strings.HasSuffix(l, ":"):
			out = append(out, Thread{Name: goroutineName(strings.TrimSuffix(l, ":"))})
			cur, fn = &out[len(out)-1], ""
		case l == "runtime stack:":
			out = append(out, Thread{Name: "runtime stack"})
			cur, fn = &out[len(out)-1], ""
		case cur == nil:
		case strings.TrimSpace(l) == "":
			cur, fn = nil, ""
		case strings.HasPrefix(l, "\t"):
			if fn == "" {
				continue
			}
			file, line := fileLine(strings.TrimSpace(l))
			cur.Frames = append(cur.Frames, Frame{Function: fn, File: file, Line: line})
			fn = ""
		case strings.HasPrefix(l, "created by "):
			// The goroutine's creator: a frame of its own, outermost.
			name, _, _ := strings.Cut(strings.TrimPrefix(l, "created by "), " in goroutine ")
			fn = name
		case strings.HasPrefix(l, "...") || strings.HasPrefix(l, "[originating from"):
			// "...additional frames elided...", "[originating from goroutine 1]"
		default:
			fn = funcName(l)
		}
	}
	return out
}

// goroutineName drops what GOTRACEBACK=system adds to a goroutine's
// header (gp=0x... m=...): "goroutine 1 [running]".
func goroutineName(h string) string {
	f := strings.Fields(h)
	var keep []string
	for _, w := range f {
		if strings.HasPrefix(w, "gp=") || strings.HasPrefix(w, "m=") || strings.HasPrefix(w, "mp=") {
			continue
		}
		keep = append(keep, w)
	}
	return strings.Join(keep, " ")
}

// funcName is a frame's function without its arguments:
// "main.(*T).m(0x1, ...)" is "main.(*T).m".
func funcName(l string) string {
	l = strings.TrimSpace(l)
	if i := strings.LastIndexByte(l, '('); i > 0 && strings.HasSuffix(l, ")") {
		return l[:i]
	}
	return l
}

// fileLine reads "/src/main.go:12 +0x1c".
func fileLine(l string) (string, int) {
	if i := strings.LastIndex(l, " +0x"); i > 0 {
		l = l[:i]
	}
	if i := strings.LastIndexByte(l, ':'); i > 0 {
		if n, err := strconv.Atoi(l[i+1:]); err == nil {
			return l[:i], n
		}
	}
	return l, 0
}

// windowsException is where, in what the runtime printed to standard
// error, it reports a Windows exception it couldn't handle ("Exception
// 0xc0000005 0x0 0x0 0x7ffb...", then "PC=" and the goroutines); -1 if
// it doesn't.
func windowsException(b []byte) int {
	for i := 0; i < len(b); {
		if bytes.HasPrefix(b[i:], []byte("Exception 0x")) {
			rest := b[i:]
			if nl := bytes.IndexByte(rest, '\n'); nl > 0 && bytes.HasPrefix(rest[nl+1:], []byte("PC=")) {
				return i
			}
		}
		nl := bytes.IndexByte(b[i:], '\n')
		if nl < 0 {
			break
		}
		i += nl + 1
	}
	return -1
}

// windowsExceptions names the exception codes a crash has.
var windowsExceptions = map[uint64]string{
	0xc0000005: "EXCEPTION_ACCESS_VIOLATION",
	0xc000001d: "EXCEPTION_ILLEGAL_INSTRUCTION",
	0xc0000094: "EXCEPTION_INT_DIVIDE_BY_ZERO",
	0xc00000fd: "EXCEPTION_STACK_OVERFLOW",
	0xc0000409: "STATUS_STACK_BUFFER_OVERRUN",
	0xc0000374: "STATUS_HEAP_CORRUPTION",
	0x80000003: "EXCEPTION_BREAKPOINT",
	0xe06d7363: "C++ exception",
}

// windowsReport is the report of a Windows exception the runtime printed.
func windowsReport(s string) *Report {
	r := &Report{Kind: Native, Fatal: true, Text: s}
	f := strings.Fields(firstLine(s))
	r.Message = "Exception"
	if len(f) > 1 {
		r.Message += " " + f[1]
		if code, err := strconv.ParseUint(strings.TrimPrefix(f[1], "0x"), 16, 64); err == nil {
			if name := windowsExceptions[code]; name != "" {
				r.Message = fmt.Sprintf("%s (%s)", r.Message, name)
			}
		}
	}
	r.Threads = goThreads(s)
	if len(r.Threads) > 0 {
		r.Threads[0].Crashed = true
	}
	return r
}
