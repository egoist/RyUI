package appearance

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/ui"
)

func TestWire(t *testing.T) {
	for _, s := range []Settings{{}, {Accent: colors.RGB(0, 122, 255)}, {Accent: colors.Hex(0xff5257), IncreaseContrast: true, ReduceTransparency: true}} {
		got, err := decode(encode(s))
		if err != nil || got != s {
			t.Errorf("%+v: %+v %v", s, got, err)
		}
	}
	if _, err := decode([]byte{1}); err == nil {
		t.Error("short: no error")
	}
}

// Watch gets the settings now and each change, until stop.
func TestWatch(t *testing.T) {
	m := follow.NewFake(encode(Settings{Accent: colors.RGB(0, 122, 255)}))
	h := fake.New()
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"appearance": m.Answer}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	wait := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); { // a safety net, not what passes
			h.RunPosted()
			r.Flush()
			if cond() {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("waiting for %s; calls %v", what, m.Calls())
	}
	var seen []Settings
	var stop func()
	ui.Post(func() { stop = Watch(func(s Settings) { seen = append(seen, s) }) })
	wait("the settings now", func() bool { return len(seen) == 1 })
	purple := Settings{Accent: colors.RGB(165, 80, 167), IncreaseContrast: true}
	m.Set(encode(purple))
	wait("a change", func() bool { return len(seen) == 2 })
	if seen[1] != purple {
		t.Errorf("changed to %+v", seen[1])
	}
	ui.Post(stop)
	wait("stop", func() bool { return slices.Contains(m.Calls(), "stop") && m.Waiting() == 0 })
}
