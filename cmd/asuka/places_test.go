package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// keepApp is a small app, as asuka dev reloads it: its signals are made
// in a closure, kept in STATE as it quits, and given back as it starts.
// SET writes them first. %s is code put before them.
const keepApp = `package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/egoist/asuka/signal"
)
%s
func Screen() (count *signal.Signal[int], name *signal.Signal[string]) {
	body := func() {
%s
		count = signal.New(0)
		name = signal.New("")
	}
	body()
	return
}

func main() {
	saved := map[string][]byte{}
	if b, err := os.ReadFile(os.Getenv("STATE")); err == nil {
		json.Unmarshal(b, &saved)
	}
	signal.KeepState(saved)
	var count *signal.Signal[int]
	var name *signal.Signal[string]
	signal.Root(func(func()) { count, name = Screen() })
	if os.Getenv("SET") != "" {
		count.Set(7)
		name.Set("hi")
	}
	fmt.Printf("%d %q\n", count.Peek(), name.Peek())
	b, _ := json.Marshal(signal.KeptState())
	os.WriteFile(os.Getenv("STATE"), b, 0o644)
}
`

// A reload keeps a signal's value through edits that move it in Go's
// naming: a closure added before the one that makes it (Go numbers
// closures in order), a signal of its type added before it.
func TestKeptStateSurvivesEditsAroundIt(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an app")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gomod := "module example.com/keep\n\ngo 1.26\n\nrequire github.com/egoist/asuka v0.0.0\n\nreplace github.com/egoist/asuka => " + repo + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)
	if sum, err := os.ReadFile(filepath.Join(repo, "go.sum")); err == nil {
		os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	}
	t.Setenv("GOFLAGS", "-mod=mod") // go list's too: the module's requirements are the replaced Asuka's
	app := &goApp{dir: dir, moduleDir: dir}
	places := newDevPlaces(app, "", "")
	state := filepath.Join(dir, "state.json")
	run := func(top, before string, env ...string) string {
		t.Helper()
		src := strings.Replace(strings.Replace(keepApp, "%s", top, 1), "%s", before, 1)
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		ov, err := devOverlay(app, t.TempDir(), false, places)
		if err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(t.TempDir(), "keep")
		c := exec.Command("go", "build", "-mod=mod", "-overlay", ov, "-o", bin, ".")
		c.Dir = dir
		c.Env = goEnv(dir, "CGO_ENABLED=0")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		c = exec.Command(bin)
		c.Env = append(append(os.Environ(), "STATE="+state), env...)
		out, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run("", "", "SET=1"); got != `7 "hi"` {
		t.Fatal(got)
	}
	// A handler before it, and an int signal before the int it keeps.
	before := "\t\tonTap := func() {}\n\t\t_ = onTap\n\t\tother := signal.New(5)\n\t\t_ = other\n"
	if got := run("", before); got != `7 "hi"` {
		t.Fatalf("after an edit before them: %s", got)
	}
	// A closure at the package's level too (numbered under init).
	if got := run("\nvar hook = func() {}\n", before); got != `7 "hi"` {
		t.Fatalf("after a package's closure: %s", got)
	}
	// A file that says //asuka:reset starts over when edited.
	if got := run("\n//asuka:reset\n", before); got != `0 ""` {
		t.Fatalf("a file that resets: %s", got)
	}
}

// The table names closures by what they are in their function, and
// signals by what they're made for.
func TestPlacesTable(t *testing.T) {
	src := `package main

import sig "github.com/egoist/asuka/signal"

type T struct{ n *sig.Signal[int] }

func (t *T) Init() {
	t.n = sig.New(0)
	go func() {}()
	run(func() {
		x, y := sig.New(1), sig.NewFunc[string]("", nil)
		_, _ = x, y
	}, func() {})
	_ = T{n: sig.New(2)}
	run(func() {})
}

func run(fs ...func()) {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte(src), 0o644)
	d := &devPlaces{parsed: map[string]parsedFile{}}
	f, ok := d.parse(path)
	if !ok {
		t.Fatal("not parsed")
	}
	var b strings.Builder
	writePlaces(&b, placesPkg{path: "example.com/x", name: "main", dir: dir}, []parsedFile{f}, []string{path})
	want := "F\t" + filepath.ToSlash(path) + `
D	main.init
D	main.(*T).Init
S	8	8	t.n
L	9	9	1	1	main.(*T).Init>go
L	10	13	1	2	main.(*T).Init>run#0
S	11	11	x
S	11	11	y
L	13	13	1	3	main.(*T).Init>run#1
S	14	14	n
L	15	15	1	4	main.(*T).Init>run#0~2
D	main.run
`
	if got := b.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
