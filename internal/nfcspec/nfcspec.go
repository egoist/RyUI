// Package nfcspec is the wire format of asuka/sdk/nfc between Go and the
// hosts' "nfc" module, shared with host/fake. Arguments and answers are
// JSON (bytes as base64, as encoding/json has them); an NDEF message is
// its bytes as NFC Forum's NDEF specification lays them out, which
// Android's NdefMessage reads and writes as they are, and the Go side
// encodes and decodes (asuka/sdk/nfc).
//
// The methods:
//
//   - "state": State.
//   - "nextState" (the State last seen): answers with the State once it
//     isn't that one; "stopState" fails the waiting one with "stopped". A
//     platform whose NFC can't be turned on and off answers "not
//     available".
//   - "settings": opens the system's NFC settings (Android).
//   - "start" (Start): begins a session, answered once it has (or with
//     why it couldn't). One at a time: a host ends the one before.
//   - "next" (Session): answers with the session's next Tag, or fails
//     when the session has ended, with ErrEnded if the app ended it (or
//     it never began), else why (a "next" that comes after the end
//     too: a host keeps why the last session ended). A tag found while none is asked for
//     waits; once one is answered, the host looks for no other until
//     "restart".
//   - "restart" (Session): looks for another tag (iOS restarts polling).
//   - "alert" (Session with Alert): iOS's sheet says Alert.
//   - "stop" (Session, with Error to end it with a failure iOS's sheet
//     shows): ends it.
//   - "read", "write", "lock", "status", "transceive", "classic" (Op):
//     the tag of Op.Handle's NDEF message (its bytes; none: empty),
//     writing one, making it read-only, its NDEF status (NDEF), a raw
//     command (the answer's bytes), a MIFARE Classic operation (Op.Op;
//     a read's block).
//   - "nextOpened": answers with the next Tag the system opened the app
//     with (Android's NDEF_DISCOVERED and TECH_DISCOVERED, at launch or
//     since), the host keeping them until asked; "stopOpened" fails the
//     waiting one with "stopped". A tag whose message's URI opened the
//     app goes to ui.OnOpenURL instead.
//
// Errors are a message that starts with one of the Err words, then
// optionally ": " and more.
package nfcspec

import "errors"

// State is whether the device has NFC, and whether it's on.
type State struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

// Poll bits: what a session looks for. None: NDEF tags (iOS's
// NFCNDEFReaderSession; elsewhere, every kind).
const (
	PollA       = 1 << iota // ISO 14443-A
	PollB                   // ISO 14443-B
	PollF                   // FeliCa (ISO 18092)
	PollV                   // ISO 15693
	PollBarcode             // Android: NFC barcodes (Kovio)
	PollPACE                // iOS: PACE (identity documents)
)

// Start begins a session.
type Start struct {
	ID    uint32 `json:"id"`
	Alert string `json:"alert,omitempty"`
	Poll  uint32 `json:"poll,omitempty"`
	// SkipNDEF: the NDEF message isn't read as a tag is found (Android's
	// FLAG_READER_SKIP_NDEF_CHECK).
	SkipNDEF bool `json:"skipNDEF,omitempty"`
	// NoSound: Android's FLAG_READER_NO_PLATFORM_SOUNDS.
	NoSound bool `json:"noSound,omitempty"`
	// PresenceDelay is Android's EXTRA_READER_PRESENCE_CHECK_DELAY, in
	// milliseconds (0: the platform's).
	PresenceDelay int `json:"presenceDelay,omitempty"`
}

// Session names a session, for "next", "restart", "alert" and "stop".
type Session struct {
	ID    uint32 `json:"id"`
	Alert string `json:"alert,omitempty"`
	Error string `json:"error,omitempty"`
}

// NDEF formats.
const (
	FormatNone       = 0 // not NDEF, and can't be made so
	FormatFormatable = 1 // not NDEF yet: writing formats it (Android's NdefFormatable)
	FormatReadWrite  = 2
	FormatReadOnly   = 3
)

// NDEF is a tag's NDEF status.
type NDEF struct {
	Format int `json:"format"`
	// Capacity is the largest message it holds, in bytes; -1: not known.
	Capacity int  `json:"capacity"`
	CanLock  bool `json:"canLock,omitempty"`
	// Type is Android's (Ndef.getType: org.nfcforum.ndef.type2...).
	Type string `json:"type,omitempty"`
}

// Tag is a tag found: Handle names it in the Ops while it's held (until
// its session ends or another tag takes its place).
type Tag struct {
	Handle uint32   `json:"handle"`
	ID     []byte   `json:"id,omitempty"`
	Techs  []string `json:"techs"`
	NDEF   NDEF     `json:"ndef"`
	// Message is its NDEF message's bytes, nil if none was read.
	Message       []byte    `json:"message,omitempty"`
	MaxTransceive int       `json:"maxTransceive,omitempty"`
	NfcA          *NfcA     `json:"nfcA,omitempty"`
	NfcB          *NfcB     `json:"nfcB,omitempty"`
	ISO7816       *ISO7816  `json:"iso7816,omitempty"`
	FeliCa        *FeliCa   `json:"felica,omitempty"`
	ISO15693      *ISO15693 `json:"iso15693,omitempty"`
	MIFARE        *MIFARE   `json:"mifare,omitempty"`
	Barcode       []byte    `json:"barcode,omitempty"`
}

// NfcA is an ISO 14443-3A tag's.
type NfcA struct {
	ATQA []byte `json:"atqa,omitempty"`
	SAK  int    `json:"sak"`
}

// NfcB is an ISO 14443-3B tag's.
type NfcB struct {
	ApplicationData []byte `json:"applicationData,omitempty"`
	ProtocolInfo    []byte `json:"protocolInfo,omitempty"`
}

// ISO7816 is an ISO 7816 (ISO 14443-4) card's.
type ISO7816 struct {
	// AID is the application iOS selected, of Info.plist's (hex).
	AID             string `json:"aid,omitempty"`
	Historical      []byte `json:"historical,omitempty"`
	HiLayer         []byte `json:"hiLayer,omitempty"`
	ApplicationData []byte `json:"applicationData,omitempty"`
	Proprietary     bool   `json:"proprietary,omitempty"`
}

// FeliCa is a FeliCa card's.
type FeliCa struct {
	SystemCode []byte `json:"systemCode,omitempty"`
	IDm        []byte `json:"idm,omitempty"`
	PMm        []byte `json:"pmm,omitempty"`
}

// ISO15693 is an ISO 15693 tag's.
type ISO15693 struct {
	Manufacturer int    `json:"manufacturer"`
	Serial       []byte `json:"serial,omitempty"`
	DSFID        int    `json:"dsfid"`
	AFI          int    `json:"afi"`
}

// MIFARE is a MIFARE tag's.
type MIFARE struct {
	// Family: "ultralight", "plus", "desfire", "classic" or "unknown".
	Family string `json:"family"`
	// Type is Android's: MifareClassic's "classic", "plus", "pro";
	// MifareUltralight's "ultralight", "ultralightC".
	Type    string `json:"type,omitempty"`
	Size    int    `json:"size,omitempty"`
	Sectors int    `json:"sectors,omitempty"`
	Blocks  int    `json:"blocks,omitempty"`
}

// Op is an operation on a tag.
type Op struct {
	Handle uint32 `json:"handle"`
	// Message is "write"'s.
	Message []byte `json:"message,omitempty"`
	// Tech and Data are "transceive"'s.
	Tech string `json:"tech,omitempty"`
	Data []byte `json:"data,omitempty"`
	// "classic"'s: Op is "auth", "read", "write", "increment",
	// "decrement", "transfer" or "restore".
	Op     string `json:"op,omitempty"`
	Sector int    `json:"sector,omitempty"`
	Block  int    `json:"block,omitempty"`
	Key    []byte `json:"key,omitempty"`
	KeyB   bool   `json:"keyB,omitempty"`
	Value  int    `json:"value,omitempty"`
}

// The words an error starts with.
const (
	ErrEnded        = "ended"
	ErrStopped      = "stopped"
	ErrNotAvailable = "not available"
	ErrDisabled     = "disabled"
	ErrDenied       = "denied"
	ErrCanceled     = "canceled"
	ErrTimeout      = "timeout"
	ErrTagLost      = "tag lost"
	ErrReadOnly     = "read only"
	ErrTooLarge     = "too large"
	ErrNotSupported = "not supported"
)

// Unavailable is the "nfc" module of a host whose platform gives apps no
// NFC (a Mac, Windows, Linux): no NFC to report, and every method that
// would use it failing "not available: " and why.
func Unavailable(why string) map[string]func([]byte, func([]byte, error)) {
	fail := func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New(ErrNotAvailable+": "+why)) }
	none := func(_ []byte, reply func([]byte, error)) { reply(nil, nil) }
	m := map[string]func([]byte, func([]byte, error)){
		"state": func(_ []byte, reply func([]byte, error)) {
			reply([]byte(`{"supported":false,"enabled":false}`), nil)
		},
		"stopState": none, "stopOpened": none, "restart": none, "alert": none, "stop": none,
	}
	for _, k := range []string{"nextState", "settings", "start", "next", "read", "write", "lock", "status", "transceive", "classic", "nextOpened"} {
		m[k] = fail
	}
	return m
}
