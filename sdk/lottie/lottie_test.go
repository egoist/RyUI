package lottie

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/lottie/internal/lottieplayer"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func successJSON(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../examples/gallery/assets/success.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// usePlayer makes View use p for the test.
func usePlayer(t *testing.T, p func(lottieplayer.PlayerProps) ui.Node) {
	old := player
	player = p
	t.Cleanup(func() { player = old })
}

func mount(t *testing.T, root func() ui.Node) (*fake.Host, *ui.Root) {
	t.Helper()
	h := fake.New()
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	return h, r
}

// loaded waits for the animation to load (read off the main thread).
func loaded(t *testing.T, h *fake.Host, done func() bool) {
	t.Helper()
	for !done() {
		if !h.WaitPost(5 * time.Second) {
			t.Fatal("it didn't load")
		}
		h.RunPosted()
	}
}

var clock time.Duration

// frame dispatches the display's next frame, dt later.
func frame(h *fake.Host, dt time.Duration) {
	clock += dt
	h.Dispatch(ops.Event{ID: h.Root().ID, Kind: ops.EventFrame, Float: clock.Seconds()})
}

// The Go player (Windows, Linux): it loads, sizes itself as the
// animation, plays on the display's frames writing Progress, completes,
// seeks where the app writes Progress (animated inside ui.Animate), plays
// a marker, and shows its last frame with Reduce Motion.
func TestCanvasPlayer(t *testing.T) {
	usePlayer(t, canvasPlayer)
	progress := signal.New(0.0)
	playing := signal.New(true)
	marker := signal.New("")
	var info Info
	completed := 0
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart}, View(Props{
			Data:       successJSON(t),
			Playing:    playing,
			Progress:   progress,
			Marker:     marker,
			OnLoad:     func(i Info) { info = i },
			OnComplete: func() { completed++ },
		}))
	})
	loaded(t, h, func() bool { return info.FrameRate != 0 })
	if info.Width != 200 || info.End != 90 || len(info.Markers) != 3 || info.Seconds() != 3 {
		t.Fatalf("info %+v", info)
	}
	c := h.OfKind(ops.KindCanvas)[0]
	if c.Frame[2] != 200 || c.Frame[3] != 200 {
		t.Fatalf("its natural size: %v", c.Frame)
	}
	// It plays: a second is 30 frames, a third of it.
	frame(h, 0)
	frame(h, time.Second)
	if p := progress.Peek(); math.Abs(p-1.0/3) > 0.01 {
		t.Fatalf("after a second: %v", p)
	}
	drawn := len(c.Props[ops.PropCanvasCommands].Bytes)
	if drawn == 0 {
		t.Fatal("nothing drawn")
	}
	// To its end, once: it completes there.
	frame(h, 3*time.Second)
	if progress.Peek() != 1 || completed != 1 {
		t.Fatalf("at the end: %v, completed %d", progress.Peek(), completed)
	}
	// Scrubbing: a write moves it there.
	playing.Set(false)
	progress.Set(0.25)
	r.Flush()
	if p := progress.Peek(); p != 0.25 {
		t.Fatalf("scrubbed to %v", p)
	}
	// Inside ui.Animate, it animates there over the animation's time.
	ui.AnimateWith(ui.Animation{Duration: 1, Curve: ui.Linear}, func() { progress.Set(0.75) })
	r.Flush()
	frame(h, 16*time.Millisecond)
	frame(h, 500*time.Millisecond)
	if p := progress.Peek(); math.Abs(p-0.5) > 0.02 {
		t.Fatalf("half way through the seek's animation: %v", p)
	}
	frame(h, 600*time.Millisecond)
	if p := progress.Peek(); p != 0.75 {
		t.Fatalf("after the seek's animation: %v", p)
	}
	// A marker's frames: it plays them from their start.
	marker.Set("check")
	playing.Set(true)
	r.Flush()
	if p := progress.Peek(); math.Abs(p-16.0/90) > 1e-9 {
		t.Fatalf("at the marker's start: %v", p)
	}
	frame(h, 0)
	frame(h, 2*time.Second)
	if p := progress.Peek(); math.Abs(p-36.0/90) > 1e-9 {
		t.Fatalf("at the marker's end: %v", p)
	}
	// Reduce Motion: it stops, at the end of what it plays.
	marker.Set("")
	r.Flush()
	env := h.Env
	env.ReduceMotion = true
	h.SetEnvironment(env)
	if p := progress.Peek(); p != 1 {
		t.Fatalf("with Reduce Motion: %v", p)
	}
	frame(h, time.Second)
	if p := progress.Peek(); p != 1 {
		t.Fatalf("it played with Reduce Motion: %v", p)
	}
}

// The Go player loops, and plays backwards at a negative speed.
func TestCanvasPlayerLoops(t *testing.T) {
	usePlayer(t, canvasPlayer)
	progress := signal.New(0.0)
	speed := signal.New(1.0)
	var info Info
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart}, View(Props{Data: successJSON(t), Loop: true, Speed: speed, Progress: progress,
			OnLoad: func(i Info) { info = i }, Style: ui.Style{Width: ui.Pt(100)}}))
	})
	loaded(t, h, func() bool { return info.FrameRate != 0 })
	if c := h.OfKind(ops.KindCanvas)[0]; c.Frame[2] != 100 || c.Frame[3] != 100 {
		t.Fatalf("a width, and the animation's aspect ratio: %v", c.Frame)
	}
	frame(h, 0)
	frame(h, 3500*time.Millisecond) // past its 3s: round again
	if p := progress.Peek(); math.Abs(p-0.5/3) > 0.01 {
		t.Fatalf("looped: %v", p)
	}
	speed.Set(-1)
	r.Flush()
	frame(h, time.Second)
	if p := progress.Peek(); math.Abs(p-(0.5/3-1.0/3+1)) > 0.01 {
		t.Fatalf("backwards, around its start: %v", p)
	}
}

// nativeProps decodes the props the native player was last sent: name →
// encoded value.
func nativeProps(h *fake.Host) map[string][]byte {
	out := map[string][]byte{}
	v := h.OfKind(ops.KindNative)[0]
	for _, op := range h.Ops {
		if op.Code != ops.OpSetProp || op.ID != v.ID || op.Prop != ops.PropNativeProp {
			continue
		}
		b := op.Value.Bytes
		n := int(binary.LittleEndian.Uint32(b))
		name := string(b[4 : 4+n])
		b = b[4+n:]
		if b[0] == 5 { // bytes
			l := int(binary.LittleEndian.Uint32(b[1:]))
			out[name] = b[5 : 5+l]
		}
	}
	return out
}

func seekOf(b []byte) lottieplayer.SeekTo {
	r := native.NewReader(b)
	return lottieplayer.SeekTo{Progress: r.Float64(), To: r.Int(), Duration: r.Float64(), Curve: r.Int(), Damping: r.Float64(), Serial: r.Int()}
}

// nativeEvent is a native view's event as a host sends it: its name and
// its value ("v", the encoded bytes).
func nativeEvent(name string, v []byte) []byte {
	str := func(b []byte, s string) []byte {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	b := str(nil, name)
	b = binary.LittleEndian.AppendUint32(b, 1)
	b = str(b, "v")
	b = append(b, 5)
	return str(b, string(v))
}

// The platforms' players get what to do as props: playing (autoplay),
// seeks from the app's writes of Progress (with ui.Animate's duration),
// Reduce Motion's stop at the end; their reports write Progress without
// seeking back; their Loaded sizes the view.
func TestNativePlayer(t *testing.T) {
	progress := signal.New(0.0)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart}, View(Props{Source: ui.Val("success.json"), Progress: progress,
			Overrides: ui.Val([]Override{Colors("Ring", ops.RGBA(255, 0, 0, 255), ops.RGBA(0, 0, 255, 255))})}))
	})
	v := h.OfKind(ops.KindNative)[0]
	if v.String(ops.PropNativeType) != native.ViewKind(&native.Module{ID: "github.com/egoist/asuka/sdk/lottie/internal/lottieplayer",
		Swift:  "AsukaModule_github_com_egoist_asuka_sdk_lottie_internal_lottieplayer",
		Kotlin: "com.github.egoist.asuka.sdk.lottie.internal.lottieplayer.AsukaModule"}, "Player") {
		t.Fatalf("kind %q", v.String(ops.PropNativeType))
	}
	p := nativeProps(h)
	if r := native.NewReader(p["Playing"]); !r.Bool() {
		t.Fatal("autoplay")
	}
	if r := native.NewReader(p["Source"]); r.String() != "success.json" {
		t.Fatal("source")
	}
	if r := native.NewReader(p["Report"]); !r.Bool() {
		t.Fatal("a Progress is reported")
	}
	// Overrides' colors for the appearance: light now.
	ovr := native.NewReader(p["Overrides"])
	if n := ovr.Len(); n != 1 || ovr.String() != "Ring" || ovr.Int() != 0 || ovr.Float64() != 1 {
		t.Fatal("overrides")
	}
	first := seekOf(p["Seek"]).Serial
	// A report writes Progress, and seeks nowhere.
	w := native.NewWriter()
	w.Float64(0.4)
	h.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventNative, Bytes: nativeEvent("Progress", w.Take())})
	if progress.Peek() != 0.4 || seekOf(nativeProps(h)["Seek"]).Serial != first {
		t.Fatal("a report sought")
	}
	// The app's write inside ui.Animate seeks, animated.
	ui.AnimateWith(ui.Animation{Duration: 0.3, Curve: ui.EaseOut}, func() { progress.Set(0.9) })
	r.Flush()
	s := seekOf(nativeProps(h)["Seek"])
	if s.Serial == first || s.Progress != 0.9 || s.Duration != float64(float32(0.3)) || s.Curve != int(ui.EaseOut) {
		t.Fatalf("seek %+v", s)
	}
	// Loaded: the view takes the animation's size.
	w = native.NewWriter()
	w.Float64(200) // width
	w.Float64(100) // height
	w.Float64(0)
	w.Float64(60)
	w.Float64(30)
	w.Len(0) // markers
	h.Dispatch(ops.Event{ID: v.ID, Kind: ops.EventNative, Bytes: nativeEvent("Loaded", w.Take())})
	if v.Frame[2] != 200 || v.Frame[3] != 100 {
		t.Fatalf("natural size: %v", v.Frame)
	}
	// Dark mode: the overrides' dark colors.
	env := h.Env
	env.Dark = true
	h.SetEnvironment(env)
	ovr = native.NewReader(nativeProps(h)["Overrides"])
	if ovr.Len(); ovr.String() != "Ring" || ovr.Int() != 0 || ovr.Float64() != 0 {
		t.Fatal("dark overrides")
	}
	// Reduce Motion: not playing, at the end.
	env.ReduceMotion = true
	h.SetEnvironment(env)
	p = nativeProps(h)
	if native.NewReader(p["Playing"]).Bool() {
		t.Fatal("playing with Reduce Motion")
	}
	if s := seekOf(p["Seek"]); s.To != 2 {
		t.Fatalf("Reduce Motion's seek: %+v", s)
	}
}

// A labelled animation is an image to screen readers.
func TestLabel(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return View(Props{Source: ui.Val("success.json"), Label: ui.Val("Done")})
	})
	views := h.OfKind(ops.KindView)
	found := false
	for _, v := range views {
		if v.String(ops.PropA11yLabel) == "Done" {
			found = true
		}
	}
	if !found {
		t.Fatal("no labelled view")
	}
}
