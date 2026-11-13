package barcode

import "sync"

// bitImage is a black and white image: one byte a pixel, 1 for dark.
type bitImage struct {
	w, h int
	pix  []uint8
	// black is the black point of each 8 by 8 block, kept for reuse.
	black []int32
}

var bitPool sync.Pool

func newBitImage(w, h int) *bitImage {
	b, _ := bitPool.Get().(*bitImage)
	if b == nil {
		b = &bitImage{}
	}
	b.w, b.h = w, h
	if cap(b.pix) < w*h {
		b.pix = make([]uint8, w*h)
	}
	b.pix = b.pix[:w*h]
	return b
}

func (b *bitImage) release() { bitPool.Put(b) }

// at reports whether (x, y) is dark; outside the image is light.
func (b *bitImage) at(x, y int) bool {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return false
	}
	return b.pix[y*b.w+x] != 0
}

const (
	blockPower   = 3
	blockSize    = 1 << blockPower
	minDynRange  = 24
	minLocalSize = 5 * blockSize
)

// binarize thresholds l locally, as ZXing's HybridBinarizer does: each 8
// by 8 block against the mean black point of the 5 by 5 blocks around
// it, so uneven light and shadows don't matter.
func binarize(l *luminance) *bitImage {
	w, h := l.w, l.h
	b := newBitImage(w, h)
	if w < minLocalSize || h < minLocalSize {
		globalThreshold(l, b)
		return b
	}
	subW := (w + blockSize - 1) >> blockPower
	subH := (h + blockSize - 1) >> blockPower
	if cap(b.black) < subW*subH {
		b.black = make([]int32, subW*subH)
	}
	black := b.black[:subW*subH]
	maxX, maxY := w-blockSize, h-blockSize
	for y := 0; y < subH; y++ {
		yo := min(y<<blockPower, maxY)
		for x := 0; x < subW; x++ {
			xo := min(x<<blockPower, maxX)
			sum, lo, hi := 0, 255, 0
			for yy := 0; yy < blockSize; yy++ {
				row := l.pix[(yo+yy)*w+xo : (yo+yy)*w+xo+blockSize]
				for _, p := range row {
					v := int(p)
					sum += v
					lo = min(lo, v)
					hi = max(hi, v)
				}
			}
			avg := sum >> (2 * blockPower)
			if hi-lo <= minDynRange {
				// A flat block: it's light unless its neighbors say its
				// level is dark.
				avg = lo / 2
				if y > 0 && x > 0 {
					n := (int(black[(y-1)*subW+x]) + 2*int(black[y*subW+x-1]) + int(black[(y-1)*subW+x-1])) / 4
					if lo < n {
						avg = n
					}
				}
			}
			black[y*subW+x] = int32(avg)
		}
	}
	for y := 0; y < subH; y++ {
		yo := min(y<<blockPower, maxY)
		top := clampBlock(y, subH-3)
		for x := 0; x < subW; x++ {
			xo := min(x<<blockPower, maxX)
			left := clampBlock(x, subW-3)
			sum := 0
			for z := -2; z <= 2; z++ {
				r := black[(top+z)*subW+left-2 : (top+z)*subW+left+3]
				sum += int(r[0] + r[1] + r[2] + r[3] + r[4])
			}
			t := uint8(min(sum/25, 255))
			for yy := 0; yy < blockSize; yy++ {
				src := l.pix[(yo+yy)*w+xo : (yo+yy)*w+xo+blockSize]
				dst := b.pix[(yo+yy)*w+xo : (yo+yy)*w+xo+blockSize]
				for i, p := range src {
					if p <= t {
						dst[i] = 1
					} else {
						dst[i] = 0
					}
				}
			}
		}
	}
	return b
}

func clampBlock(v, hi int) int {
	if v < 2 {
		return 2
	}
	return min(v, hi)
}

// globalThreshold thresholds a small image at the valley between its
// histogram's two peaks.
func globalThreshold(l *luminance, b *bitImage) {
	var hist [32]int
	for _, p := range l.pix {
		hist[p>>3]++
	}
	first, firstN := 0, 0
	for i, n := range hist {
		if n > firstN {
			first, firstN = i, n
		}
	}
	second, best := 0, 0
	for i, n := range hist {
		d := i - first
		if s := d * d * n; s > best {
			second, best = i, s
		}
	}
	if first > second {
		first, second = second, first
	}
	t := (first + second) / 2
	if second-first > 1 {
		// The emptiest bucket between the peaks, nearer the light one.
		bestV := -1
		for i := second - 1; i > first; i-- {
			d := i - first
			s := d * d * (second - i) * (firstN - hist[i])
			if s > bestV {
				t, bestV = i, s
			}
		}
	}
	th := uint8(t << 3)
	for i, p := range l.pix {
		if p < th {
			b.pix[i] = 1
		} else {
			b.pix[i] = 0
		}
	}
}
