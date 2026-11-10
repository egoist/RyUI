package windows

import "math"

// Shadow layers' math, apart from the host (it builds anywhere, so it's
// tested anywhere).

// shadowStep is one of the rounded rectangles a shadow is drawn with:
// how far it reaches past the shape (negative: inside it) and its alpha.
type shadowStep struct {
	spread float64 // pixels
	alpha  float64 // 0 to 1
}

// maxShadowSteps bounds the rectangles a shadow takes.
const maxShadowSteps = 24

// shadowSteps are the rectangles that draw a shadow of alpha a blurred
// by a Gaussian of σ sigma (pixels), as the other platforms draw it: at
// a distance d past the shape's edge, a·½erfc(d/(σ√2)), from 3σ outside
// to 3σ inside, the outermost first. They're all one color, so where
// they overlap their alphas compound: each one's is what takes the sum
// at its step to the Gaussian's in the step's middle.
func shadowSteps(sigma, a float64, out []shadowStep) []shadowStep {
	out = out[:0]
	a = min(a, 0.999)
	if a <= 0 {
		return out
	}
	if sigma <= 0.25 {
		return append(out, shadowStep{0, a}) // no blur to speak of
	}
	n := int(min(max(math.Ceil(6*sigma), 2), maxShadowSteps))
	step := 6 * sigma / float64(n)
	prev := 0.0 // the sum outside this step
	for i := range n {
		spread := 3*sigma - float64(i)*step
		want := a * 0.5 * math.Erfc((spread-step/2)/(sigma*math.Sqrt2))
		if i == n-1 {
			want = a // the innermost: as dark as the shadow gets
		}
		if want <= prev {
			continue
		}
		out = append(out, shadowStep{spread, 1 - (1-want)/(1-prev)})
		prev = want
	}
	return out
}
