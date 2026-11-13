package nativegen

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write the kitchen package's generated files")

// The kitchen package (testdata/kitchen) declares every kind of value,
// a view, errors, an async method and objects: its generated files, for
// Go and each project it has (Swift, Android, web), are what's
// committed (-update writes them), and its Go compiles on each platform.
func TestKitchen(t *testing.T) {
	p, err := Load("testdata/kitchen")
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if _, err := Write(p); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := Stale(p)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := Files(p)
	for rel, b := range files {
		old, _ := os.ReadFile(filepath.Join(p.Dir, rel))
		if string(old) != string(b) {
			stale = append(stale, rel)
		}
	}
	if len(stale) > 0 {
		t.Fatalf("stale (go test ./internal/nativegen -update): %v", stale)
	}
	if len(files) != 4 {
		t.Errorf("%d files, want Go's, Swift's, Kotlin's and JavaScript's", len(files))
	}
	for _, goos := range []string{"darwin", "windows", "linux", "js"} {
		c := exec.Command("go", "vet", "./testdata/kitchen")
		c.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		if goos == "js" {
			c.Env = append(c.Env, "GOARCH=wasm")
		}
		if out, err := c.CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", goos, err, out)
		}
	}
}

// What can't cross says where it is and why.
func TestLoadErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"//asuka:native\ntype X struct{}", "is for an interface"},
		{"//asuka:native\ntype x interface{ M() }", "is exported"},
		{"//asuka:native\ntype X interface{ M(c chan int) }", "can't cross"},
		{"//asuka:native\ntype X interface{ M(m map[int]string) }", "a map's keys are strings"},
		{"//asuka:native\ntype X interface{ M() (int, int) }", "second result is an error"},
		{"//asuka:native\ntype X interface{ M(s string, ctx context.Context) }", "is the first parameter"},
		{"//asuka:native\ntype X interface{ M() func() }", "a parameter only"},
		{"//asuka:native\ntype X interface{ M(s ...string) }", "isn't variadic"},
		{"//asuka:native\ntype X interface{ M(o []X) }", "not in a slice"},
		{"type S struct{ a int }\n//asuka:native\ntype X interface{ M(s S) }", "exported fields only"},
		{"//asuka:native\ntype X interface{ M(t time.Month) }", "a type of another package"},
		{"type F float64\n//asuka:native\ntype X interface{ M(f F) }", "over a string or an integer"},
		{"//asuka:view\ntype Dial struct{ A int }", "named <View>Props"},
		{"//asuka:view\ntype DialProps struct{ On func() int }", "an event returns nothing"},
		{"var ErrX = native.NewError(fmt.Sprint(1))\n//asuka:native\ntype X interface{ M() }", "a constant string"},
	} {
		// A package of this module's (go list resolves its imports).
		dir, err := os.MkdirTemp("testdata", "bad")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		src := "package x\n\nimport (\n\t\"context\"\n\t\"fmt\"\n\t\"time\"\n\n\t\"github.com/egoist/asuka/native\"\n)\n\nvar _ = context.Background\nvar _ = fmt.Sprint\nvar _ time.Time\nvar _ = native.NewError\n\n" + c.src + "\n"
		os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644)
		_, err = Load(dir)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "x.go:") {
			t.Errorf("%s:\n  %v, want %q at its line", c.src, err, c.want)
		}
	}
}

// Names as each language writes them.
func TestNames(t *testing.T) {
	for in, want := range map[string]string{"Level": "level", "ID": "id", "URLString": "urlString", "HTTPServer": "httpServer", "X": "x", "isOn": "isOn"} {
		if got := lowerCamel(in); got != want {
			t.Errorf("lowerCamel(%s) = %s, want %s", in, got, want)
		}
	}
	if got := KotlinPackage("github.com/acme/go-battery/native"); got != "com.github.acme.go_battery.native_" {
		t.Errorf("KotlinPackage: %s", got)
	}
	if got := SwiftClass("example.com/a-b/c"); got != "AsukaModule_example_com_a_b_c" {
		t.Errorf("SwiftClass: %s", got)
	}
	if got := upperSnake("LowPower"); got != "LOW_POWER" {
		t.Errorf("upperSnake: %s", got)
	}
}
