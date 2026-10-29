package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	tpl "github.com/egoist/nui/template"
)

// nuiCheckout returns the nui source tree this CLI was built from, when
// it runs from a checkout (go run ./cmd/nui, or go install from one).
func nuiCheckout() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil && bytes.Contains(data, []byte("module github.com/egoist/nui\n")) {
		return root
	}
	return ""
}

func cmdNew(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: nui new <dir>")
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
		Name: name, Module: strings.ToLower(name), Version: "v0.0.0",
		ID: "com.example." + strings.Trim(strings.ToLower(idPart.ReplaceAllString(name, "")), "-"),
	}
	if c := nuiCheckout(); c != "" {
		data.Replace = c
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
	if data.Replace == "" {
		fmt.Println("nui: note: nui isn't published yet; add a replace directive to go.mod pointing at a nui checkout")
	}
	fmt.Printf("nui: made %s\n\n  cd %s\n  nui dev -ios\n", dir, dir)
	return nil
}
