package bluetoothspec

import (
	"encoding/hex"
	"strings"
)

// Base is the Bluetooth Base UUID (0000xxxx-0000-1000-8000-00805f9b34fb):
// a 16- or 32-bit UUID is its first four bytes.
var Base = [16]byte{0, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0x80, 0x5f, 0x9b, 0x34, 0xfb}

// ParseUUID reads a UUID as the platforms write them: 16-bit ("180d",
// "180D", "0x180d"), 32-bit ("0000180d"), or 128-bit, with or without
// its dashes or braces ("0000180d-0000-1000-8000-00805f9b34fb").
func ParseUUID(s string) ([16]byte, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	switch len(s) {
	case 4, 8:
		var b [4]byte
		h := s
		if len(h) == 4 {
			h = "0000" + h
		}
		if _, err := hex.Decode(b[:], []byte(h)); err != nil {
			return [16]byte{}, false
		}
		u := Base
		copy(u[:4], b[:])
		return u, true
	case 36:
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return [16]byte{}, false
		}
		s = s[:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	}
	if len(s) != 32 {
		return [16]byte{}, false
	}
	var u [16]byte
	if _, err := hex.Decode(u[:], []byte(s)); err != nil {
		return [16]byte{}, false
	}
	return u, true
}

// FormatUUID is u as the wire has it: 128-bit, lower case, with dashes.
func FormatUUID(u [16]byte) string {
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:], u[10:])
	return string(b[:])
}

// Normalize is s as the wire has it (FormatUUID), or s itself if it
// isn't a UUID.
func Normalize(s string) string {
	if u, ok := ParseUUID(s); ok {
		return FormatUUID(u)
	}
	return s
}

// Short is u's 16-bit form ("180d") when it's a 16-bit UUID, else its
// wire form: what a platform that wants the short form takes.
func Short(s string) string {
	u, ok := ParseUUID(s)
	if !ok {
		return s
	}
	if u[0] == 0 && u[1] == 0 && [12]byte(u[4:]) == [12]byte(Base[4:]) {
		return hex.EncodeToString(u[2:4])
	}
	return FormatUUID(u)
}
