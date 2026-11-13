package ops

// An Alert's text fields as the core sends them (its Fields prop), and
// what a host reports as the user types in one (FieldChanged), read by
// every host with DecodeAlertFields (the Swift, Kotlin and JavaScript
// hosts with their own readers of the same encoding).
//
// Fields is u32 count, then per field a u32-length placeholder, a
// u32-length text (the field's starting text), u8 AlertFieldFlags, u8
// Keyboard and a u32-length content type (AutoFill's hint, as a
// TextField's ContentType: "username", "password"...).
//
// FieldChanged is u32 the field's index, then its text, u32-length.

// AlertFieldFlags are an alert field's options.
type AlertFieldFlags uint8

const (
	// AlertFieldSecure hides what's typed: a password.
	AlertFieldSecure AlertFieldFlags = 1
)

// AlertField is a text field of an alert.
type AlertField struct {
	Placeholder string
	Text        string // its starting text
	Flags       AlertFieldFlags
	Keyboard    Keyboard
	ContentType string
}

// Secure reports whether the field hides what's typed.
func (f *AlertField) Secure() bool { return f.Flags&AlertFieldSecure != 0 }

// AppendAlertFields appends an alert's fields.
func AppendAlertFields(b []byte, fs []AlertField) []byte {
	b = le.AppendUint32(b, uint32(len(fs)))
	for i := range fs {
		f := &fs[i]
		b = appendStr(b, f.Placeholder)
		b = appendStr(b, f.Text)
		b = append(b, byte(f.Flags), byte(f.Keyboard))
		b = appendStr(b, f.ContentType)
	}
	return b
}

// DecodeAlertFields reads what AppendAlertFields writes; what's cut
// short is left out.
func DecodeAlertFields(b []byte) []AlertField {
	if len(b) < 4 {
		return nil
	}
	r := &reader{b: b}
	n := r.u32()
	var out []AlertField
	for i := uint32(0); i < n && r.err == nil; i++ {
		var f AlertField
		f.Placeholder = string(r.bytes())
		f.Text = string(r.bytes())
		f.Flags = AlertFieldFlags(r.u8())
		f.Keyboard = Keyboard(r.u8())
		f.ContentType = string(r.bytes())
		if r.err != nil {
			break
		}
		out = append(out, f)
	}
	return out
}

// AppendFieldChanged appends FieldChanged's payload: field index's text.
func AppendFieldChanged(b []byte, index int, text string) []byte {
	b = le.AppendUint32(b, uint32(index))
	return appendStr(b, text)
}

// DecodeFieldChanged reads FieldChanged's payload; ok is false when
// it's cut short.
func DecodeFieldChanged(b []byte) (index int, text string, ok bool) {
	r := &reader{b: b}
	i := r.u32()
	s := r.bytes()
	if r.err != nil {
		return 0, "", false
	}
	return int(i), string(s), true
}

// DefaultAlertAction is the number of an alert's default button, which
// return in its last field presses: the first whose role is Default
// (neither Cancel nor Destructive), as UIAlertController's
// preferredAction; -1 if there's none. A host presses it only if it's
// enabled.
func DefaultAlertAction(items []ActionItem) int {
	for i := range items {
		if items[i].Role == AlertButtonRoleDefault {
			return items[i].Index
		}
	}
	return -1
}
