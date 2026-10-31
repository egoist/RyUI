package screen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/screen"
	"github.com/egoist/ryui/ui"
)

func TestSettings(t *testing.T) {
	h := fake.New()
	var calls []string
	h.Modules = map[string]func(string, []byte) ([]byte, error){"screen": func(m string, a []byte) ([]byte, error) {
		calls = append(calls, fmt.Sprint(m, a))
		return nil, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	screen.Lock(screen.Landscape)
	screen.SetBars(screen.Bars{Style: screen.Light, HideStatus: true, HideNavigation: true})
	screen.PreventCapture(true)
	r.Flush()
	h.RunPosted()
	if strings.Join(calls, " ") != "lock[3] bars[1 3] preventCapture[1]" {
		t.Fatalf("calls %v", calls)
	}
}
