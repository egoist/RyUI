package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// isVetTool reports whether asuka was run by go vet as its -vettool: to
// describe itself (-V=full, -flags) or to check one package (a .cfg file,
// after the analyzers' flags).
func isVetTool(args []string) bool {
	if len(args) == 0 || !strings.HasPrefix(args[0], "-") && len(args) > 1 {
		return false
	}
	switch args[0] {
	case "-V=full", "-flags":
		return true
	}
	return strings.HasSuffix(args[len(args)-1], ".cfg") &&
		(len(args) == 1 || strings.HasPrefix(args[0], "-"))
}

// cmdVet runs Asuka's analyzers (package vet) as go vet runs its own:
// go vet's flags and the analyzers' (-reads=false turns that one off)
// first, then the packages, ./... by default.
func cmdVet(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") }) {
		args = append(args, "./...")
	}
	c := exec.Command("go", append([]string{"vet", "-vettool=" + exe}, args...)...)
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode()) // go vet said why
		}
		return err
	}
	return nil
}

// A finding is one of the analyzers' reports.
type finding struct {
	Posn    string `json:"posn"`
	Message string `json:"message"`
}

// vetApp runs the analyzers on an app's packages and returns what they
// report, as "file:line:col: message (analyzer)" with the file relative
// to the app. A package that doesn't compile reports nothing: the build
// says why.
func vetApp(dir string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	c := exec.Command("go", "vet", "-vettool="+exe, "-json", "./...")
	c.Dir = dir
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	c.Run() // a failure is a build's error, which the build reports
	return parseVet(&out, dir), nil
}

// parseVet reads go vet -json's output: per package, a "# path" line,
// then an object of the analyzers' findings (or errors), by package and
// analyzer.
func parseVet(r io.Reader, dir string) []string {
	var found []string
	data, _ := io.ReadAll(r)
	for len(data) > 0 {
		i := bytes.IndexByte(data, '{')
		if i < 0 {
			break
		}
		data = data[i:]
		var pkgs map[string]map[string]json.RawMessage
		d := json.NewDecoder(bytes.NewReader(data))
		if err := d.Decode(&pkgs); err != nil {
			data = data[1:]
			continue
		}
		data = data[d.InputOffset():]
		for _, analyzers := range pkgs {
			for name, raw := range analyzers {
				var fs []finding
				if json.Unmarshal(raw, &fs) != nil {
					continue // {"error": ...}: it didn't compile
				}
				for _, f := range fs {
					posn := f.Posn
					if rel, err := filepath.Rel(dir, posn); err == nil && !strings.HasPrefix(rel, "..") {
						posn = rel
					}
					found = append(found, fmt.Sprintf("%s: %s (%s)", posn, f.Message, name))
				}
			}
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// vetWarnings prints what vetApp found, as warnings: they don't stop a
// build.
func vetWarnings(w io.Writer, found []string) {
	if len(found) == 0 {
		return
	}
	fmt.Fprintf(w, "asuka vet: %d %s:\n", len(found), plural(len(found), "warning", "warnings"))
	for _, f := range found {
		fmt.Fprintln(w, "  "+f)
	}
}

// vetBuild runs the analyzers before a release build and prints what
// they found.
func vetBuild(dir string, on bool) {
	if !on {
		return
	}
	found, err := vetApp(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "asuka vet:", err)
		return
	}
	vetWarnings(os.Stderr, found)
}

// devVet runs the analyzers in the background while asuka dev runs: once
// the app is loaded and again on each change, beside the build. It prints
// what's new since the last run.
var devVet struct {
	sync.Mutex
	on             bool   // asuka dev, unless -vet=false
	dir            string // the app's
	running, again bool
	last           []string
}

// startDevVet runs the analyzers on the app in dir, if asuka dev has them
// on.
func startDevVet(dir string) {
	devVet.Lock()
	on := devVet.on
	devVet.dir = dir
	devVet.Unlock()
	if on {
		vetAgain()
	}
}

// vetAgain runs the analyzers again, once the run in progress is done.
func vetAgain() {
	devVet.Lock()
	defer devVet.Unlock()
	if !devVet.on || devVet.dir == "" {
		return
	}
	if devVet.running {
		devVet.again = true
		return
	}
	devVet.running = true
	go func() {
		for {
			devVet.Lock()
			dir := devVet.dir
			devVet.Unlock()
			found, err := vetApp(dir)
			devVet.Lock()
			if err != nil {
				fmt.Fprintln(os.Stderr, "asuka vet:", err)
			} else {
				var fresh []string
				for _, f := range found {
					if !slices.Contains(devVet.last, f) {
						fresh = append(fresh, f)
					}
				}
				devVet.last = found
				vetWarnings(os.Stderr, fresh)
			}
			if !devVet.again {
				devVet.running = false
				devVet.Unlock()
				return
			}
			devVet.again = false
			devVet.Unlock()
		}
	}()
}
