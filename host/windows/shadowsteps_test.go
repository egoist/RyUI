package windows

import (
	"math"
	"testing"
)

// The rectangles' compounded alpha follows a Gaussian-blurred edge, as
// the other platforms' shadows do: half the shadow's alpha at the edge,
// fading out to about 3σ.
func TestShadowSteps(t *testing.T) {
	for _, sigma := range []float64{1, 2, 4, 8, 20} {
		for _, a := range []float64{1, 0.3} {
			steps := shadowSteps(sigma, a, nil)
			if len(steps) > maxShadowSteps {
				t.Fatalf("σ %g: %d steps", sigma, len(steps))
			}
			// The alpha at d pixels past the edge: every rectangle that
			// reaches it, compounded.
			at := func(d float64) float64 {
				clear := 1.0
				for _, s := range steps {
					if s.spread >= d {
						clear *= 1 - s.alpha
					}
				}
				return 1 - clear
			}
			step := 6 * sigma / float64(len(steps))
			for d := -3 * sigma; d <= 3*sigma; d += sigma / 4 {
				want := min(a, 0.999) * 0.5 * math.Erfc(d/(sigma*math.Sqrt2))
				// To half a step of the curve where it's steepest, at the
				// edge: its slope is a/(σ√(2π)), about 0.4a/σ.
				if got := at(d); math.Abs(got-want) > 0.2*a*step/sigma+0.01 {
					t.Errorf("σ %g, alpha %g: %.3f at %.2f past the edge, want %.3f", sigma, a, got, d, want)
				}
			}
			if got := at(3*sigma + 0.5); got != 0 {
				t.Errorf("σ %g: %.3f past 3σ", sigma, got)
			}
		}
	}
}
