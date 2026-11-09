package main

import (
	"slices"
	"strings"
	"testing"
)

func TestIsVetTool(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"-V=full"}, true},
		{[]string{"-flags"}, true},
		{[]string{"/tmp/go-build1/b001/vet.cfg"}, true},
		{[]string{"-reads=false", "-json", "/tmp/go-build1/b001/vet.cfg"}, true},
		{nil, false},
		{[]string{"vet"}, false},
		{[]string{"vet", "./..."}, false},
		{[]string{"dev", "-platform", "ios", "x.cfg"}, false},
		{[]string{"-h"}, false},
	} {
		if got := isVetTool(c.args); got != c.want {
			t.Errorf("isVetTool(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestParseVet(t *testing.T) {
	out := `# example.com/app
{
	"example.com/app": {
		"reads": [
			{
				"posn": "/src/app/main.go:14:34",
				"message": "n.Get() is read once"
			}
		],
		"mainthread": [
			{
				"posn": "/src/app/main.go:13:14",
				"message": "n.Set on a goroutine"
			}
		]
	}
}
# example.com/app/broken
{
	"example.com/app/broken": {
		"reads": {
			"error": "undefined: x"
		}
	}
}
`
	got := parseVet(strings.NewReader(out), "/src/app")
	want := []string{
		"main.go:13:14: n.Set on a goroutine (mainthread)",
		"main.go:14:34: n.Get() is read once (reads)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
