//go:build windows && amd64

package windows

import (
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

const (
	enLink          = 0x070B
	enRequestResize = 0x0701
)

type rich struct{}

func (h *Host) makeRich(n *node) { fail("can't show RichText yet") }

func (r *rich) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {}
func (r *rich) invalidate(h *Host, n *node)                          {}
func (r *rich) resized(h *Host, n *node)                             {}
func (r *rich) placeInlines(h *Host, n *node)                        {}
func (r *rich) link(h *Host, n *node, lp uintptr) uintptr            { return 0 }
func (r *rich) measure(h *Host, n *node, c layout.Constraints) (float32, float32) {
	return 0, 0
}
