package ops

// A Router's address, as the core sends it (the Location command, to the
// root or a Window node) and as a host gives it back (the Location event,
// the user going back or forward in a browser's history).
//
// The command is u8 LocationAction, i32 delta (Back's: how many entries
// back, negative; 0 otherwise), a u32-length href (the app's own path and
// query: /chat/7?tab=info), a u32-length state (the router's navigation
// state, which only the core reads) and a u32-length window key (the
// window's StateKey, "main" for the main window's unless it names
// another; empty: a Window that keeps none), under which a desktop host
// keeps it with where the window was. The event is the href
// and the state, u32-length each; an empty state means the host has none
// (a link pasted, an entry from before the app kept one): the core
// builds it from the href.

// Location is a Location command's arguments.
type Location struct {
	Action LocationAction
	Delta  int32
	Href   string
	State  []byte
	Window string
}

// AppendLocation appends a Location command's arguments.
func AppendLocation(b []byte, l Location) []byte {
	b = append(b, byte(l.Action))
	b = le.AppendUint32(b, uint32(l.Delta))
	b = appendStr(b, l.Href)
	b = le.AppendUint32(b, uint32(len(l.State)))
	b = append(b, l.State...)
	return appendStr(b, l.Window)
}

// DecodeLocation reads what AppendLocation writes.
func DecodeLocation(b []byte) (Location, bool) {
	r := &reader{b: b}
	var l Location
	l.Action = LocationAction(r.u8())
	l.Delta = int32(r.u32())
	l.Href = string(r.bytes())
	l.State = append([]byte(nil), r.bytes()...)
	l.Window = string(r.bytes())
	return l, r.err == nil
}

// AppendLocationEvent appends a Location event's payload: the href and
// the state the host kept with it.
func AppendLocationEvent(b []byte, href string, state []byte) []byte {
	b = appendStr(b, href)
	b = le.AppendUint32(b, uint32(len(state)))
	return append(b, state...)
}

// DecodeLocationEvent reads what AppendLocationEvent writes.
func DecodeLocationEvent(b []byte) (href string, state []byte, ok bool) {
	r := &reader{b: b}
	href = string(r.bytes())
	state = append([]byte(nil), r.bytes()...)
	return href, state, r.err == nil
}
