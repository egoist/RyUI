package bluetooth

import (
	"encoding/binary"
	"fmt"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
)

// UUID names a service, a characteristic or a descriptor: 128 bits, of
// which the Bluetooth SIG's assigned numbers are 16 (UUID16) on the
// Bluetooth Base UUID. Comparable: a map key, a switch's case.
type UUID [16]byte

// UUID16 is a 16-bit assigned number's UUID: UUID16(0x180d) is the Heart
// Rate service, 0000180d-0000-1000-8000-00805f9b34fb.
func UUID16(n uint16) UUID { return UUID32(uint32(n)) }

// UUID32 is a 32-bit UUID's, on the Bluetooth Base UUID as UUID16's.
func UUID32(n uint32) UUID {
	u := UUID(spec.Base)
	binary.BigEndian.PutUint32(u[:4], n)
	return u
}

// ParseUUID reads a UUID: 16-bit ("180d", "0x180D"), 32-bit
// ("0000180d"), or 128-bit, with or without dashes
// ("6e400001-b5a3-f393-e0a9-e50e24dcca9e").
func ParseUUID(s string) (UUID, error) {
	u, ok := spec.ParseUUID(s)
	if !ok {
		return UUID{}, fmt.Errorf("bluetooth: %q isn't a UUID", s)
	}
	return UUID(u), nil
}

// MustParseUUID is ParseUUID, panicking if s isn't a UUID: for a
// package's variables.
func MustParseUUID(s string) UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// String is the UUID's 128-bit form, lower case.
func (u UUID) String() string { return spec.FormatUUID(u) }

// Short is the 16-bit assigned number u is, if it's one.
func (u UUID) Short() (uint16, bool) {
	if u[0] != 0 || u[1] != 0 || [12]byte(u[4:]) != [12]byte(spec.Base[4:]) {
		return 0, false
	}
	return binary.BigEndian.Uint16(u[2:4]), true
}

// MarshalText is String's.
func (u UUID) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

// UnmarshalText is ParseUUID's.
func (u *UUID) UnmarshalText(b []byte) error {
	v, err := ParseUUID(string(b))
	if err != nil {
		return err
	}
	*u = v
	return nil
}

func uuids(us []UUID) []string {
	if len(us) == 0 {
		return nil
	}
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.String()
	}
	return out
}

func parseUUIDs(ss []string) []UUID {
	if len(ss) == 0 {
		return nil
	}
	out := make([]UUID, 0, len(ss))
	for _, s := range ss {
		if u, ok := spec.ParseUUID(s); ok {
			out = append(out, UUID(u))
		}
	}
	return out
}

// Some of the Bluetooth SIG's assigned numbers (Assigned Numbers, 3.4 to
// 3.8): services, characteristics and descriptors.
var (
	GenericAccess      = UUID16(0x1800)
	GenericAttribute   = UUID16(0x1801)
	ImmediateAlert     = UUID16(0x1802)
	LinkLoss           = UUID16(0x1803)
	TxPowerService     = UUID16(0x1804)
	CurrentTime        = UUID16(0x1805)
	DeviceInformation  = UUID16(0x180a)
	HeartRate          = UUID16(0x180d)
	Battery            = UUID16(0x180f)
	BloodPressure      = UUID16(0x1810)
	HumanInterface     = UUID16(0x1812)
	CyclingSpeed       = UUID16(0x1816)
	CyclingPower       = UUID16(0x1818)
	LocationNavigation = UUID16(0x1819)
	EnvironmentSensing = UUID16(0x181a)
	BodyComposition    = UUID16(0x181b)
	WeightScale        = UUID16(0x181d)
	FitnessMachine     = UUID16(0x1826)

	DeviceName            = UUID16(0x2a00)
	Appearance            = UUID16(0x2a01)
	ServiceChanged        = UUID16(0x2a05)
	AlertLevel            = UUID16(0x2a06)
	TxPowerLevel          = UUID16(0x2a07)
	BatteryLevel          = UUID16(0x2a19)
	SystemID              = UUID16(0x2a23)
	ModelNumber           = UUID16(0x2a24)
	SerialNumber          = UUID16(0x2a25)
	FirmwareRevision      = UUID16(0x2a26)
	HardwareRevision      = UUID16(0x2a27)
	SoftwareRevision      = UUID16(0x2a28)
	ManufacturerName      = UUID16(0x2a29)
	HeartRateMeasurement  = UUID16(0x2a37)
	BodySensorLocation    = UUID16(0x2a38)
	HeartRateControlPoint = UUID16(0x2a39)
	Temperature           = UUID16(0x2a6e)
	Humidity              = UUID16(0x2a6f)

	ExtendedProperties                = UUID16(0x2900)
	UserDescription                   = UUID16(0x2901)
	ClientCharacteristicConfiguration = UUID16(0x2902)
	PresentationFormat                = UUID16(0x2904)
)
