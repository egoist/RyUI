package notify

import (
	"encoding/json"

	"github.com/egoist/ryui/ui"
)

// Channel is an Android notification channel: a kind of notification the
// user can turn off, or make quieter, in the app's settings ("Messages",
// "Mentions", "Reminders"). Its importance, sound and vibration are its
// when it's made: after, they're the user's to change, and only the name,
// description and group follow SetChannels. iOS has no channels: there
// they're ignored.
type Channel struct {
	ID          string
	Name        string
	Description string
	Importance  Importance
	// Silent: no sound.
	Silent bool
	// NoVibration: it doesn't vibrate.
	NoVibration bool
	// NoBadge: its notifications don't count in the launcher's badge.
	NoBadge bool
	// Group is a channel group's ID (SetChannelGroups).
	Group string
}

// Importance is how much a channel's notifications interrupt.
type Importance int

const (
	// ImportanceDefault makes a sound, and shows in the status bar.
	ImportanceDefault Importance = iota
	// ImportanceHigh shows heads-up too: messages, calls.
	ImportanceHigh
	// ImportanceLow makes no sound.
	ImportanceLow
	// ImportanceMin shows only in the shade, folded.
	ImportanceMin
)

// ChannelGroup is a heading over channels in the app's settings: an
// account's, in an app with more than one.
type ChannelGroup struct {
	ID   string
	Name string
}

// SetChannels sets the app's channels: it makes those it doesn't have,
// renames the others, and deletes those it made before that aren't in cs.
// The default channel ("Notifications"), which a notification without a
// channel is in, stays. Set them as the app starts (in its root
// component), before notifications that name them come: an FCM
// notification's android.notification.channel_id, a data message's
// "channel". Main thread.
func SetChannels(cs ...Channel) {
	type channel struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Importance  int    `json:"importance"`
		Silent      bool   `json:"silent,omitempty"`
		NoVibration bool   `json:"noVibration,omitempty"`
		NoBadge     bool   `json:"noBadge,omitempty"`
		Group       string `json:"group,omitempty"`
	}
	out := make([]channel, 0, len(cs))
	for _, c := range cs {
		out = append(out, channel{c.ID, c.Name, c.Description, int(c.Importance), c.Silent, c.NoVibration, c.NoBadge, c.Group})
	}
	b, _ := json.Marshal(out)
	ui.Call("notify", "channels", b, nil)
}

// SetChannelGroups sets the app's channel groups, as SetChannels does
// its channels (deleting a group deletes its channels). Set them before
// the channels in them. Main thread.
func SetChannelGroups(gs ...ChannelGroup) {
	type group struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := make([]group, 0, len(gs))
	for _, g := range gs {
		out = append(out, group{g.ID, g.Name})
	}
	b, _ := json.Marshal(out)
	ui.Call("notify", "channelGroups", b, nil)
}
