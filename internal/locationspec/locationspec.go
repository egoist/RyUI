// Package locationspec is the wire format of asuka/sdk/location's
// permission, background updates and regions between Go and the hosts.
//
// "locationAccess" is the authorization, a followed module (package
// follow): "current", "next" (args: the last state) and "stop" answer
// with the state, three bytes (Access); "request" (one byte: WhenInUse
// or Always) asks the user and answers with the state they left;
// "precise" (a purpose key, iOS's) asks for precise positions this once
// and answers likewise.
//
// "locationBackground" is what runs with the app out of sight:
//
//   - "capabilities" answers with Capabilities, as JSON: what the system
//     does itself. What it doesn't, the Go package computes from the
//     positions it watches while the app runs.
//   - "start" (Updates) starts the background updates, "stop" stops
//     them, "updating" answers with the Updates running (JSON null:
//     none). The host keeps them across launches until "stop".
//   - "monitor" (Region) monitors a region, replacing one with its ID;
//     "unmonitor" (its ID; empty: all of them); "regions" answers with
//     those monitored (a JSON list). A host refuses one past its limit
//     with an error starting "too many regions".
//   - "next" answers with an Event, once one came: what the host keeps
//     until Go reports it handled, with "done" (its ID). The host keeps
//     them across launches (an event that came as the app was ended is
//     handed over again), and keeps the app running in the background
//     while one isn't done, for Event.Seconds at most.
//
// Errors: "denied" (the user hasn't allowed what it needs), "always"
// (it needs the Always authorization), "too many regions..." .
package locationspec

// Access levels (sdk/location's Level), the state's first byte.
const (
	NotAsked byte = iota
	Denied
	Restricted
	WhenInUse
	Always
)

// Access is the authorization on the wire: level, precise (0, 1), off
// (1: location services are off for the whole device).
func Access(level byte, precise, off bool) []byte {
	b := []byte{level, 0, 0}
	if precise {
		b[1] = 1
	}
	if off {
		b[2] = 1
	}
	return b
}

// Accuracy values (Updates.Accuracy).
const (
	AccuracyHigh = iota
	AccuracyNavigation
	AccuracyBalanced
	AccuracyLow
	AccuracyPassive
)

// Activity values (Updates.Activity): iOS's CLActivityType.
const (
	ActivityOther = iota
	ActivityAutomotive
	ActivityOtherNavigation
	ActivityFitness
	ActivityAirborne
)

// Updates are the background updates' options.
type Updates struct {
	Accuracy int `json:"accuracy"`
	// Distance is the meters between two updates (0: any move).
	Distance float64 `json:"distance,omitempty"`
	// Interval is the milliseconds wanted between updates (Android's;
	// 0: the host's default).
	Interval int64 `json:"interval,omitempty"`
	Activity int   `json:"activity,omitempty"`
	// Defer is the milliseconds an update may wait to come with others.
	Defer int64 `json:"defer,omitempty"`
	// Significant: only the significant changes (about 500 m).
	Significant bool `json:"significant,omitempty"`
	// AutoPause lets iOS pause the updates while the device doesn't move.
	AutoPause bool `json:"autoPause,omitempty"`
	// Indicator shows iOS's background location indicator.
	Indicator bool `json:"indicator,omitempty"`
	// Title and Text are Android's foreground service's notification
	// (Title "": none, no service).
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
}

// Transitions (Region.Events, Event.Kind), a bit each.
const (
	Enter = 1 << iota
	Exit
	Dwell
)

// Region is a circle monitored.
type Region struct {
	ID     string  `json:"id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Radius float64 `json:"radius"`
	// Events are the transitions reported (Enter | Exit | Dwell bits).
	Events int `json:"events"`
	// Dwell is the milliseconds inside before a Dwell event.
	Dwell int64 `json:"dwell,omitempty"`
}

// Position is a position as the hosts send them: degrees, meters, meters
// per second and degrees (negative: not known), Unix milliseconds.
type Position struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Acc float64 `json:"acc"`
	Alt float64 `json:"alt"`
	Spd float64 `json:"spd"`
	Crs float64 `json:"crs"`
	T   int64   `json:"t"`
}

// Event is what happened while the app was out of sight: positions, or
// a region's transition.
type Event struct {
	ID string `json:"id"`
	// Seconds is how long the app may run for it (0: no limit, it's in
	// use).
	Seconds   int        `json:"seconds,omitempty"`
	Positions []Position `json:"positions,omitempty"`
	Region    string     `json:"region,omitempty"`
	Kind      int        `json:"kind,omitempty"`
	At        *Position  `json:"at,omitempty"`
	T         int64      `json:"t,omitempty"`
}

// Capabilities are what the system does itself.
type Capabilities struct {
	// Updates: background updates by the system.
	Updates bool `json:"updates"`
	// Regions: regions monitored by the system, at most Limit (0: no
	// limit stated).
	Regions bool `json:"regions"`
	Limit   int  `json:"limit"`
	// Dwell: the system reports Dwell.
	Dwell bool `json:"dwell"`
	// Relaunch: an event starts the app if it isn't running.
	Relaunch bool `json:"relaunch"`
}
