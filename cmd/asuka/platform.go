package main

import (
	"flag"
	"fmt"
	"runtime"
	"slices"
	"strings"
)

// The platforms an app runs on, as -platform names them.
const (
	platformIOS     = "ios"
	platformAndroid = "android"
	platformMac     = "mac"
	platformWindows = "windows"
	platformWeb     = "web"
	platformLinux   = "linux"
)

// platformFlag defines -platform on fs: one of names, stored in p. Its
// usage names its argument in backquotes, as flag.UnquoteUsage reads it.
func platformFlag(fs *flag.FlagSet, p *string, names []string, usage string) {
	fs.Func("platform", usage, func(s string) error {
		if !slices.Contains(names, s) {
			return fmt.Errorf("%q: one of %s", s, strings.Join(names, ", "))
		}
		*p = s
		return nil
	})
}

// thisComputer is the platform of the computer asuka runs on, for a dev
// or a build that names none: mac on macOS, windows on Windows, linux on
// Linux; "" where there's no desktop host yet.
func thisComputer() string {
	switch runtime.GOOS {
	case "darwin":
		return platformMac
	case "windows":
		return platformWindows
	case "linux":
		return platformLinux
	}
	return ""
}
