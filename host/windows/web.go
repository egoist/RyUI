//go:build windows && amd64

package windows

import "github.com/egoist/nui/ops"

type web struct{}

func (h *Host) makeWeb(n *node) { fail("can't show WebView yet") }

func (w *web) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {}
func (w *web) resized(h *Host, n *node)                             {}
func (w *web) close()                                               {}
func (w *web) eval(js string)                                       {}
func (w *web) navigate(op byte)                                     {}
