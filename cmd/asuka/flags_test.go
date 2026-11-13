package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestFlags(t *testing.T) {
	type got struct {
		platform, device string
		port             int
		once, verbose    bool
		args             []string
	}
	for _, c := range []struct {
		args []string
		want got
		err  string
	}{
		{[]string{"--platform", "ios", "app"}, got{platform: "ios", args: []string{"app"}}, ""},
		{[]string{"--platform=web", "--port=9000"}, got{platform: "web", port: 9000}, ""},
		{[]string{"-p", "android", "-d", "Pixel"}, got{platform: "android", device: "Pixel"}, ""},
		{[]string{"-pios", "-dPixel"}, got{platform: "ios", device: "Pixel"}, ""},
		{[]string{"-v", "--once"}, got{once: true, verbose: true}, ""},
		{[]string{"-vp", "ios"}, got{platform: "ios", verbose: true}, ""},
		{[]string{"-vpios"}, got{platform: "ios", verbose: true}, ""},
		{[]string{"app", "--once", "-p", "ios"}, got{platform: "ios", once: true, args: []string{"app"}}, ""},
		{[]string{"--once", "--", "-p", "--x"}, got{once: true, args: []string{"-p", "--x"}}, ""},
		{[]string{"-"}, got{args: []string{"-"}}, ""},
		{[]string{"--device", "-x"}, got{device: "-x"}, ""},
		{[]string{"-platform", "ios"}, got{}, "-platform: did you mean --platform?"},
		{[]string{"-once"}, got{}, "-once: did you mean --once?"},
		{[]string{"--nope"}, got{}, "unknown flag --nope (asuka dev --help lists them)"},
		{[]string{"-x"}, got{}, "unknown flag -x"},
		{[]string{"--once=true"}, got{}, "--once takes no value"},
		{[]string{"--platform"}, got{}, "--platform needs a value"},
		{[]string{"-p"}, got{}, "-p needs a value"},
		{[]string{"-p", "mac2"}, got{}, `-p: "mac2": one of`},
		{[]string{"--port", "x"}, got{}, `--port: "x" is not a whole number`},
	} {
		var o devOptions
		fs := devFlags(&o)
		err := fs.parse(c.args)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: %v, want %q", c.args, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		g := got{o.platform, o.device, o.port, o.once, o.verbose, fs.Args()}
		if g.platform != c.want.platform || g.device != c.want.device || g.port != c.want.port ||
			g.once != c.want.once || g.verbose != c.want.verbose || !slices.Equal(g.args, c.want.args) {
			t.Errorf("%q: %+v, want %+v", c.args, g, c.want)
		}
	}
}

func TestFlagsChanged(t *testing.T) {
	var o publishOptions
	fs := publishFlags(&o)
	if err := fs.parse([]string{"-p", "android", "--track", "internal"}); err != nil {
		t.Fatal(err)
	}
	if !fs.changed("platform") || !fs.changed("track") || fs.changed("rollout") {
		t.Error(fs.set)
	}
	if o.track != "internal" || o.rollout != 0 {
		t.Error("defaults:", o.track, o.rollout)
	}
}

func TestFlagsHelp(t *testing.T) {
	var o buildOptions
	fs := buildFlags(&o)
	var b bytes.Buffer
	fs.out = &b
	for _, h := range []string{"-h", "--help"} {
		b.Reset()
		if err := fs.parse([]string{"--simulator", h}); !errors.Is(err, errHelp) {
			t.Fatal(h, err)
		}
		help := b.String()
		for _, want := range []string{
			"usage: asuka build [flags] [dir]\n",
			"\n  -p, --platform <platform>\n        the platform to build for: ios,",
			"\n      --distribution <distribution>\n        iOS: app-store, ad-hoc or development (default: app-store)\n",
			"\n  -o, --out <directory>\n",
			"\n      --no-vet\n",
			"\n  -v, --verbose\n        print the commands asuka runs\n",
		} {
			if !strings.Contains(help, want) {
				t.Errorf("%s: no %q in\n%s", h, want, help)
			}
		}
		for _, l := range strings.Split(help, "\n") {
			if len(l) > 80 {
				t.Errorf("%s: a line over 80 columns: %q", h, l)
			}
		}
	}
}
