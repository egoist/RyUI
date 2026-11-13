//go:build darwin && !ios

package darwin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// testSwift is an app's macos/*.swift: an NSSlider view, and a module
// that answers now, later from another thread, with an error, and that
// makes the view send an event.
const testSwift = `
import AppKit

@objc(AsukaAppNatives) final class AppNatives: NSObject, AsukaNativeModule {
    @MainActor static func register() {
        AsukaRuntime.shared.registerNative("TestSwiftSlider") { ctx in Slider(ctx) }
        AsukaRuntime.shared.registerModule("testSwift", [
            "echo": { args, reply in reply(.success(Data("echo:".utf8) + args)) },
            "later": { args, reply in
                DispatchQueue.global().async { reply(.success(args)); reply(.success(Data("twice".utf8))) }
            },
            "fail": { _, reply in reply(.failure(Oops())) },
            "fire": { _, reply in
                Slider.last?.ctx.emit("changed", ["value": .float(0.5), "label": .string("x"), "n": .int(3), "on": .bool(true)])
                reply(.success(Data()))
            },
            "state": { _, reply in
                reply(.success(Data("\(Slider.last?.sets.joined(separator: ",") ?? "")|\(Slider.destroyed)".utf8)))
            },
        ])
    }
}

struct Oops: LocalizedError { var errorDescription: String? { "oops from Swift" } }

@MainActor final class Slider: AsukaNativeView {
    static var last: Slider?
    static var destroyed = 0
    let ctx: AsukaNativeContext
    let slider = NSSlider()
    var sets: [String] = []
    var view: NSView { slider }
    init(_ ctx: AsukaNativeContext) { self.ctx = ctx; Slider.last = self }
    func set(_ name: String, _ value: AsukaNativeValue) {
        sets.append(name)
        if name == "value" { slider.doubleValue = value.float }
    }
    func sizeThatFits(_ size: CGSize) -> CGSize { CGSize(width: 160, height: 24) }
    func destroy() { Slider.destroyed += 1 }
}
`

// The app's Swift, compiled as the CLI compiles it, loaded by the host:
// its view made, set, measured, sending events and destroyed; its
// module's methods answering, once each, on the main thread.
func TestSwiftNatives(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode command line tools")
	}
	dir := t.TempDir()
	app := filepath.Join(dir, "Natives.swift")
	if err := os.WriteFile(app, []byte(testSwift), 0o644); err != nil {
		t.Fatal(err)
	}
	arch := map[string]string{"arm64": "arm64", "amd64": "x86_64"}[runtime.GOARCH]
	lib := filepath.Join(dir, swiftLib)
	out, err := exec.Command("xcrun", "swiftc", "-swift-version", "5", "-module-name", "AsukaApp",
		"-target", arch+"-apple-macos11", "-emit-library", "-Onone",
		"../../platform/macos/Sources/AsukaMac/AsukaMacRuntime.swift", app, "-o", lib).CombinedOutput()
	if err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	onMain(func() {
		if err := loadSwiftNatives(lib); err != nil {
			fatal(t, err)
		}
	})
	value := signal.New(0.25)
	shown := signal.New(true)
	var got []map[string]any
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Show(shown, func() ui.Node {
				return ui.Native("TestSwiftSlider", ui.NativeProps{
					Props: map[string]any{"value": signal.Accessor[float64](value), "label": "Volume"},
					On: map[string]func(ui.NativeEvent){"changed": func(e ui.NativeEvent) {
						got = append(got, e.Values())
					}},
				})
			}))
	})
	onMain(func() {
		n := find(h, ops.KindNative)[0]
		if f := MsgRect(n.view, Sel("frame")); f.Size.Width != 160 || f.Size.Height != 24 {
			fatalf(t, "the native view's frame %+v, want 160x24 (its sizeThatFits)", f.Size)
		}
		slider := ID(Send(n.view, "subviews"))
		slider = Send(slider, "firstObject")
		if GoString(Send(Send(slider, "className"), "description")) != "NSSlider" {
			fatal(t, "the Swift view isn't in its node's view")
		}
		if MsgRect(slider, Sel("frame")).Size.Width != 160 {
			fatal(t, "the Swift view doesn't fill its node's")
		}
		if v := MsgFloat(slider, Sel("doubleValue")); v != 0.25 {
			fatalf(t, "value %v", v)
		}
		inCore(h, func() { value.Set(0.75) })
		if v := MsgFloat(slider, Sel("doubleValue")); v != 0.75 {
			fatalf(t, "value after a change %v", v)
		}

		if b, err := await(h, "testSwift", "echo", []byte("hi")); err != nil || string(b) != "echo:hi" {
			fatalf(t, "echo: %q, %v", b, err)
		}
		if b, err := await(h, "testSwift", "later", []byte("bye")); err != nil || string(b) != "bye" {
			fatalf(t, "later: %q, %v", b, err)
		}
		if _, err := await(h, "testSwift", "fail", nil); err == nil || err.Error() != "oops from Swift" {
			fatalf(t, "fail: %v", err)
		}
		if _, err := await(h, "testSwift", "fire", nil); err != nil {
			fatal(t, err)
		}
		if len(got) != 1 || got[0]["value"] != 0.5 || got[0]["label"] != "x" || got[0]["n"] != int64(3) || got[0]["on"] != true {
			fatalf(t, "events %v", got)
		}
		b, _ := await(h, "testSwift", "state", nil)
		if string(b) != "label,value,value|0" && string(b) != "value,label,value|0" {
			fatalf(t, "props set %q", b)
		}
		inCore(h, func() { shown.Set(false) })
		if b, _ := await(h, "testSwift", "state", nil); string(b[len(b)-2:]) != "|1" {
			fatalf(t, "not destroyed with its node: %q", b)
		}
		if len(swift.contexts) != 0 {
			fatalf(t, "contexts kept: %d", len(swift.contexts))
		}
	})
}
