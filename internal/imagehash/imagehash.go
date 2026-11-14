// Package imagehash decodes the compact placeholders apps send ahead of
// an image: BlurHash (blurha.sh) and ThumbHash (evanw.github.io/thumbhash),
// into small pictures the hosts show until the image loads. The decoders
// follow the reference implementations (blurhash's TypeScript,
// thumbhash's JavaScript) step for step, so a hash decodes here to the
// pixels it decodes to there.
package imagehash

import (
	"encoding/base64"
	"errors"
	"math"
	"strings"
)

// ErrHash is a hash that isn't one.
var ErrHash = errors.New("imagehash: not a valid hash")

const digits83 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

func decode83(s string) (int, bool) {
	v := 0
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(digits83, s[i])
		if d < 0 {
			return 0, false
		}
		v = v*83 + d
	}
	return v, true
}

func srgbToLinear(v int) float64 {
	f := float64(v) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) uint8 {
	f := math.Max(0, math.Min(1, v))
	if f <= 0.0031308 {
		return uint8(math.Trunc(f*12.92*255 + 0.5))
	}
	return uint8(math.Trunc((1.055*math.Pow(f, 1/2.4)-0.055)*255 + 0.5))
}

func signPow(v, e float64) float64 {
	s := 1.0
	if v < 0 {
		s = -1
	}
	return s * math.Pow(math.Abs(v), e)
}

// BlurHashSize is a BlurHash's components across and down, if it's
// well formed.
func BlurHashSize(hash string) (nx, ny int, err error) {
	if len(hash) < 6 {
		return 0, 0, ErrHash
	}
	flag, ok := decode83(hash[:1])
	if !ok {
		return 0, 0, ErrHash
	}
	ny, nx = flag/9+1, flag%9+1
	if len(hash) != 4+2*nx*ny {
		return 0, 0, ErrHash
	}
	return nx, ny, nil
}

// BlurHash decodes hash into w×h pixels: RGBA, 8 bits each, opaque.
// punch above 1 makes its colors stronger (blurhash's punch; 0 or 1 as
// they are).
func BlurHash(hash string, w, h int, punch float64) ([]byte, error) {
	nx, ny, err := BlurHashSize(hash)
	if err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return nil, errors.New("imagehash: an empty size")
	}
	if punch < 1 {
		punch = 1
	}
	q, ok := decode83(hash[1:2])
	if !ok {
		return nil, ErrHash
	}
	maxv := float64(q+1) / 166
	colors := make([][3]float64, nx*ny)
	for i := range colors {
		if i == 0 {
			v, ok := decode83(hash[2:6])
			if !ok {
				return nil, ErrHash
			}
			colors[0] = [3]float64{srgbToLinear(v >> 16), srgbToLinear(v >> 8 & 255), srgbToLinear(v & 255)}
			continue
		}
		v, ok := decode83(hash[4+i*2 : 6+i*2])
		if !ok {
			return nil, ErrHash
		}
		m := maxv * punch
		colors[i] = [3]float64{
			signPow(float64(v/361-9)/9, 2) * m,
			signPow(float64(v/19%19-9)/9, 2) * m,
			signPow(float64(v%19-9)/9, 2) * m,
		}
	}
	// The cosines along each axis, computed once (the reference computes
	// them per pixel, the same products).
	cx := make([]float64, w*nx)
	for x := range w {
		for i := range nx {
			cx[x*nx+i] = math.Cos(math.Pi * float64(x) * float64(i) / float64(w))
		}
	}
	cy := make([]float64, h*ny)
	for y := range h {
		for j := range ny {
			cy[y*ny+j] = math.Cos(math.Pi * float64(y) * float64(j) / float64(h))
		}
	}
	px := make([]byte, w*h*4)
	for y := range h {
		for x := range w {
			var r, g, b float64
			for j := range ny {
				for i := range nx {
					basis := cx[x*nx+i] * cy[y*ny+j]
					c := &colors[i+j*nx]
					r += c[0] * basis
					g += c[1] * basis
					b += c[2] * basis
				}
			}
			o := (y*w + x) * 4
			px[o], px[o+1], px[o+2], px[o+3] = linearToSRGB(r), linearToSRGB(g), linearToSRGB(b), 255
		}
	}
	return px, nil
}

// ParseThumbHash reads a ThumbHash as apps keep it: base64 (standard or
// URL-safe, padded or not).
func ParseThumbHash(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	enc := base64.RawStdEncoding
	if strings.ContainsAny(s, "-_") {
		enc = base64.RawURLEncoding
	}
	b, err := enc.DecodeString(s)
	if err != nil || len(b) < 5 {
		return nil, ErrHash
	}
	return b, nil
}

// ThumbHashRatio is the width over the height of the image hash was made
// from, approximately.
func ThumbHashRatio(hash []byte) float64 {
	if len(hash) < 5 {
		return 1
	}
	header := int(hash[3])
	alpha := hash[2]&0x80 != 0
	landscape := hash[4]&0x80 != 0
	var lx, ly int
	switch {
	case landscape && alpha:
		lx, ly = 5, header&7
	case landscape:
		lx, ly = 7, header&7
	case alpha:
		lx, ly = header&7, 5
	default:
		lx, ly = header&7, 7
	}
	if ly == 0 {
		return 1
	}
	return float64(lx) / float64(ly)
}

// ThumbHash decodes hash into its picture: at most 32 pixels on its
// longer side, RGBA, 8 bits each, not premultiplied.
func ThumbHash(hash []byte) (w, h int, rgba []byte, err error) {
	if len(hash) < 5 {
		return 0, 0, nil, ErrHash
	}
	at := func(i int) int {
		if i < len(hash) {
			return int(hash[i])
		}
		return 0
	}
	header24 := at(0) | at(1)<<8 | at(2)<<16
	header16 := at(3) | at(4)<<8
	lDC := float64(header24&63) / 63
	pDC := float64(header24>>6&63)/31.5 - 1
	qDC := float64(header24>>12&63)/31.5 - 1
	lScale := float64(header24>>18&31) / 31
	hasAlpha := header24>>23 != 0
	pScale := float64(header16>>3&63) / 63
	qScale := float64(header16>>9&63) / 63
	landscape := header16>>15 != 0
	lx, ly := header16&7, 7
	if hasAlpha {
		ly = 5
	}
	if landscape {
		lx, ly = 7, header16&7
		if hasAlpha {
			lx = 5
		}
	}
	lx, ly = max(3, lx), max(3, ly)
	aDC, aScale := 1.0, float64(at(5)>>4)/15
	if hasAlpha {
		aDC = float64(at(5)&15) / 15
	}
	start := 5
	if hasAlpha {
		start = 6
	}
	index := 0
	channel := func(nx, ny int, scale float64) []float64 {
		var ac []float64
		for cy := range ny {
			cx := 0
			if cy == 0 {
				cx = 1
			}
			for ; cx*ny < nx*(ny-cy); cx++ {
				v := at(start+index>>1) >> ((index & 1) << 2) & 15
				index++
				ac = append(ac, (float64(v)/7.5-1)*scale)
			}
		}
		return ac
	}
	lAC := channel(lx, ly, lScale)
	pAC := channel(3, 3, pScale*1.25)
	qAC := channel(3, 3, qScale*1.25)
	var aAC []float64
	if hasAlpha {
		aAC = channel(5, 5, aScale)
	}
	ratio := ThumbHashRatio(hash)
	if ratio > 1 {
		w, h = 32, int(math.Round(32/ratio))
	} else {
		w, h = int(math.Round(32*ratio)), 32
	}
	if w <= 0 || h <= 0 {
		return 0, 0, nil, ErrHash
	}
	nfx, nfy := max(lx, 3), max(ly, 3)
	if hasAlpha {
		nfx, nfy = max(lx, 5), max(ly, 5)
	}
	fx, fy := make([]float64, nfx), make([]float64, nfy)
	rgba = make([]byte, w*h*4)
	clamp := func(v float64) byte { return byte(math.Max(0, 255*math.Min(1, v))) }
	for y, i := 0, 0; y < h; y++ {
		for x := 0; x < w; x, i = x+1, i+4 {
			l, p, q, a := lDC, pDC, qDC, aDC
			for cx := range fx {
				fx[cx] = math.Cos(math.Pi / float64(w) * (float64(x) + 0.5) * float64(cx))
			}
			for cy := range fy {
				fy[cy] = math.Cos(math.Pi / float64(h) * (float64(y) + 0.5) * float64(cy))
			}
			for cy, j := 0, 0; cy < ly; cy++ {
				fy2 := fy[cy] * 2
				cx := 0
				if cy == 0 {
					cx = 1
				}
				for ; cx*ly < lx*(ly-cy); cx, j = cx+1, j+1 {
					l += lAC[j] * fx[cx] * fy2
				}
			}
			for cy, j := 0, 0; cy < 3; cy++ {
				fy2 := fy[cy] * 2
				cx := 0
				if cy == 0 {
					cx = 1
				}
				for ; cx < 3-cy; cx, j = cx+1, j+1 {
					f := fx[cx] * fy2
					p += pAC[j] * f
					q += qAC[j] * f
				}
			}
			if hasAlpha {
				for cy, j := 0, 0; cy < 5; cy++ {
					fy2 := fy[cy] * 2
					cx := 0
					if cy == 0 {
						cx = 1
					}
					for ; cx < 5-cy; cx, j = cx+1, j+1 {
						a += aAC[j] * fx[cx] * fy2
					}
				}
			}
			b := l - 2.0/3*p
			r := (3*l - b + q) / 2
			g := r - q
			rgba[i], rgba[i+1], rgba[i+2], rgba[i+3] = clamp(r), clamp(g), clamp(b), clamp(a)
		}
	}
	return w, h, rgba, nil
}
