package signal

import (
	"runtime"
	"testing"
)

func TestClosureDepth(t *testing.T) {
	for _, c := range []struct {
		rest       string
		depth, num int
	}{
		{".func2", 1, 2},
		{".func2.func1", 2, 1},
		{".1", 1, 1}, // inlined
		{"-range1.func3", 1, 3},
		{"-range1-range1.1", 1, 1},
		{".func1-range1.func2", 2, 2},
		{"-range1", 0, 0}, // a loop's body is no literal
	} {
		if d, n := closureDepth(c.rest); d != c.depth || n != c.num {
			t.Errorf("%s: %d, %d; want %d, %d", c.rest, d, n, c.depth, c.num)
		}
	}
}

func TestPlaceLookups(t *testing.T) {
	defer func() { places = nil }()
	KeepPlaces("F\t/app/main.go\n" +
		"D\tmain.Screen\nD\tmain.ScreenList\nD\tmain.(*T[...]).M\n" +
		"L\t10\t20\t1\t1\tmain.Screen>body\n" +
		"L\t12\t12\t2\t1\tmain.Screen>body>ui.Show#1\n" +
		"L\t30\t30\t1\t2\tmain.Screen>run#0\n" +
		"L\t30\t30\t1\t3\tmain.Screen>run#1\n" +
		"L\t40\t41\t1\t1\tmain.(*T[...]).M>onTap\n" +
		"S\t11\t11\tcount\nS\t13\t13\ta\nS\t13\t13\tb\n" +
		"F\t/app/reset.go\nR\tabc\nS\t5\t5\tx\n")
	frame := func(fn, file string, line int) runtime.Frame {
		return runtime.Frame{Function: fn, File: file, Line: line}
	}
	for _, c := range []struct {
		fr   runtime.Frame
		want string
	}{
		{frame("main.Screen.func1", "/app/main.go", 11), "main.Screen>body"},
		{frame("main.Screen.1", "/app/main.go", 15), "main.Screen>body"},                     // inlined
		{frame("main.Screen.func1.func1", "/app/main.go", 12), "main.Screen>body>ui.Show#1"}, // the inner, on a shared line
		{frame("main.Screen.func1", "/app/main.go", 12), "main.Screen>body"},                 // the outer, likewise
		{frame("main.Screen.func2", "/app/main.go", 30), "main.Screen>run#0"},                // siblings on a line
		{frame("main.Screen.func3", "/app/main.go", 30), "main.Screen>run#1"},
		{frame("main.Screen.func9", "/app/main.go", 30), ""}, // can't tell
		{frame("main.(*T[...]).M.func1", "/app/main.go", 40), "main.(*T[...]).M>onTap"},
		{frame("main.ScreenList", "/app/main.go", 11), ""}, // a declared function
		{frame("main.Screen.func1", "/elsewhere.go", 11), ""},
	} {
		if got := places.closure(c.fr); got != c.want {
			t.Errorf("%s:%d: %q, want %q", c.fr.Function, c.fr.Line, got, c.want)
		}
	}
	if name, reset := places.site(frame("main.Screen.func1", "/app/main.go", 11)); name != "count" || reset != "" {
		t.Error(name, reset)
	}
	if name, _ := places.site(frame("main.Screen.func1", "/app/main.go", 13)); name != "" {
		t.Error("two on a line:", name)
	}
	if name, reset := places.site(frame("main.F", "/app/reset.go", 5)); name != "x" || reset != "abc" {
		t.Error(name, reset)
	}
}
