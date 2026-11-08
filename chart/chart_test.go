package chart

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func TestValueScale(t *testing.T) {
	for _, c := range []struct {
		series []Series
		want   string
	}{
		{[]Series{{Kind: Line, Values: []float64{3, 7, 12}}}, "2 4 6 8 10 12"},
		{[]Series{{Kind: Line, Values: []float64{103, 107, 112}}}, "102 104 106 108 110 112"},
		{[]Series{{Kind: Bar, Values: []float64{103, 107, 112}}}, "0 25 50 75 100 125"},
		{[]Series{{Kind: Bar, Values: []float64{-4, 9}}}, "-5 0 5 10"},
		{nil, "0 0.2 0.4 0.6 0.8 1"},
	} {
		s := valueScale(c.series, 0, 0)
		var got []string
		for _, v := range s.ticks {
			got = append(got, short(v, s.step))
		}
		if g := strings.Join(got, " "); g != c.want {
			t.Errorf("%v: ticks %s, want %s", c.series, g, c.want)
		}
	}
	if s := valueScale(nil, 0, 100); s.lo != 0 || s.hi != 100 || len(s.ticks) != 6 {
		t.Errorf("a fixed range %+v", s)
	}
}

func TestShort(t *testing.T) {
	for _, c := range []struct {
		v, step float64
		want    string
	}{{2500, 500, "2.5k"}, {3000000, 1e6, "3M"}, {0.25, 0.05, "0.25"}, {12, 2, "12"}} {
		if got := short(c.v, c.step); got != c.want {
			t.Errorf("short(%v, %v) = %q, want %q", c.v, c.step, got, c.want)
		}
	}
}

// A chart is a canvas between its value labels and its category labels,
// read by screen readers as its label and values, redrawn as its series
// change.
func TestView(t *testing.T) {
	data := signal.NewFunc([]Series{{Name: "Steps", Kind: Bar, Values: []float64{4200, 8100, math.NaN()}}}, nil)
	h := fake.New()
	r := ui.Mount(h, func() ui.Node {
		return View(Props{Series: data, Labels: ui.Val([]string{"Mon", "Tue", "Wed"}), Label: ui.Val("Steps this week"),
			Style: ui.Style{Height: ui.Pt(200)}})
	})
	defer r.Dispose()
	cv := h.OfKind(ops.KindCanvas)
	if len(cv) != 1 || len(cv[0].Props[ops.PropCanvasCommands].Bytes) == 0 {
		t.Fatal("no plot drawn")
	}
	for _, l := range []string{"Mon", "Tue", "Wed", "0", "10k"} {
		if len(h.Find(ops.KindText, l)) == 0 {
			t.Errorf("no label %q", l)
		}
	}
	plot := cv[0].Frame
	tue := h.Find(ops.KindText, "Tue")[0]
	// Tue's center is under the second of three slots.
	if c := tue.Parent.Frame[0] + tue.Frame[0] + tue.Frame[2]/2; math.Abs(float64(c-(plot[0]+plot[2]/2))) > 1 {
		t.Errorf("Tue centered at %v, the plot's middle is %v", c, plot[0]+plot[2]/2)
	}
	var a11y string
	for _, v := range h.OfKind(ops.KindView) {
		if s := v.String(ops.PropA11yLabel); s != "" {
			a11y = s
		}
	}
	if a11y != "Steps this week. Steps: Mon 4200, Tue 8100, Wed –" {
		t.Errorf("a11y %q", a11y)
	}
	before := fmt.Sprint(cv[0].Props[ops.PropCanvasCommands].Bytes)
	data.Set([]Series{{Kind: Line, Values: []float64{1, 2, 3}, Smooth: true}})
	r.Flush()
	if fmt.Sprint(cv[0].Props[ops.PropCanvasCommands].Bytes) == before {
		t.Fatal("the plot didn't follow its series")
	}
}
