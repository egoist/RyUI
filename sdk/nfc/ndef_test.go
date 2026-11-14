package nfc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The bytes Android's NdefRecord.createUri and createTextRecord write.
func TestEncodeKnown(t *testing.T) {
	cases := []struct {
		m    Message
		want string
	}{
		// https://example.com: "https://" is code 4.
		{Message{URI("https://example.com")}, "d1 01 0c 55 04 6578616d706c652e636f6d"},
		// https://www. is code 2, the longest prefix.
		{Message{URI("https://www.asuka.dev/t")}, "d1 01 0c 55 02 6173756b612e6465762f74"},
		// No prefix: code 0.
		{Message{URI("geo:35.6,139.7")}, "d1 01 0f 55 00 67656f3a33352e362c3133392e37"},
		{Message{Text("hi", "en")}, "d1 01 05 54 02 656e 6869"},
		{Message{MIME("Text/Plain", []byte("x"))}, "d2 0a 01 746578742f706c61696e 78"},
		{nil, "d0 00 00"},
	}
	for _, c := range cases {
		if got := c.m.Encode(); !bytes.Equal(got, unhex(t, c.want)) {
			t.Errorf("%v: % x, want %s", c.m, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	long := bytes.Repeat([]byte("a"), 300) // a long record: a 4-byte length
	m := Message{
		URI("tel:+81312345678"),
		Text("こんにちは", "ja"),
		{TNF: TNFMedia, Type: []byte("application/octet-stream"), ID: []byte("id1"), Payload: long},
		External("Example.com", "Thing", []byte{1, 2, 3}),
		AndroidApp("dev.asuka.nfc"),
		{TNF: TNFAbsoluteURI, Type: []byte("https://example.com/schema")},
		{TNF: TNFUnknown, Payload: []byte{9}},
	}
	got, err := Decode(m.Encode())
	if err != nil {
		t.Fatal(err)
	}
	m[3].Type = []byte("example.com:thing")
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("got %+v\nwant %+v", got, m)
	}
	if u, ok := got[0].URI(); !ok || u != "tel:+81312345678" {
		t.Errorf("URI %q %v", u, ok)
	}
	if s, l, ok := got[1].Text(); !ok || s != "こんにちは" || l != "ja" {
		t.Errorf("text %q %q %v", s, l, ok)
	}
	if mt, ok := got[2].MediaType(); !ok || mt != "application/octet-stream" {
		t.Errorf("media type %q", mt)
	}
	if et, ok := got[4].ExternalType(); !ok || et != "android.com:pkg" || string(got[4].Payload) != "dev.asuka.nfc" {
		t.Errorf("external %q", et)
	}
	if u, ok := got[5].URI(); !ok || u != "https://example.com/schema" {
		t.Errorf("absolute URI %q", u)
	}
	if u, ok := got.URI(); !ok || u != "tel:+81312345678" {
		t.Errorf("message's URI %q", u)
	}
	if s, _, ok := got.Text(); !ok || s != "こんにちは" {
		t.Errorf("message's text %q", s)
	}
	if err := m.Check(); err != nil {
		t.Error(err)
	}
}

// Every URI prefix comes back as it went.
func TestURIPrefixes(t *testing.T) {
	for i, p := range uriPrefixes {
		u := p + "x"
		r := URI(u)
		if i > 0 && r.Payload[0] == 0 {
			t.Errorf("%q: not abbreviated", u)
		}
		if got, _ := r.URI(); got != u {
			t.Errorf("%q came back %q", u, got)
		}
	}
	// A code past the table's is no prefix.
	if u, _ := (Record{TNF: TNFWellKnown, Type: []byte("U"), Payload: []byte{0x40, 'a'}}).URI(); u != "a" {
		t.Errorf("reserved code: %q", u)
	}
}

func TestTextUTF16(t *testing.T) {
	for _, c := range []struct {
		name string
		body []byte
	}{
		{"big-endian", []byte{0, 'h', 0, 'i'}},
		{"BOM big", []byte{0xfe, 0xff, 0, 'h', 0, 'i'}},
		{"BOM little", []byte{0xff, 0xfe, 'h', 0, 'i', 0}},
	} {
		r := Record{TNF: TNFWellKnown, Type: []byte("T"), Payload: append([]byte{0x80 | 2, 'f', 'r'}, c.body...)}
		if s, l, ok := r.Text(); !ok || s != "hi" || l != "fr" {
			t.Errorf("%s: %q %q %v", c.name, s, l, ok)
		}
	}
	if _, _, ok := (Record{TNF: TNFWellKnown, Type: []byte("T"), Payload: []byte{5, 'e'}}).Text(); ok {
		t.Error("a language longer than the payload read")
	}
	if s, l, _ := Text("x", "").Text(); s != "x" || l != "en" {
		t.Errorf("default language: %q %q", s, l)
	}
}

func TestSmartPoster(t *testing.T) {
	p := Poster{
		URI:    "https://asuka.dev",
		Titles: []Title{{"Asuka", "en"}, {"アスカ", "ja"}},
		Action: ActionSave,
		Icon:   &Icon{Type: "image/png", Data: []byte{0x89, 'P'}},
		Size:   1234,
		Type:   "text/html",
		Records: Message{
			External("example.com", "x", []byte("y")),
		},
	}
	r := SmartPoster(p)
	m, err := Decode(Message{r}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m[0].SmartPoster()
	if !ok || !reflect.DeepEqual(got, p) {
		t.Fatalf("poster %+v, want %+v", got, p)
	}
	if u, ok := m.URI(); !ok || u != "https://asuka.dev" {
		t.Errorf("its URI %q", u)
	}
	// The action is 0, 1, 2 on the tag.
	inner, _ := Decode(r.Payload)
	for _, x := range inner {
		if string(x.Type) == "act" && x.Payload[0] != 1 {
			t.Errorf("save's byte %d", x.Payload[0])
		}
	}
}

// Chunked records are joined; a message of one empty record is none.
func TestDecodeChunks(t *testing.T) {
	// MB CF SR TNF=2 "a/b" "he", then a middle chunk "ll", then the last "o".
	b := unhex(t, "b2 03 02 612f62 6865  36 00 02 6c6c  56 00 01 6f")
	m, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || string(m[0].Payload) != "hello" || string(m[0].Type) != "a/b" {
		t.Fatalf("got %+v", m)
	}
	if m, err := Decode(unhex(t, "d0 00 00")); err != nil || m == nil || len(m) != 0 {
		t.Fatalf("empty: %v %v", m, err)
	}
	// Records after ME are left.
	if m, err := Decode(unhex(t, "d1 01 05 54 02 656e 6869 0000")); err != nil || len(m) != 1 {
		t.Fatalf("trailing: %v %v", m, err)
	}
}

func TestDecodeBad(t *testing.T) {
	for _, s := range []string{
		"d1",                               // short
		"d1 01 09 55 04 6578",              // a payload past the end
		"c1 01 ffffffff 55",                // a long length past the end
		"d9 01 01",                         // an ID length past the end
		"b2 03 02 612f62 6865",             // a chunk never finished
		"b2 03 02 612f62 6865 d1 01 00 55", // a chunk followed by a typed record
		"d6 00 00",                         // unchanged, alone
	} {
		if _, err := Decode(unhex(t, s)); !errors.Is(err, ErrBadMessage) {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestCheck(t *testing.T) {
	bad := []Message{
		{{TNF: TNFEmpty, Payload: []byte{1}}},
		{{TNF: TNFUnknown, Type: []byte("x")}},
		{{TNF: TNFUnchanged}},
		{{TNF: TNFMedia}},
		{{TNF: TNFExternal, Type: bytes.Repeat([]byte("a"), 256)}},
	}
	for _, m := range bad {
		if m.Check() == nil {
			t.Errorf("%+v passed", m)
		}
	}
	if err := (Message{{}, URI("x")}).Check(); err != nil {
		t.Error(err)
	}
}

func TestAPDU(t *testing.T) {
	cases := []struct {
		a    APDU
		want string
	}{
		{APDU{CLA: 0, INS: 0xb0, P1: 0, P2: 0}, "00b00000"},
		{APDU{CLA: 0, INS: 0xb0, Le: 256}, "00b0000000"},
		{SelectAID([]byte{0xd2, 0x76, 0, 0, 0x85, 1, 1}), "00a4040007d276000085010100"},
		{APDU{INS: 0xd6, Data: bytes.Repeat([]byte{1}, 300)}, "00d60000 00012c" + strings.Repeat("01", 300)},
		{APDU{INS: 0xb0, Le: 65536}, "00b00000 000000"},
		{APDU{INS: 0xb0, Data: []byte{7}, Le: 300}, "00b00000 000001 07 012c"},
	}
	for _, c := range cases {
		if got := c.a.Encode(); !bytes.Equal(got, unhex(t, c.want)) {
			t.Errorf("%+v: % x, want %s", c.a, got, c.want)
		}
	}
	r := Response{SW1: 0x90}
	if !r.OK() || r.Status() != 0x9000 {
		t.Error("90 00 isn't OK")
	}
}
