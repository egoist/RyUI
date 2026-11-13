package windows

import (
	"strconv"
	"strings"
)

// The tray's and the jump list's parts apart from the host (they build
// anywhere, so they're tested anywhere, as deeplink.go's).

// dockItemArg starts the app for a Dock menu item (a jump list task):
// --asuka-dock-item=<its number>.
const dockItemArg = "--asuka-dock-item="

// dockItemLaunch is what a launch for a Dock menu item hands the running
// instance, in place of a URL.
const dockItemLaunch = "asuka-dock-item:"

// takeDockItem takes the Dock menu item argument out of args (os.Args),
// and reports its number (-1: none).
func takeDockItem(args []string) ([]string, int) {
	for i, a := range args {
		if i == 0 || !strings.HasPrefix(a, dockItemArg) {
			continue
		}
		n, err := strconv.Atoi(a[len(dockItemArg):])
		if err != nil || n < 0 {
			continue
		}
		out := append(append([]string(nil), args[:i]...), args[i+1:]...)
		return out, n
	}
	return args, -1
}

// dockItemPayload reads a launch handed over for a Dock menu item: its
// number.
func dockItemPayload(s string) (int, bool) {
	if !strings.HasPrefix(s, dockItemLaunch) {
		return 0, false
	}
	n, err := strconv.Atoi(s[len(dockItemLaunch):])
	return n, err == nil && n >= 0
}

func itoa(i int) string { return strconv.Itoa(i) }

// fitIcon fits an image of w×h premultiplied BGRA pixels into a square
// icon of size pixels, its aspect kept and centered (each icon pixel the
// average of the image's it covers, or the nearest when it's smaller):
// BGRA not premultiplied, as icons take it.
func fitIcon(px []byte, w, h, size int32) []byte {
	out := make([]byte, int(size)*int(size)*4)
	if w <= 0 || h <= 0 || size <= 0 || len(px) < int(w)*int(h)*4 {
		return out
	}
	s := min(float64(size)/float64(w), float64(size)/float64(h))
	dw, dh := float64(w)*s, float64(h)*s
	ox, oy := (float64(size)-dw)/2, (float64(size)-dh)/2
	for y := range size {
		for x := range size {
			// The image's rectangle under this pixel.
			x0, x1 := (float64(x)-ox)/s, (float64(x)+1-ox)/s
			y0, y1 := (float64(y)-oy)/s, (float64(y)+1-oy)/s
			if x1 <= 0 || y1 <= 0 || x0 >= float64(w) || y0 >= float64(h) {
				continue
			}
			ix0, ix1 := int32(max(x0, 0)), min(int32(x1+0.999), w)
			iy0, iy1 := int32(max(y0, 0)), min(int32(y1+0.999), h)
			if ix1 <= ix0 {
				ix1 = ix0 + 1
			}
			if iy1 <= iy0 {
				iy1 = iy0 + 1
			}
			var sum [4]uint32
			n := uint32(0)
			for iy := iy0; iy < iy1; iy++ {
				for ix := ix0; ix < ix1; ix++ {
					i := (int(iy)*int(w) + int(ix)) * 4
					sum[0] += uint32(px[i])
					sum[1] += uint32(px[i+1])
					sum[2] += uint32(px[i+2])
					sum[3] += uint32(px[i+3])
					n++
				}
			}
			a := sum[3] / n
			o := (int(y)*int(size) + int(x)) * 4
			out[o+3] = byte(a)
			if a == 0 {
				continue
			}
			for c := range 3 {
				out[o+c] = byte(min(sum[c]/n*255/a, 255))
			}
		}
	}
	return out
}
