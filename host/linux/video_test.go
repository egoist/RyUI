//go:build linux && !android

package linux

import (
	"path/filepath"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A Video plays a file (testdata/clip.webm: 160x90, a second): its
// natural size sizes the view (Loaded), it plays to its end (Ended), and
// hiding the controls shows it in a picture instead of a GtkVideo.
func TestVideo(t *testing.T) {
	clip, _ := filepath.Abs("testdata/clip.webm")
	ends := 0
	hide := signal.New(false)
	var errs []string
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart},
			ui.Dynamic(func() ui.Node {
				return ui.Video(ui.VideoProps{
					Source:       ui.Val(clip),
					AutoPlay:     true,
					Muted:        true,
					HideControls: hide.Get(),
					OnEnd:        func() { ends++ },
					OnError:      func(m string) { errs = append(errs, m) },
					Style:        ui.Style{Width: ui.Pt(320)},
				})
			}))
	})
	var n *node
	waitFor(t, "the video's size", func() bool {
		if len(errs) > 0 {
			return true
		}
		vs := find(h, ops.KindVideo)
		if len(vs) == 1 && vs[0].frame[3] > 0 {
			n = vs[0]
			return true
		}
		return false
	})
	if len(errs) > 0 {
		t.Fatalf("the video failed: %q", errs)
	}
	onMain(func() {
		if n.frame[2] != 320 || n.frame[3] != 180 {
			fatalf(t, "frame %v, want 320x180 (the clip's 16:9)", n.frame)
		}
		if vd := n.ext.(*video); !TypeCheckInstanceIsA(n.inner, Call(Sym("gtk_video_get_type"))) || vd.widget != n.inner {
			fatalf(t, "not a GtkVideo")
		}
	})
	waitFor(t, "the end", func() bool { return ends > 0 })

	// Without controls: a picture showing the stream.
	ui.Post(func() { hide.Set(true) })
	waitFor(t, "the new video", func() bool {
		vs := find(h, ops.KindVideo)
		return len(vs) == 1 && vs[0] != n && vs[0].inner != 0
	})
	onMain(func() {
		m := find(h, ops.KindVideo)[0]
		vd := m.ext.(*video)
		if !TypeCheckInstanceIsA(m.inner, Call(Sym("gtk_picture_get_type"))) || vd.stream == 0 {
			fatalf(t, "hidden controls: not a picture of the stream")
		}
		if old := n.ext.(*video); old.stream != 0 || len(old.handlers) != 0 {
			fatalf(t, "the first video's stream wasn't let go")
		}
	})
}
