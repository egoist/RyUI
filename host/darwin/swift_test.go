//go:build darwin && !ios

package darwin

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/nativegen"
	"github.com/egoist/asuka/internal/nativegen/testdata/kitchen"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// buildKitchenSwift builds the kitchen package's Swift (nativegen's
// testdata) as the CLI does for an app's packages: Asuka's Swift (module
// Asuka) and the package's (its generated Swift and its own), in one
// library.
func buildKitchenSwift(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode command line tools")
	}
	pkg := "../../internal/nativegen/testdata/kitchen"
	p, err := nativegen.Load(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if stale, err := nativegen.Stale(p); err != nil || len(stale) > 0 {
		t.Fatalf("the kitchen package's generated files are stale (go test ./internal/nativegen -update): %v %v", stale, err)
	}
	dir := t.TempDir()
	// Its Swift: its own and what asuka native generated.
	srcs, _ := filepath.Glob(filepath.Join(pkg, nativegen.SwiftDir, "*.swift"))
	arch := map[string]string{"arm64": "arm64", "amd64": "x86_64"}[runtime.GOARCH]
	target := arch + "-apple-macos11"
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("xcrun", append([]string{"swiftc", "-swift-version", "5", "-target", target, "-parse-as-library", "-Onone"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("swiftc: %v\n%s", err, out)
		}
	}
	src := "../../platform/macos/Sources/AsukaMac/"
	run("-module-name", "Asuka", "-emit-module", "-emit-module-path", filepath.Join(dir, "Asuka.swiftmodule"), "-wmo", "-c",
		src+"AsukaMacRuntime.swift", src+"NativeModules.swift", "-o", filepath.Join(dir, "Asuka.o"))
	run(append([]string{"-module-name", "KitchenNative", "-I", dir, "-wmo", "-c", "-o", filepath.Join(dir, "KitchenNative.o")}, srcs...)...)
	lib := filepath.Join(dir, swiftLib)
	run("-emit-library", filepath.Join(dir, "Asuka.o"), filepath.Join(dir, "KitchenNative.o"), "-o", lib)
	return lib
}

// A package's Swift, built and loaded as the host loads an app's: its
// typed module's calls from Go, on any goroutine, with every kind of
// value; its errors; an async method, its progress and its cancelation;
// a question callback; a main-thread object, closed; and its typed view,
// made, set, measured, sending events and destroyed.
func TestSwiftNatives(t *testing.T) {
	lib := buildKitchenSwift(t)
	onMain(func() {
		if err := loadSwiftNatives(lib); err != nil {
			fatal(t, err)
		}
	})
	value := signal.New(0.25)
	shown := signal.New(true)
	var changed []float64
	var picked []string
	var progress []float64
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Show(shown, func() ui.Node {
				return kitchen.Dial(kitchen.DialProps{
					Value:   value,
					Label:   "Heat",
					Items:   signal.Static([]kitchen.Item{{Name: "a"}, {Name: "b", Tags: []string{"t"}}}),
					Changed: func(v float64) { changed = append(changed, v) },
					Picked:  func(it kitchen.Item, i int) { picked = append(picked, it.Name, string(rune('0'+i))) },
				})
			}))
	})

	k, err := kitchen.NewKitchen()
	if err != nil {
		t.Fatal(err)
	}
	if n := k.Name(); n != "home" {
		t.Errorf("Name %q", n)
	}
	items, err := k.Items("x")
	best := time.Unix(1_700_000_000, 250_000_000)
	want := kitchen.Item{Name: "x", Weight: 1.5, Unit: kitchen.UnitGram, Tags: []string{"a", "b"}, Notes: map[string]string{"k": "v"},
		Best: &best, Next: &kitchen.Item{Name: "inner"}, Data: []byte{1, 2, 3}, Took: 2500 * time.Millisecond, ID: ^uint64(0), Count: -3, Ratio: 0.5, Fresh: true}
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0], want) {
		t.Errorf("Items: %+v, %v\nwant %+v", items, err, want)
	}
	if err := k.Add(kitchen.Item{}); !errors.Is(err, kitchen.ErrBusy) {
		t.Errorf("Add: %v, want ErrBusy", err)
	}
	if err := k.Add(kitchen.Item{Name: "y"}); err != nil {
		t.Errorf("Add: %v", err)
	}
	if l := k.Level(); l != kitchen.LevelHigh {
		t.Errorf("Level %v", l)
	}
	if c := k.Counts(); !reflect.DeepEqual(c, map[string]int64{"a": 1, "b": -2}) {
		t.Errorf("Counts %v", c)
	}
	at := time.Unix(100, 0)
	if w, err := k.When(at, time.Minute); err != nil || w == nil || !w.Equal(at.Add(time.Minute)) {
		t.Errorf("When: %v, %v", w, err)
	}
	if w, err := k.When(at, 0); err != nil || w != nil {
		t.Errorf("When (none): %v, %v", w, err)
	}

	// A question: Go's func answers on the Swift's thread.
	var asked string
	if ok, err := k.Ask(func(q string) bool { asked = q; return true }); !ok || err != nil || asked != "ok?" {
		t.Errorf("Ask: %v, %v, %q", ok, err, asked)
	}

	// Async: the Swift's task answers from its thread; its progress is
	// posted to the main thread.
	item, err := k.Cook(context.Background(), "soup", func(done float64) { progress = append(progress, done) })
	if err != nil || item.Name != "soup done" {
		t.Errorf("Cook: %+v, %v", item, err)
	}
	onMain(func() { h.runPosted() })
	if !slices.Equal(progress, []float64{0.5, 1}) {
		t.Errorf("progress %v", progress)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := k.Cook(ctx, "slow", nil); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Errorf("a canceled Cook: %v after %v", err, time.Since(start))
	}

	// An error the package doesn't declare: its domain, code and message.
	var ne *native.Error
	if _, err := k.Open("none"); !errors.As(err, &ne) || ne.Domain != "Oven" || ne.Code != 7 || ne.Message != "no oven" {
		t.Errorf("Open(none): %#v", err)
	}
	var oven kitchen.Oven
	onMain(func() {
		// Its Oven is the main thread's (//asuka:mainthread): made there.
		if oven, err = k.Open("main"); err != nil {
			fatal(t, err)
		}
		if err := oven.Heat(180, kitchen.UnitGram); err != nil {
			fatal(t, err)
		}
		if err := oven.Heat(1, kitchen.UnitOunce); !errors.Is(err, errors.ErrUnsupported) {
			fatalf(t, "Heat in ounces: %v", err)
		}
		if err := oven.Close(); err != nil {
			fatal(t, err)
		}
		if err := oven.Heat(1, kitchen.UnitGram); !errors.Is(err, native.ErrClosed) {
			fatalf(t, "after Close: %v", err)
		}
	})

	// The view.
	onMain(func() {
		n := find(h, ops.KindNative)[0]
		if f := MsgRect(n.view, Sel("frame")); f.Size.Width != 160 || f.Size.Height != 24 {
			fatalf(t, "the view's frame %+v, want 160x24 (its sizeThatFits)", f.Size)
		}
		slider := Send(Send(n.view, "subviews"), "firstObject")
		if GoString(Send(Send(slider, "className"), "description")) != "NSSlider" {
			fatal(t, "the Swift view isn't in its node's view")
		}
		if v := MsgFloat(slider, Sel("doubleValue")); v != 0.25 {
			fatalf(t, "value %v", v)
		}
		inCore(h, func() { value.Set(0.75) })
		if v := MsgFloat(slider, Sel("doubleValue")); v != 0.75 {
			fatalf(t, "value after a change %v", v)
		}
		if !slices.Equal(changed, []float64{0.5, 1.5}) {
			fatalf(t, "Changed %v", changed)
		}
		if !slices.Equal(picked, []string{"b", "1"}) {
			fatalf(t, "Picked %v", picked)
		}
		inCore(h, func() { shown.Set(false) })
		if len(swift.contexts) != 0 {
			fatalf(t, "contexts kept: %d", len(swift.contexts))
		}
	})
}

// A typed call into the Swift and back (purego both ways), its answer a
// string: the module's steady state.
func BenchmarkSwiftCall(b *testing.B) {
	t := &testing.T{}
	lib := buildKitchenSwift(t)
	onMain(func() {
		if err := loadSwiftNatives(lib); err != nil {
			b.Fatal(err)
		}
	})
	k, err := kitchen.NewKitchen()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if k.Name() != "home" {
			b.Fatal("name")
		}
	}
}
