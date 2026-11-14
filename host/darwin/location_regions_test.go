//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"testing"

	"github.com/egoist/asuka/ui"
)

// asuka/sdk/location's authorization and regions are on macOS: the state
// is three bytes, the regions the system's (20 at most, while the app
// runs), the background updates the Go package's.
func TestLocationModules(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		for mod, ms := range map[string][]string{
			"locationAccess":     {"current", "next", "stop", "request", "precise", "settings"},
			"locationBackground": {"capabilities", "start", "stop", "updating", "monitor", "unmonitor", "regions", "next", "done"},
		} {
			for _, m := range ms {
				if h.module(mod)[m] == nil {
					t.Errorf("%s.%s isn't on macOS", mod, m)
				}
			}
		}
		if b, err, ok := callNow(t, h, "locationAccess", "current", nil); !ok || err != nil || len(b) != 3 {
			t.Errorf("locationAccess.current: %v %v %v", b, err, ok)
		}
		b, err, _ := callNow(t, h, "locationBackground", "capabilities", nil)
		var c struct {
			Updates, Regions, Relaunch bool
			Limit                      int
			MaxRadius                  float64
		}
		if err != nil || json.Unmarshal(b, &c) != nil || c.Updates || !c.Regions || c.Relaunch || c.Limit != 20 {
			t.Errorf("capabilities %s %v", b, err)
		}
		if _, err, _ := callNow(t, h, "locationBackground", "start", []byte(`{}`)); err == nil {
			t.Error("start: computed by the Go code, not the host")
		}
		if b, _, _ := callNow(t, h, "locationBackground", "regions", nil); string(b) != "[]" {
			t.Errorf("regions %s", b)
		}
	})
}
