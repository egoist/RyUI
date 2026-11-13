package notify

import (
	"encoding/json"

	"github.com/egoist/asuka/internal/notifyspec"
	"github.com/egoist/asuka/ui"
)

// Presentation is how a notification shows while the app is in the
// foreground.
type Presentation int

const (
	// Banner shows it as it would with the app closed: a banner (iOS),
	// a heads-up notification (Android, as its channel says), its sound.
	Banner Presentation = iota
	// Quiet puts it in Notification Center (the notification shade)
	// without a banner or a sound.
	Quiet
	// Hide doesn't show it: the chat it's from is on screen.
	Hide
)

var presenter func(Delivered) Presentation

// Present sets f to say how each notification that comes while the app
// is in the foreground shows, local or push; nil (or no Present) shows
// them all as banners. f runs on the main thread, as the notification
// comes, and must answer at once: the host waits for it a second at
// most, then shows the notification. Main thread.
//
//	notify.Present(func(n notify.Delivered) notify.Presentation {
//		if n.Thread == openChat.Get() {
//			return notify.Hide
//		}
//		return notify.Banner
//	})
func Present(f func(n Delivered) Presentation) {
	had := presenter != nil
	presenter = f
	switch {
	case f != nil && !had:
		ui.Call("notify", "present", []byte{1}, nil)
		presenting.watch(present, f)
	case f == nil && had:
		ui.Call("notify", "present", []byte{0}, nil)
		presenting.watchers = nil
		ui.Call("notify", "stop", []byte(presenting.method), nil)
	}
}

var presenting = &chain{method: "nextPresent"}

func present(b []byte) {
	var p notifyspec.Present
	if json.Unmarshal(b, &p) != nil {
		return
	}
	how := Banner
	if f := presenter; f != nil {
		how = f(delivered(p.Notification))
	}
	ui.Call("notify", "presented", append([]byte{byte(how)}, p.Token...), nil)
}
