//go:build windows

package windows

import (
	"os"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// In dark mode the month calendar, which has no dark visual style, is
// drawn in the dark palette: a dark background, light text.
func TestDarkCalendar(t *testing.T) {
	os.Setenv("ASUKA_COLOR_SCHEME", "dark")
	defer os.Setenv("ASUKA_COLOR_SCHEME", "")
	day := signal.New(time.Date(2026, 3, 14, 0, 0, 0, 0, time.Local))
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 20},
			ui.DatePicker(ui.DatePickerProps{Value: day, Inline: true}),
			ui.DatePicker(ui.DatePickerProps{Value: day}),
		)
	})
	var cal *node
	onMain(t, func() {
		if !h.dark {
			fatalf("not dark")
		}
		showHost(h)
		cal = nodeWith(h, func(n *node) bool { return n.ctl != 0 && ClassName(n.ctl) == "SysMonthCal32" })
		if cal == nil {
			fatalf("no month calendar")
		}
	})
	img, x, y := capture(t, h, cal)
	r := ClientRect(cal.ctl)
	dark, light := 0, 0
	for py := y + 4; py < y+int(r.H())-4; py += 3 {
		for px := x + 4; px < x+int(r.W())-4; px += 3 {
			c := img.NRGBAAt(px, py)
			l := (int(c.R) + int(c.G) + int(c.B)) / 3
			switch {
			case l < 80:
				dark++
			case l > 200:
				light++
			}
		}
	}
	t.Logf("dark pixels %d, light %d", dark, light)
	if dark < 10*light || light == 0 {
		t.Errorf("the calendar isn't dark with light text: %d dark pixels, %d light", dark, light)
	}
}
