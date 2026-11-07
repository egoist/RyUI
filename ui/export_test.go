package ui

// EncodeNativeEventForTest is what a host sends for a native view's event.
var EncodeNativeEventForTest = encodeNativeEvent

// EncodeRich is the wire encoding of styled text (RichText's Content, a
// Rich field's Rich).
func EncodeRich(r Rich) []byte { return appendRich(nil, r) }
