package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"text/template"

	tpl "github.com/egoist/ryui/template"
)

// ryuiCheckout returns the RyUI source tree this CLI was built from, when
// it runs from a checkout (go run ./cmd/ryui, or go install from one).
func ryuiCheckout() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil && bytes.Contains(data, []byte("module github.com/egoist/ryui\n")) {
		return root
	}
	return ""
}

func cmdNew(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ryui new <dir>")
	}
	dir := args[0]
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	name := filepath.Base(abs)
	data := struct{ Name, Module, ID, Version, Replace string }{
		Name: name, Module: strings.ToLower(name),
		ID: "com.example." + strings.Trim(strings.ToLower(idPart.ReplaceAllString(name, "")), "-"),
	}
	// The RyUI the project uses: this checkout, or the version this CLI is
	// (go install github.com/egoist/ryui/cmd/ryui@v0.1.0), else the latest.
	if c := ryuiCheckout(); c != "" {
		data.Replace, data.Version = c, "v0.0.0"
	} else if bi, ok := debug.ReadBuildInfo(); ok && strings.HasPrefix(bi.Main.Version, "v") {
		data.Version = bi.Main.Version
	}
	err = fs.WalkDir(tpl.Files, "files", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, _ := tpl.Files.ReadFile(path)
		rel := strings.TrimSuffix(strings.TrimPrefix(path, "files/"), ".tmpl")
		if rel == "gitignore" {
			rel = ".gitignore"
		}
		t, err := template.New(rel).Parse(string(src))
		if err != nil {
			return err
		}
		var out bytes.Buffer
		if err := t.Execute(&out, data); err != nil {
			return err
		}
		dst := filepath.Join(abs, rel)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, out.Bytes(), 0o644)
	})
	if err != nil {
		return err
	}
	// go.sum (and, with no version, the latest RyUI).
	steps := [][]string{{"mod", "tidy"}}
	if data.Version == "" {
		steps = [][]string{{"get", "github.com/egoist/ryui@latest"}, {"mod", "tidy"}}
	}
	for _, s := range steps {
		c := exec.Command("go", s...)
		c.Dir, c.Stdout, c.Stderr = abs, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("go %s: %v", strings.Join(s, " "), err)
		}
	}
	fmt.Printf("ryui: made %s\n\n  cd %s\n  ryui dev -ios\n", dir, dir)
	return nil
}
