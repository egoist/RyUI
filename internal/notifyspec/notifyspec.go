// Package notifyspec is the wire format of a notification between Go and
// the hosts: what asuka/sdk/notify schedules and asuka/sdk/push's rewrite makes,
// which each host's one builder turns into the platform's notification
// (iOS's UNNotificationContent, Android's NotificationCompat). JSON: it's
// sent once for each notification, not on a hot path.
package notifyspec

// Spec is a notification to show.
type Spec struct {
	ID       string            `json:"id,omitempty"`
	Title    string            `json:"title,omitempty"`
	Subtitle string            `json:"subtitle,omitempty"`
	Body     string            `json:"body,omitempty"`
	URL      string            `json:"url,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	// Seconds from now it shows; 0: now.
	Seconds  float64 `json:"seconds,omitempty"`
	Category string  `json:"category,omitempty"`
	Thread   string  `json:"thread,omitempty"`
	Summary  string  `json:"summary,omitempty"`
	Channel  string  `json:"channel,omitempty"`
	Badge    int     `json:"badge,omitempty"`
	Silent   bool    `json:"silent,omitempty"`
	// Image is a file's path or an http(s) URL.
	Image string `json:"image,omitempty"`
	// Sender makes it a message; Conversation is where it was said.
	Sender       *Person       `json:"sender,omitempty"`
	Conversation *Conversation `json:"conversation,omitempty"`
}

// Person is a message's sender, or the user (Conversation.Me).
type Person struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Image string `json:"image,omitempty"`
	Bot   bool   `json:"bot,omitempty"`
}

// Conversation is a message's: its ID is never empty on the wire (a 1:1
// conversation's is the sender's).
type Conversation struct {
	ID      string    `json:"id"`
	Title   string    `json:"title,omitempty"`
	Group   bool      `json:"group,omitempty"`
	Image   string    `json:"image,omitempty"`
	Me      *Person   `json:"me,omitempty"`
	Earlier []Message `json:"earlier,omitempty"`
}

// Message is one said earlier in the conversation. Sender nil: the user.
type Message struct {
	Sender *Person `json:"sender,omitempty"`
	Text   string  `json:"text"`
	// Time is in milliseconds since 1970; 0: unknown.
	Time int64 `json:"time,omitempty"`
}

// Shown is a notification a host reports: one showing (asuka/sdk/notify's
// Showing), one coming in the foreground, one the user answered.
type Shown struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle"`
	Body     string            `json:"body"`
	URL      string            `json:"url"`
	Data     map[string]string `json:"data"`
	Category string            `json:"category"`
	Thread   string            `json:"thread"`
	// Time is in milliseconds since 1970.
	Time   int64 `json:"time"`
	Remote bool  `json:"remote"`
}

// Response is the user's answer to a notification: an action's, or a tap
// (Action ""). Token names it to report it done; Seconds is how long the
// system gives the app to finish.
type Response struct {
	Token        string `json:"token"`
	Action       string `json:"action"`
	Reply        string `json:"reply"`
	Seconds      int    `json:"seconds"`
	Notification Shown  `json:"notification"`
}

// Present asks whether a notification that comes while the app is in the
// foreground shows.
type Present struct {
	Token        string `json:"token"`
	Notification Shown  `json:"notification"`
}

// Rewritten is the rewrite's answer: OK false shows the push as it came.
type Rewritten struct {
	Token string `json:"token"`
	OK    bool   `json:"ok"`
	Spec  Spec   `json:"spec"`
}
