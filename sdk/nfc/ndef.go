package nfc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// TNF is a record's type name format: what its Type means.
type TNF uint8

const (
	TNFEmpty       TNF = 0 // no type, ID or payload
	TNFWellKnown   TNF = 1 // an NFC Forum well-known type: "T" text, "U" URI, "Sp" smart poster
	TNFMedia       TNF = 2 // a MIME type (RFC 2046): "text/vcard"
	TNFAbsoluteURI TNF = 3 // the type is a URI (RFC 3986)
	TNFExternal    TNF = 4 // an NFC Forum external type: "example.com:thing"
	TNFUnknown     TNF = 5 // no type
	TNFUnchanged   TNF = 6 // a chunk after the first (Decode joins them)
)

func (t TNF) String() string {
	return [...]string{"empty", "well-known", "media", "absolute URI", "external", "unknown", "unchanged", "reserved"}[min(int(t), 7)]
}

// Record is an NDEF record.
type Record struct {
	TNF     TNF
	Type    []byte
	ID      []byte
	Payload []byte
}

// Message is an NDEF message: its records, in order.
type Message []Record

// The NDEF record header's flags.
const (
	flagMB  = 0x80 // message begin
	flagME  = 0x40 // message end
	flagCF  = 0x20 // chunk
	flagSR  = 0x10 // short record: a one-byte payload length
	flagIL  = 0x08 // an ID length
	tnfMask = 0x07
)

// Encode is the message's bytes, as a tag holds them: short records where
// they fit, no chunks. An empty message is one empty record (as Android
// writes it).
func (m Message) Encode() []byte {
	if len(m) == 0 {
		return []byte{flagMB | flagME | flagSR | byte(TNFEmpty), 0, 0}
	}
	var b []byte
	for i, r := range m {
		h := byte(r.TNF) & tnfMask
		if i == 0 {
			h |= flagMB
		}
		if i == len(m)-1 {
			h |= flagME
		}
		short := len(r.Payload) < 256
		if short {
			h |= flagSR
		}
		if len(r.ID) > 0 {
			h |= flagIL
		}
		b = append(b, h, byte(len(r.Type)))
		if short {
			b = append(b, byte(len(r.Payload)))
		} else {
			b = binary.BigEndian.AppendUint32(b, uint32(len(r.Payload)))
		}
		if len(r.ID) > 0 {
			b = append(b, byte(len(r.ID)))
		}
		b = append(b, r.Type...)
		b = append(b, r.ID...)
		b = append(b, r.Payload...)
	}
	return b
}

// Check reports what makes the message one a tag can't hold: a type or
// ID longer than 255 bytes, a type, ID or payload where its TNF has none,
// a TNF past TNFUnknown.
func (m Message) Check() error {
	for i, r := range m {
		switch {
		case len(r.Type) > 255 || len(r.ID) > 255:
			return fmt.Errorf("nfc: record %d's type or ID is longer than 255 bytes", i)
		case r.TNF == TNFEmpty && (len(r.Type) > 0 || len(r.ID) > 0 || len(r.Payload) > 0):
			return fmt.Errorf("nfc: record %d is empty (TNFEmpty) but has a type, ID or payload", i)
		case r.TNF == TNFUnknown && len(r.Type) > 0:
			return fmt.Errorf("nfc: record %d's TNF is unknown but it has a type", i)
		case r.TNF >= TNFUnchanged:
			return fmt.Errorf("nfc: record %d's TNF is %d: not one a record is written with", i, r.TNF)
		case r.TNF != TNFEmpty && r.TNF != TNFUnknown && len(r.Type) == 0:
			return fmt.Errorf("nfc: record %d has no type", i)
		}
	}
	return nil
}

// ErrBadMessage: bytes that aren't an NDEF message.
var ErrBadMessage = errors.New("nfc: not an NDEF message")

// Decode reads an NDEF message's bytes: its records, chunked ones
// joined. A message of one empty record (an empty tag's) is none.
func Decode(b []byte) (Message, error) {
	var m Message
	var chunk *Record // the chunked record being joined
	for len(b) > 0 {
		if len(b) < 3 {
			return nil, ErrBadMessage
		}
		h := b[0]
		typeLen := int(b[1])
		p := 2
		var payloadLen int
		if h&flagSR != 0 {
			payloadLen = int(b[p])
			p++
		} else {
			if len(b) < p+4 {
				return nil, ErrBadMessage
			}
			n := binary.BigEndian.Uint32(b[p:])
			if uint64(n) > uint64(len(b)) {
				return nil, ErrBadMessage
			}
			payloadLen = int(n)
			p += 4
		}
		idLen := 0
		if h&flagIL != 0 {
			if len(b) < p+1 {
				return nil, ErrBadMessage
			}
			idLen = int(b[p])
			p++
		}
		if len(b) < p+typeLen+idLen+payloadLen {
			return nil, ErrBadMessage
		}
		r := Record{TNF: TNF(h & tnfMask)}
		r.Type = clone(b[p : p+typeLen])
		p += typeLen
		r.ID = clone(b[p : p+idLen])
		p += idLen
		r.Payload = clone(b[p : p+payloadLen])
		p += payloadLen
		b = b[p:]
		switch {
		case chunk != nil:
			// A middle or last chunk: unchanged, no type, no ID.
			if r.TNF != TNFUnchanged || len(r.Type) > 0 || len(r.ID) > 0 {
				return nil, ErrBadMessage
			}
			chunk.Payload = append(chunk.Payload, r.Payload...)
			if h&flagCF == 0 {
				m = append(m, *chunk)
				chunk = nil
			}
		case r.TNF == TNFUnchanged:
			return nil, ErrBadMessage
		case h&flagCF != 0:
			chunk = &r
		default:
			m = append(m, r)
		}
		if h&flagME != 0 {
			break
		}
	}
	if chunk != nil {
		return nil, ErrBadMessage
	}
	if len(m) == 1 && m[0].TNF == TNFEmpty {
		return Message{}, nil
	}
	return m, nil
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}

// Well-known types.
var (
	typeText    = []byte("T")
	typeURI     = []byte("U")
	typePoster  = []byte("Sp")
	typeAction  = []byte("act")
	typeSize    = []byte("s")
	typeTypeRec = []byte("t")
)

// Text is a well-known text record: text in the language lang (an IETF
// language tag: "en", "pt-BR"; "" is "en"), as UTF-8.
func Text(text, lang string) Record {
	if lang == "" {
		lang = "en"
	}
	p := make([]byte, 0, 1+len(lang)+len(text))
	p = append(p, byte(len(lang)&0x3f))
	p = append(p, lang...)
	p = append(p, text...)
	return Record{TNF: TNFWellKnown, Type: typeText, Payload: p}
}

// uriPrefixes are the URI record's abbreviations, by code (NFC Forum's
// URI Record Type Definition).
var uriPrefixes = [...]string{
	"", "http://www.", "https://www.", "http://", "https://", "tel:", "mailto:",
	"ftp://anonymous:anonymous@", "ftp://ftp.", "ftps://", "sftp://", "smb://",
	"nfs://", "ftp://", "dav://", "news:", "telnet://", "imap:", "rtsp://",
	"urn:", "pop:", "sip:", "sips:", "tftp:", "btspp://", "btl2cap://",
	"btgoep://", "tcpobex://", "irdaobex://", "file://", "urn:epc:id:",
	"urn:epc:tag:", "urn:epc:pat:", "urn:epc:raw:", "urn:epc:", "urn:nfc:",
}

// URI is a well-known URI record: uri, its scheme's prefix abbreviated
// ("https://" is one byte), as Android's and iOS's own make it.
func URI(uri string) Record {
	code := 0
	for i, p := range uriPrefixes {
		if p != "" && strings.HasPrefix(uri, p) && len(p) > len(uriPrefixes[code]) {
			code = i
		}
	}
	rest := uri[len(uriPrefixes[code]):]
	return Record{TNF: TNFWellKnown, Type: typeURI, Payload: append([]byte{byte(code)}, rest...)}
}

// MIME is a media record: data of the MIME type typ ("text/vcard",
// "application/json").
func MIME(typ string, data []byte) Record {
	return Record{TNF: TNFMedia, Type: []byte(strings.ToLower(typ)), Payload: data}
}

// External is an external type record, of the type domain:name
// ("example.com:thing", lowercased: NFC Forum's are case-insensitive).
func External(domain, name string, data []byte) Record {
	return Record{TNF: TNFExternal, Type: []byte(strings.ToLower(domain + ":" + name)), Payload: data}
}

// AndroidApp is an Android Application Record: on Android, a tag with
// one opens the app of the package (or its Play Store page), whatever
// else the tag holds.
func AndroidApp(pkg string) Record { return External("android.com", "pkg", []byte(pkg)) }

// Action is what a smart poster suggests doing with its URI.
type Action uint8

const (
	ActionNone Action = iota // none said
	ActionDo                 // open it, call it, send it
	ActionSave               // save it for later
	ActionEdit               // open it for editing
)

// Title is a smart poster's title in a language.
type Title struct {
	Text string
	Lang string
}

// Icon is a smart poster's image (or video).
type Icon struct {
	Type string // its MIME type: "image/png"
	Data []byte
}

// Poster is a smart poster: a URI with what to show and do with it.
type Poster struct {
	URI    string
	Titles []Title
	Action Action
	Icon   *Icon
	// Size is what the URI points to's size, in bytes (0: not said);
	// Type, its MIME type ("" not said).
	Size int
	Type string
	// Records are the poster's other records, as they are.
	Records Message
}

// SmartPoster is a smart poster record.
func SmartPoster(p Poster) Record {
	m := Message{URI(p.URI)}
	for _, t := range p.Titles {
		m = append(m, Text(t.Text, t.Lang))
	}
	if p.Action != ActionNone {
		m = append(m, Record{TNF: TNFWellKnown, Type: typeAction, Payload: []byte{byte(p.Action - 1)}})
	}
	if p.Icon != nil {
		m = append(m, MIME(p.Icon.Type, p.Icon.Data))
	}
	if p.Size > 0 {
		m = append(m, Record{TNF: TNFWellKnown, Type: typeSize, Payload: binary.BigEndian.AppendUint32(nil, uint32(p.Size))})
	}
	if p.Type != "" {
		m = append(m, Record{TNF: TNFWellKnown, Type: typeTypeRec, Payload: []byte(p.Type)})
	}
	m = append(m, p.Records...)
	return Record{TNF: TNFWellKnown, Type: typePoster, Payload: m.Encode()}
}

func (r Record) is(tnf TNF, typ []byte) bool { return r.TNF == tnf && bytes.Equal(r.Type, typ) }

// Text is a text record's text and language (UTF-8 or UTF-16, as the
// record says).
func (r Record) Text() (text, lang string, ok bool) {
	if !r.is(TNFWellKnown, typeText) || len(r.Payload) == 0 {
		return "", "", false
	}
	status := r.Payload[0]
	n := int(status & 0x3f)
	if 1+n > len(r.Payload) {
		return "", "", false
	}
	lang = string(r.Payload[1 : 1+n])
	body := r.Payload[1+n:]
	if status&0x80 == 0 {
		return string(body), lang, true
	}
	// UTF-16: a byte order mark, or big-endian.
	little := false
	if len(body) >= 2 {
		switch {
		case body[0] == 0xff && body[1] == 0xfe:
			little, body = true, body[2:]
		case body[0] == 0xfe && body[1] == 0xff:
			body = body[2:]
		}
	}
	u := make([]uint16, len(body)/2)
	for i := range u {
		if little {
			u[i] = binary.LittleEndian.Uint16(body[2*i:])
		} else {
			u[i] = binary.BigEndian.Uint16(body[2*i:])
		}
	}
	return string(utf16.Decode(u)), lang, true
}

// URI is the URI a record holds: a URI record's, an absolute URI
// record's type, a smart poster's.
func (r Record) URI() (string, bool) {
	switch {
	case r.is(TNFWellKnown, typeURI) && len(r.Payload) > 0:
		prefix := ""
		if c := int(r.Payload[0]); c < len(uriPrefixes) {
			prefix = uriPrefixes[c]
		}
		return prefix + string(r.Payload[1:]), true
	case r.TNF == TNFAbsoluteURI:
		return string(r.Type), true
	case r.is(TNFWellKnown, typePoster):
		if p, ok := r.SmartPoster(); ok {
			return p.URI, true
		}
	}
	return "", false
}

// MediaType is a media record's MIME type.
func (r Record) MediaType() (string, bool) {
	if r.TNF != TNFMedia {
		return "", false
	}
	return string(r.Type), true
}

// ExternalType is an external type record's type ("example.com:thing").
func (r Record) ExternalType() (string, bool) {
	if r.TNF != TNFExternal {
		return "", false
	}
	return string(r.Type), true
}

// SmartPoster is a smart poster record's poster.
func (r Record) SmartPoster() (Poster, bool) {
	if !r.is(TNFWellKnown, typePoster) {
		return Poster{}, false
	}
	m, err := Decode(r.Payload)
	if err != nil {
		return Poster{}, false
	}
	var p Poster
	found := false
	for _, x := range m {
		switch {
		case x.is(TNFWellKnown, typeURI):
			if u, ok := x.URI(); ok && !found {
				p.URI, found = u, true
				continue
			}
		case x.is(TNFWellKnown, typeText):
			if t, l, ok := x.Text(); ok {
				p.Titles = append(p.Titles, Title{t, l})
				continue
			}
		case x.is(TNFWellKnown, typeAction) && len(x.Payload) == 1:
			p.Action = Action(x.Payload[0] + 1)
			continue
		case x.is(TNFWellKnown, typeSize) && len(x.Payload) == 4:
			p.Size = int(binary.BigEndian.Uint32(x.Payload))
			continue
		case x.is(TNFWellKnown, typeTypeRec):
			p.Type = string(x.Payload)
			continue
		case x.TNF == TNFMedia && p.Icon == nil && (strings.HasPrefix(string(x.Type), "image/") || strings.HasPrefix(string(x.Type), "video/")):
			p.Icon = &Icon{Type: string(x.Type), Data: x.Payload}
			continue
		}
		p.Records = append(p.Records, x)
	}
	return p, found
}

// URI is the message's first record's URI, if it holds one: the one
// Android opens a tag's app with.
func (m Message) URI() (string, bool) {
	if len(m) == 0 {
		return "", false
	}
	return m[0].URI()
}

// Text is the message's first text record's text and language.
func (m Message) Text() (text, lang string, ok bool) {
	for _, r := range m {
		if t, l, ok := r.Text(); ok {
			return t, l, true
		}
	}
	return "", "", false
}
