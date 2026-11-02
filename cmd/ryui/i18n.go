package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/ryui/i18n"
	"github.com/egoist/ryui/internal/i18ngen"
	"github.com/egoist/ryui/internal/l10n"
)

// i18nConfig is ryui.json's "i18n": where the app's catalogs are. An app
// with a locales directory of catalogs needs none of it.
//
//	"i18n": {"dir": "locales", "source": "en"}
type i18nConfig struct {
	// Dir holds a catalog per language, named after its BCP 47 tag: en.json,
	// de.json, zh-Hans.json (default: locales).
	Dir string `json:"dir"`
	// Source is the language the app is written in: its catalog has every
	// message, and the others fall back to it (default: en, or the only
	// catalog there is).
	Source string `json:"source"`
}

// devLang and devDirection are ryui dev's -lang and -dir: built into the app
// for this run.
var devLang, devDirection string

// setDevLocale checks ryui dev's -lang and -dir.
func setDevLocale(lang, dir string) error {
	if lang != "" {
		if i18n.Normalize(lang) == "" {
			return fmt.Errorf("-lang %q is not a language tag (try de, zh-Hans, or the pseudo locales en-XA and ar-XB)", lang)
		}
		devLang = i18n.Normalize(lang)
	}
	if dir != "" && dir != "rtl" && dir != "ltr" {
		return fmt.Errorf("-dir %q: want rtl or ltr", dir)
	}
	devDirection = dir
	return nil
}

// useI18nDefaults finds the app's catalogs when ryui.json doesn't say where
// they are, and fills in what it leaves out.
func (c *appConfig) useI18nDefaults(dir string) {
	c.dir = dir
	if c.I18n == nil {
		if m, _ := filepath.Glob(filepath.Join(dir, "locales", "*.json")); len(m) == 0 {
			return
		}
		c.I18n = &i18nConfig{}
	}
	if c.I18n.Dir == "" {
		c.I18n.Dir = "locales"
	}
	if c.I18n.Source == "" {
		c.I18n.Source = "en"
		m, _ := filepath.Glob(filepath.Join(c.i18nDir(), "*.json"))
		if len(m) == 1 {
			c.I18n.Source = strings.TrimSuffix(filepath.Base(m[0]), ".json")
		}
	}
}

func (c *appConfig) i18nDir() string { return filepath.Join(c.dir, c.I18n.Dir) }

// framework is RyUI's own catalog, whose messages the keys "ryui.<key>" of
// apps change.
var framework = sync.OnceValues(func() (*i18ngen.Catalog, error) {
	cat, diags, err := i18ngen.Load(l10n.FS(), i18ngen.Options{Dir: "ryui", Source: "en", Namespace: l10n.Namespace})
	if err == nil && diags.Errors() > 0 {
		err = diags.Err()
	}
	return cat, err
})

// loadCatalogs reads and checks the app's catalogs.
func (c *appConfig) loadCatalogs() (*i18ngen.Catalog, i18ngen.Diagnostics, error) {
	dir := c.i18nDir()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("i18n: %s: no such directory", c.I18n.Dir)
	}
	lib, err := framework()
	if err != nil {
		return nil, nil, err
	}
	return i18ngen.Load(os.DirFS(dir), i18ngen.Options{
		Dir: filepath.ToSlash(c.I18n.Dir), Source: c.I18n.Source,
		Libraries: map[string]*i18ngen.Catalog{l10n.Namespace: lib},
	})
}

// generateCatalogs checks the app's catalogs and writes their functions,
// only if they differ: a rebuild's watcher sees no change otherwise. It
// reports problems on stderr; errors in the catalogs are the error it
// returns, which names how many.
func (c *appConfig) generateCatalogs() error {
	if c.I18n == nil {
		return nil
	}
	cat, diags, err := c.loadCatalogs()
	if err != nil {
		return err
	}
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, "ryui:", d)
	}
	if n := diags.Errors(); n > 0 {
		return fmt.Errorf("%d %s in the catalogs in %s", n, plural(n, "error", "errors"), c.I18n.Dir)
	}
	dir := c.i18nDir()
	pkg, err := i18ngen.PackageName(dir)
	if err != nil {
		return err
	}
	src, err := i18ngen.Go(cat, pkg)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, i18ngen.GoFile)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, src) {
		return nil
	}
	return os.WriteFile(path, src, 0o644)
}

func plural(n int, one, other string) string {
	if n == 1 {
		return one
	}
	return other
}

// warnDevLang says when -lang asks for a language the app has no catalog of,
// which then shows its own.
func warnDevLang(c *appConfig) {
	if devLang == "" || i18n.IsPseudo(devLang) {
		return
	}
	langs := []string{}
	if c.I18n != nil {
		if cat, _, err := c.loadCatalogs(); err == nil && cat != nil {
			langs = cat.Languages
		}
	}
	if i18n.Match([]string{devLang}, langs) == "" {
		fmt.Fprintf(os.Stderr, "ryui: the app isn't in %s, so -lang %s shows its own language; try -dir rtl to see it right to left, or the pseudo locale -lang ar-XB\n", devLang, devLang)
	}
}

// warnCatalogsUnused says when the app doesn't import its catalogs, whose
// languages it then doesn't ship.
func warnCatalogsUnused(c *appConfig, deps []string) {
	if c.I18n == nil {
		return
	}
	out, err := exec.Command("go", "-C", c.i18nDir(), "list", ".").Output()
	if err != nil {
		return
	}
	if pkg := strings.TrimSpace(string(out)); !slices.Contains(deps, pkg) {
		fmt.Fprintf(os.Stderr, "ryui: the app doesn't import %s, so it shows only its source language: import it, with _ if its Go code says no message itself\n", pkg)
	}
}

// cmdGenerate is ryui generate.
func cmdGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: ryui generate [dir]\n\nWrites the typed functions of the app's catalogs (locales/catalog_gen.go), as ryui dev and\nryui build do. Commit them, so that go build and go test work without the CLI.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := configOf(fs.Arg(0))
	if err != nil {
		return err
	}
	return c.generateCatalogs()
}

func configOf(dir string) (*appConfig, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	c, err := loadConfig(abs)
	if err != nil {
		return nil, err
	}
	if c.I18n == nil {
		return nil, errors.New("the app has no catalogs: run ryui i18n init")
	}
	return c, nil
}

// cmdI18n is ryui i18n.
func cmdI18n(args []string) error {
	const usage = `usage:
  ryui i18n init [-source en] [dir]   start the app's catalogs: locales/en.json and its functions
  ryui i18n add <tag>... [-dir dir]   add languages, each with the source's messages to translate
  ryui i18n check [dir]               check the catalogs: errors exit 1, warnings don't
`
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("i18n: which subcommand?")
	}
	switch args[0] {
	case "init":
		return i18nInit(args[1:])
	case "add":
		return i18nAdd(args[1:])
	case "check":
		return i18nCheck(args[1:])
	case "-h", "-help", "--help", "help":
		fmt.Fprint(os.Stderr, usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("i18n: unknown subcommand %q", args[0])
}

func i18nCheck(args []string) error {
	fs := flag.NewFlagSet("i18n check", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := configOf(fs.Arg(0))
	if err != nil {
		return err
	}
	cat, diags, err := c.loadCatalogs()
	if err != nil {
		return err
	}
	for _, d := range diags {
		fmt.Println(d)
	}
	for _, w := range frozenMessages(c, c.dir) {
		fmt.Println(w)
	}
	if n := diags.Errors(); n > 0 {
		return fmt.Errorf("%d %s in the catalogs", n, plural(n, "error", "errors"))
	}
	fmt.Printf("%d %s in %s\n", len(cat.Keys), plural(len(cat.Keys), "message", "messages"), strings.Join(cat.Languages, ", "))
	return nil
}

func i18nInit(args []string) error {
	fs := flag.NewFlagSet("i18n init", flag.ContinueOnError)
	source := fs.String("source", "en", "the language the app is written in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tag := i18n.Normalize(*source)
	if tag == "" || i18n.IsPseudo(tag) {
		return fmt.Errorf("-source %q is not a language", *source)
	}
	dir := fs.Arg(0)
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	locales := filepath.Join(abs, "locales")
	file := filepath.Join(locales, tag+".json")
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s exists", file)
	}
	if err := os.MkdirAll(locales, 0o755); err != nil {
		return err
	}
	starter := fmt.Sprintf(`{
  "@@locale": %q,
  "appTitle": "Hello",
  "@appTitle": {"description": "What the app calls itself on its first screen."}
}
`, tag)
	if err := os.WriteFile(file, []byte(starter), 0o644); err != nil {
		return err
	}
	c, err := loadConfig(abs)
	if err != nil {
		return err
	}
	if c.I18n.Source != tag {
		return fmt.Errorf("the source language is %s, as ryui.json says", c.I18n.Source)
	}
	if err := c.generateCatalogs(); err != nil {
		return err
	}
	fmt.Printf("wrote %s and its functions: import \"<module>/locales\", and say ui.Text(locales.Live.AppTitle())\n", filepath.Join("locales", tag+".json"))
	return nil
}

func i18nAdd(args []string) error {
	fs := flag.NewFlagSet("i18n add", flag.ContinueOnError)
	dir := fs.String("dir", ".", "the app's directory")
	var tags []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		tags, args = append(tags, args[0]), args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	tags = append(tags, fs.Args()...)
	if len(tags) == 0 {
		return errors.New("i18n add: which languages? ryui i18n add de zh-Hans")
	}
	c, err := configOf(*dir)
	if err != nil {
		return err
	}
	cat, _, err := c.loadCatalogs()
	if err != nil {
		return err
	}
	src := cat.Files[cat.Source]
	for _, t := range tags {
		tag := i18n.Normalize(t)
		if tag == "" || i18n.IsPseudo(tag) {
			return fmt.Errorf("%q is not a language tag (try de or zh-Hans; pseudo locales need no catalog)", t)
		}
		file := filepath.Join(c.i18nDir(), tag+".json")
		if _, err := os.Stat(file); err == nil {
			return fmt.Errorf("%s exists", file)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "{\n  \"@@locale\": %q", tag)
		for _, k := range src.Keys {
			key, _ := json.Marshal(k)
			val, _ := json.Marshal(src.Messages[k])
			fmt.Fprintf(&b, ",\n  %s: %s", key, val)
		}
		b.WriteString("\n}\n")
		if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s with the source's %d messages to translate\n", filepath.Join(c.I18n.Dir, tag+".json"), len(src.Keys))
	}
	return c.generateCatalogs()
}

// catalogStrings returns what the catalogs say for packaging, by language
// then key, of the keys "ryui.<key>" of the app's catalogs. It's nil without
// catalogs. Packaging has checked them as it built.
func (c *appConfig) catalogStrings() (langs []string, source string, byLang map[string]map[string]string) {
	if c.I18n == nil {
		return nil, "", nil
	}
	cat, _, err := c.loadCatalogs()
	if err != nil || cat == nil {
		return nil, "", nil
	}
	byLang = map[string]map[string]string{}
	for _, tag := range cat.Languages {
		m := map[string]string{}
		for k, v := range cat.Files[tag].Messages {
			if name, ok := strings.CutPrefix(k, l10n.Namespace+"."); ok && i18ngen.IsPackagingKey(name) {
				m[name] = v
			}
		}
		byLang[tag] = m
	}
	return cat.Languages, cat.Source, byLang
}

// plistLocalizations adds Info.plist's languages to m: which languages the
// app is in, which makes UIKit and AppKit say their own words and lay out in
// them, Settings offer the app's language, and the App Store list them.
func (c *appConfig) plistLocalizations(m map[string]any) {
	langs, source, _ := c.catalogStrings()
	if len(langs) == 0 {
		return
	}
	m["CFBundleDevelopmentRegion"] = source
	m["CFBundleLocalizations"] = langs
}

// plistLocalizationKeys are plistLocalizations as XML, for the Info.plist of
// a development build, which is a template.
func (c *appConfig) plistLocalizationKeys() string {
	langs, source, _ := c.catalogStrings()
	if len(langs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<key>CFBundleDevelopmentRegion</key><string>%s</string>\n<key>CFBundleLocalizations</key><array>", html.EscapeString(source))
	for _, l := range langs {
		fmt.Fprintf(&b, "<string>%s</string>", html.EscapeString(l))
	}
	b.WriteString("</array>\n")
	return b.String()
}

// writeLprojs writes a <tag>.lproj per language into dir, a bundle's
// resources. InfoPlist.strings has the app's name and the purpose strings of
// protected resources in the language (ryui.appName,
// ryui.NSCameraUsageDescription...), and Localizable.strings the titles of
// its shortcuts and the names of its widgets, which their Swift looks up by
// their source language's. A language needs its lproj even when these say
// nothing: it's how iOS knows the bundle is in it.
func (c *appConfig) writeLprojs(dir string) error {
	langs, source, byLang := c.catalogStrings()
	for _, tag := range langs {
		lproj := filepath.Join(dir, tag+".lproj")
		if err := os.MkdirAll(lproj, 0o755); err != nil {
			return err
		}
		var info, loc strings.Builder
		keys := make([]string, 0, len(byLang[tag]))
		for k := range byLang[tag] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := byLang[tag][k]
			switch {
			case k == "appName":
				fmt.Fprintf(&info, "%s = %s;\n%s = %s;\n", stringsQuote("CFBundleDisplayName"), stringsQuote(v), stringsQuote("CFBundleName"), stringsQuote(v))
			case strings.HasPrefix(k, "NS"):
				fmt.Fprintf(&info, "%s = %s;\n", stringsQuote(k), stringsQuote(v))
			case k != "appDescription":
				if orig := c.packagingSource(k, byLang[source]); orig != "" {
					fmt.Fprintf(&loc, "%s = %s;\n", stringsQuote(orig), stringsQuote(v))
				}
			}
		}
		if err := os.WriteFile(filepath.Join(lproj, "InfoPlist.strings"), []byte(info.String()), 0o644); err != nil {
			return err
		}
		if loc.Len() > 0 {
			if err := os.WriteFile(filepath.Join(lproj, "Localizable.strings"), []byte(loc.String()), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// packagingSource returns what a packaging key says in ryui.json, which is
// what Swift looks up: a shortcut's title or a widget's name or description.
func (c *appConfig) packagingSource(key string, source map[string]string) string {
	if v, ok := strings.CutPrefix(key, "shortcut."); ok {
		for _, s := range c.Shortcuts {
			if s.ID == v {
				return s.Title
			}
		}
	}
	if v, ok := strings.CutPrefix(key, "widget."); ok {
		kind, field, _ := strings.Cut(v, ".")
		for _, w := range c.Widgets {
			if w.Kind == kind {
				if field == "description" {
					return w.Description
				}
				return w.Name
			}
		}
	}
	return source[key]
}

// stringsQuote quotes s for a .strings file.
func stringsQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// androidQualifier is the resource qualifier of a language: values-b+zh+Hant.
func androidQualifier(tag string) string { return "b+" + strings.ReplaceAll(tag, "-", "+") }

// androidHasLocales reports whether the app has catalogs, for the manifest's
// localeConfig.
func (c *appConfig) androidHasLocales() bool { return c != nil && c.I18n != nil }

// androidLabel is the app's android:label: its localized resource when any
// language names it, else its name. Android resource strings can't hold a
// bare @ or ?, which xmlText escapes.
func (c *appConfig) androidLabel() string {
	_, _, byLang := c.catalogStrings()
	for _, m := range byLang {
		if m["appName"] != "" {
			return "@string/ryui_app_name"
		}
	}
	return c.Name
}

// androidLocaleRes writes the languages' resources: the locale config that
// offers the app's language in Android 13's settings, the app's name in each,
// and its widgets' and shortcuts' names.
func (c *appConfig) androidLocaleRes(res string) (bool, error) {
	langs, _, byLang := c.catalogStrings()
	if len(langs) == 0 {
		return false, nil
	}
	var cfg strings.Builder
	cfg.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<locale-config xmlns:android=\"http://schemas.android.com/apk/res/android\">\n")
	for _, tag := range langs {
		fmt.Fprintf(&cfg, "    <locale android:name=%q />\n", tag)
	}
	cfg.WriteString("</locale-config>\n")
	if err := os.MkdirAll(filepath.Join(res, "xml"), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(res, "xml", "ryui_locales_config.xml"), []byte(cfg.String()), 0o644); err != nil {
		return false, err
	}
	write := func(qualifier string, strs map[string]string) error {
		if len(strs) == 0 {
			return nil
		}
		names := make([]string, 0, len(strs))
		for k := range strs {
			names = append(names, k)
		}
		sort.Strings(names)
		var b strings.Builder
		b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<resources>\n")
		for _, k := range names {
			fmt.Fprintf(&b, "    <string name=\"%s\">%s</string>\n", k, xmlText(strs[k]))
		}
		b.WriteString("</resources>\n")
		d := filepath.Join(res, qualifier)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(d, "ryui_i18n.xml"), []byte(b.String()), 0o644)
	}
	if c.androidLabel() != c.Name {
		if err := write("values", map[string]string{"ryui_app_name": c.Name}); err != nil {
			return false, err
		}
	}
	for _, tag := range langs {
		strs := map[string]string{}
		for k, v := range byLang[tag] {
			switch {
			case k == "appName":
				strs["ryui_app_name"] = v
			case strings.HasPrefix(k, "shortcut."):
				strs["ryui_shortcut_"+resName(strings.TrimPrefix(k, "shortcut."))] = v
			case strings.HasPrefix(k, "widget."):
				kind, field, ok := strings.Cut(strings.TrimPrefix(k, "widget."), ".")
				if ok && (field == "name" || field == "description") {
					strs["ryui_widget_"+resName(kind)+"_"+field] = v
				}
			}
		}
		if err := write("values-"+androidQualifier(tag), strs); err != nil {
			return false, err
		}
	}
	return true, nil
}

// warnFrozenMessages finds messages said once where they'd never follow the
// language: ui.StaticText(locales.Title()) and ui.Val(locales.Title()), which
// read natural and which locales.Live.Title() is for. It looks at the syntax
// alone, so that it runs before the app compiles.
func warnFrozenMessages(c *appConfig, moduleDir string) {
	for _, w := range frozenMessages(c, moduleDir) {
		fmt.Fprintln(os.Stderr, "ryui:", w)
	}
}

// frozenMessages returns a warning per message found so.
func frozenMessages(c *appConfig, moduleDir string) []string {
	if c.I18n == nil {
		return nil
	}
	pkg, err := i18ngen.PackageName(c.i18nDir())
	if err != nil {
		return nil
	}
	var out []string
	filepath.WalkDir(c.dir, func(path string, d iofs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); path != c.dir && (strings.HasPrefix(n, ".") || n == "testdata" || n == "build" || n == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == i18ngen.GoFile {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			outer, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (outer.Sel.Name != "StaticText" && outer.Sel.Name != "Val") {
				return true
			}
			if x, ok := outer.X.(*ast.Ident); !ok || x.Name != "ui" {
				return true
			}
			inner, ok := call.Args[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != pkg || sel.Sel.Name == "In" {
				return true
			}
			rel, err := filepath.Rel(moduleDir, path)
			if err != nil {
				rel = path
			}
			pos := fset.Position(call.Pos())
			out = append(out, fmt.Sprintf("%s:%d: ui.%s(%s.%s(...)) won't follow the app's language: use %s.Live.%s", rel, pos.Line, outer.Sel.Name, pkg, sel.Sel.Name, pkg, sel.Sel.Name))
			return true
		})
		return nil
	})
	return out
}
