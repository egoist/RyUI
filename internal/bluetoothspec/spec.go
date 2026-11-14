// Package bluetoothspec is the wire format of asuka/sdk/bluetooth between
// Go and the hosts' "bluetooth" module. Every method's arguments are a
// JSON object (the Args type named below; an empty one for none), and
// every reply is JSON, unless it says otherwise. Byte values are base64,
// as encoding/json writes a []byte. UUIDs are strings, lower case, in
// the 128-bit form (0000180d-0000-1000-8000-00805f9b34fb): a host
// shortens them where its platform wants the short form, and gives
// them back long.
//
// The module's methods:
//
//   - "state" (no args) answers State: the adapter's, without asking
//     for the permission.
//   - "request" (no args) asks for the permission where the platform
//     asks (iOS's and macOS's prompt, which creating a CBCentralManager
//     shows; Android's runtime permissions: BLUETOOTH_SCAN,
//     BLUETOOTH_CONNECT and BLUETOOTH_ADVERTISE from Android 12, the
//     location permission before), then answers State.
//   - "next" (no args) answers once events are queued, with all of them
//     ([]Event, in the order they happened); "stop" fails a waiting
//     "next" with the error "stopped", and the host may drop the
//     advertisements queued then (Go stopped every scan first). A host
//     queues the events that come while no "next" waits, advertisements
//     too: a scan Go starts right after a "stop" hears its first before
//     Go's next "next".
//   - "scan" (ScanArgs) starts a scan; it answers {} once scanning, or
//     fails. Advertisements come as "advertisement" events with its ID;
//     a scan the platform stops (Bluetooth off, a failure) sends
//     "scanStopped". Several scans may run at once: a host that has one
//     scanner merges them (the union of their filters; duplicates if
//     any asks) and gives each its own advertisements (those that match
//     its Services; once per device unless it asked for duplicates).
//   - "stopScan" (ID) stops one; {} (also for one that isn't running).
//   - "stopBackgroundScan" (no args) stops the background scan
//     (ScanArgs.Background) a previous run of the app left running; {}.
//   - "choose" (ChooseArgs) shows the system's device chooser (the
//     browser's requestDevice; Android's CompanionDeviceManager) and
//     answers the Device chosen; "canceled" when the user closes it;
//     "not available: ..." where the platform has none.
//   - "known" (KnownArgs) answers []Device: those of the IDs the
//     platform still knows (iOS's retrievePeripherals(withIdentifiers:);
//     Android's getRemoteDevice; the web's getDevices; Windows' and
//     BlueZ's devices by address), to connect without scanning.
//   - "connected" (ConnectedArgs) answers []Device: those the system is
//     connected to (another app's, the system's own: a watch, a
//     keyboard), with one of the services where the platform filters by
//     them (iOS needs at least one).
//   - "connect" (ConnectArgs) connects; it answers {} once connected
//     (the services aren't discovered yet), or fails (after the
//     platform's own timeout where it has one: Android's 30 seconds
//     without AutoConnect). "disconnect" with its Conn ends a pending
//     connect: that "connect" fails with "canceled". Once connected, a
//     disconnection sends a "disconnected" event; the connection's ID
//     is then gone (Go connects again with a new one). Several
//     connections may be to one device: they share the platform's one
//     link (its handles, its subscriptions), which ends with the last.
//   - "disconnect" (Conn) disconnects, answering {} once disconnected,
//     and sends no event for it. Every call waiting on the connection
//     fails with "disconnected".
//   - "services" (Conn) discovers the device's services, their
//     characteristics and their descriptors, all of them, and answers
//     []Service with the handles the other calls name them by (a
//     host's own numbers, unique in the connection). Asked again, it
//     answers the same handles for the same attributes, discovering
//     again only after a "servicesChanged" event.
//   - "read" (Attr) reads a characteristic; it answers the value itself,
//     raw bytes (not JSON).
//   - "write" (Write) writes one; {} once written (with response: the
//     device's answer; without: once the platform took it, waiting for
//     room where it says to: iOS's canSendWriteWithoutResponse).
//   - "readDescriptor" (Attr), "writeDescriptor" (Write): a
//     descriptor's, by its handle, likewise. Writing the Client
//     Characteristic Configuration descriptor is "subscribe"'s, which
//     iOS insists on.
//   - "subscribe" (Subscribe) turns the characteristic's notifications
//     (or indications) on: the CCCD written where the platform needs
//     that done (Android, BlueZ's AcquireNotify aside, Windows); {} once
//     on. Values come as "value" events. "unsubscribe" (Attr) turns them
//     off; {}.
//   - "mtu" (Conn) answers MTU; "requestMtu" (RequestMTU) asks for one
//     (Android's requestMtu; elsewhere the platform negotiates it
//     itself, and the answer is what it is) and answers MTU.
//   - "rssi" (Conn) reads the connection's signal strength: {"rssi": n}.
//   - "priority" (Priority) asks for a connection priority (Android's
//     requestConnectionPriority); {} (elsewhere too: the system decides).
//   - "advertise" (AdvertiseArgs) starts advertising; {} once it is, or
//     fails (too much data, no peripheral role). "stopAdvertise" (ID).
//     Where advertising stops by itself: an "advertiseStopped" event.
//   - "serve" (ServeArgs) opens a GATT server with the services; {} once
//     they're added. Requests come as "read" and "write" events, which
//     Go answers with "respond"; subscriptions as "subscription"
//     events. "unserve" (ID) closes it (removes its services).
//   - "respond" (Respond) answers a request; {}.
//   - "notify" (Notify) notifies (or indicates) a characteristic's new
//     value to its subscribed centrals, or those named; {} once sent
//     (queued while the platform says it has no room: iOS's
//     peripheralManagerIsReady(toUpdateSubscribers:)).
//   - "restore" (no args) answers Restored: what the system restored
//     at launch (iOS's state restoration, the background scan's results
//     Android delivered while the app wasn't running), once; then
//     nothing ({}).
//
// Errors are strings. Those Go reads: "not available: ..." (the
// platform hasn't the part: the web's peripheral role, the chooser
// elsewhere); "off", "denied", "unsupported" (the adapter's state stops
// it); "canceled"; "disconnected"; "not found" (a handle or a device
// the host doesn't know); "att <n>: ..." (the device's ATT error, its
// code in decimal); "timeout".
package bluetoothspec

// The states, as State.State has them.
const (
	StateUnknown     = "unknown"     // not known yet: the adapter starting, resetting
	StateUnsupported = "unsupported" // no Bluetooth LE here
	StateNotAsked    = "notAsked"    // the permission not asked for yet
	StateDenied      = "denied"      // the user (or a policy) didn't allow it
	StateOff         = "off"
	StateOn          = "on"
)

// State is the adapter's, as "state" and "request" answer it and the
// "state" event carries it.
type State struct {
	State string `json:"state"`
}

// ID names a scan, an advertisement or a server, which Go numbers.
type ID struct {
	ID uint32 `json:"id"`
}

// Conn names a connection, which Go numbers ("connect" takes it).
type Conn struct {
	Conn uint32 `json:"conn"`
}

// Attr names a characteristic or a descriptor of a connection, by the
// handle "services" gave it.
type Attr struct {
	Conn   uint32 `json:"conn"`
	Handle uint32 `json:"handle"`
}

// Scan modes (Android's ScanSettings; elsewhere ignored).
const (
	ModeLowPower      = "lowPower"
	ModeBalanced      = "balanced"
	ModeLowLatency    = "lowLatency"
	ModeOpportunistic = "opportunistic"
)

// ScanArgs start a scan.
type ScanArgs struct {
	ID uint32 `json:"id"`
	// Services: only devices advertising one of them (none: every device).
	Services []string `json:"services,omitempty"`
	// Duplicates: every advertisement (iOS's
	// CBCentralManagerScanOptionAllowDuplicatesKey; Android's
	// CALLBACK_TYPE_ALL_MATCHES, its default), not once per device.
	Duplicates bool   `json:"duplicates,omitempty"`
	Mode       string `json:"mode,omitempty"`
	// Background: keep scanning with the app away and after it's gone
	// (Android: a scan with a PendingIntent, whose results while the app
	// isn't running "restore" gives; iOS: the bluetooth-central
	// background mode does it, Services needed).
	Background bool `json:"background,omitempty"`
}

// Device is a peripheral.
type Device struct {
	// ID is the platform's: iOS's and macOS's peripheral identifier (a
	// UUID, the app's own), Android's, Windows' and BlueZ's address
	// ("AA:BB:CC:DD:EE:FF"), the web's device ID.
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Event types.
const (
	EventState            = "state"
	EventAdvertisement    = "advertisement"
	EventScanStopped      = "scanStopped"
	EventDisconnected     = "disconnected"
	EventValue            = "value"
	EventServicesChanged  = "servicesChanged"
	EventRead             = "read"
	EventWrite            = "write"
	EventSubscription     = "subscription"
	EventAdvertiseStopped = "advertiseStopped"
	EventServerStopped    = "serverStopped"
	EventRestored         = "restored"
)

// Disconnection reasons ("disconnected"'s Reason).
const (
	ReasonRemote  = "remote"  // the device ended it (HCI 0x13, 0x15)
	ReasonTimeout = "timeout" // its signal was lost (supervision timeout, HCI 0x08)
	ReasonFailed  = "failed"  // the link failed (HCI 0x3e, 0x22, ...)
	ReasonOff     = "off"     // Bluetooth turned off
	ReasonLocal   = "local"   // this device ended it (the system: another app's isn't a reason)
	ReasonUnknown = "unknown"
)

// Event is something that happened, as "next" answers them. Which
// fields an event has depends on its Type:
//
//   - "state": State.
//   - "advertisement": Scan, Advertisement.
//   - "scanStopped": Scan, Error.
//   - "disconnected": Conn, Reason, Error (the platform's words), Code
//     (its status: HCI's, CoreBluetooth's error code, Android's GATT
//     status).
//   - "value": Conn, Handle, Value (a notification's or an
//     indication's).
//   - "servicesChanged": Conn (the device's services changed: "services"
//     discovers them again; old handles fail with "not found").
//   - "read": Server, Request, Central, Handle, Offset: answer with
//     "respond" (Go answers with the value from Offset on).
//   - "write": Server, Request, Central, Handle, Offset, Value, Response
//     (whether it waits for "respond"; a write without response is
//     answered by no one). A host puts a long (prepared) write's parts
//     together and sends it once executed, whole, at Offset 0, where the
//     platform lets it (Android, iOS, macOS); BlueZ and Windows hand them
//     over a part at a time, each at its Offset.
//   - "subscription": Server, Central, Handle, On (subscribed or not).
//   - "advertiseStopped": Advertisement's ID as ID, Error.
//   - "serverStopped": Server, Error.
//   - "restored": nothing: "restore" has something (sent when the
//     system relaunched the app for it while Go was already following).
type Event struct {
	Type          string         `json:"type"`
	State         string         `json:"state,omitempty"`
	Scan          uint32         `json:"scan,omitempty"`
	Advertisement *Advertisement `json:"advertisement,omitempty"`
	Conn          uint32         `json:"conn,omitempty"`
	Handle        uint32         `json:"handle,omitempty"`
	Value         []byte         `json:"value,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Error         string         `json:"error,omitempty"`
	Code          int            `json:"code,omitempty"`
	ID            uint32         `json:"id,omitempty"`
	Server        uint32         `json:"server,omitempty"`
	Request       uint32         `json:"request,omitempty"`
	Central       *Device        `json:"central,omitempty"`
	Offset        int            `json:"offset,omitempty"`
	Response      bool           `json:"response,omitempty"`
	On            bool           `json:"on,omitempty"`
}

// Advertisement is what a scan heard from a device.
type Advertisement struct {
	Device Device `json:"device"`
	RSSI   int    `json:"rssi"`
	// LocalName is the advertised name (Device.Name may be the system's
	// cached name instead).
	LocalName string   `json:"localName,omitempty"`
	Services  []string `json:"services,omitempty"`
	// Solicited are the service solicitation UUIDs.
	Solicited []string `json:"solicited,omitempty"`
	// ServiceData by service UUID; ManufacturerData by the company's
	// identifier, in decimal ("76": Apple's).
	ServiceData      map[string][]byte `json:"serviceData,omitempty"`
	ManufacturerData map[string][]byte `json:"manufacturerData,omitempty"`
	// TxPower in dBm, when advertised.
	TxPower *int `json:"txPower,omitempty"`
	// Connectable, when the platform says (Android 8, iOS, Windows' event
	// type, BlueZ doesn't).
	Connectable *bool `json:"connectable,omitempty"`
	// Time in milliseconds since 1970.
	Time int64 `json:"time"`
}

// Filter is one of a chooser's filters: a device matches it when it
// matches each of its parts that are set.
type Filter struct {
	Services   []string `json:"services,omitempty"`
	Name       string   `json:"name,omitempty"`
	NamePrefix string   `json:"namePrefix,omitempty"`
	// Manufacturer is a company identifier the device's manufacturer
	// data has, when set.
	Manufacturer *int `json:"manufacturer,omitempty"`
}

// ChooseArgs show the chooser.
type ChooseArgs struct {
	// Filters: the devices shown match one (none: every device).
	Filters []Filter `json:"filters,omitempty"`
	// OptionalServices are the other services the app uses once
	// connected (the web lets an app use only those it names).
	OptionalServices []string `json:"optionalServices,omitempty"`
}

// KnownArgs name devices by their IDs.
type KnownArgs struct {
	IDs []string `json:"ids"`
}

// ConnectedArgs filter the connected devices.
type ConnectedArgs struct {
	Services []string `json:"services,omitempty"`
}

// ConnectArgs connect.
type ConnectArgs struct {
	Conn   uint32 `json:"conn"`
	Device string `json:"device"`
	// AutoConnect waits for the device to be in range, however long
	// (Android's autoConnect; iOS's connect always does).
	AutoConnect bool `json:"autoConnect,omitempty"`
}

// Characteristic properties, as Characteristic.Properties has them:
// the Bluetooth core spec's bits.
const (
	PropBroadcast          = 0x01
	PropRead               = 0x02
	PropWriteNoResponse    = 0x04
	PropWrite              = 0x08
	PropNotify             = 0x10
	PropIndicate           = 0x20
	PropSignedWrite        = 0x40
	PropExtendedProperties = 0x80
)

// Service is a device's service, as "services" answers it.
type Service struct {
	Handle          uint32           `json:"handle"`
	UUID            string           `json:"uuid"`
	Primary         bool             `json:"primary"`
	Characteristics []Characteristic `json:"characteristics,omitempty"`
}

// Characteristic is a service's.
type Characteristic struct {
	Handle      uint32       `json:"handle"`
	UUID        string       `json:"uuid"`
	Properties  int          `json:"properties"`
	Descriptors []Descriptor `json:"descriptors,omitempty"`
}

// Descriptor is a characteristic's.
type Descriptor struct {
	Handle uint32 `json:"handle"`
	UUID   string `json:"uuid"`
}

// Write writes a value.
type Write struct {
	Conn   uint32 `json:"conn"`
	Handle uint32 `json:"handle"`
	Value  []byte `json:"value"`
	// Response: a write with response (a characteristic's; a
	// descriptor's always is).
	Response bool `json:"response,omitempty"`
}

// Subscribe turns notifications on.
type Subscribe struct {
	Conn   uint32 `json:"conn"`
	Handle uint32 `json:"handle"`
	// Indicate: indications (acknowledged), where the characteristic has
	// both; else whichever it has.
	Indicate bool `json:"indicate,omitempty"`
}

// MTU is a connection's: the ATT MTU, and the longest value a write
// takes (iOS's maximumWriteValueLength; elsewhere MTU - 3).
type MTU struct {
	MTU                  int `json:"mtu"`
	WriteWithResponse    int `json:"writeWithResponse"`
	WriteWithoutResponse int `json:"writeWithoutResponse"`
}

// RequestMTU asks for an MTU.
type RequestMTU struct {
	Conn uint32 `json:"conn"`
	MTU  int    `json:"mtu"`
}

// Connection priorities (Android's).
const (
	PriorityBalanced = "balanced"
	PriorityHigh     = "high"
	PriorityLowPower = "lowPower"
)

// Priority asks for a connection priority.
type Priority struct {
	Conn     uint32 `json:"conn"`
	Priority string `json:"priority"`
}

// RSSI is a connection's signal strength, in dBm.
type RSSI struct {
	RSSI int `json:"rssi"`
}

// AdvertiseArgs advertise the app as a peripheral.
type AdvertiseArgs struct {
	ID        uint32   `json:"id"`
	LocalName string   `json:"localName,omitempty"`
	Services  []string `json:"services,omitempty"`
	// ServiceData and ManufacturerData as Advertisement's (iOS and macOS
	// advertise only the name and the services).
	ServiceData      map[string][]byte `json:"serviceData,omitempty"`
	ManufacturerData map[string][]byte `json:"manufacturerData,omitempty"`
	// Connectable: centrals may connect (to the app's GATT server).
	Connectable    bool   `json:"connectable"`
	IncludeTxPower bool   `json:"includeTxPower,omitempty"`
	Mode           string `json:"mode,omitempty"` // as a scan's: Android's AdvertiseSettings
}

// Attribute permissions, as LocalCharacteristic.Permissions has them.
const (
	PermRead           = 0x01
	PermWrite          = 0x02
	PermReadEncrypted  = 0x04
	PermWriteEncrypted = 0x08
)

// ServeArgs open a GATT server.
type ServeArgs struct {
	ID       uint32         `json:"id"`
	Services []LocalService `json:"services"`
}

// LocalService is one of the app's services.
type LocalService struct {
	UUID            string                `json:"uuid"`
	Primary         bool                  `json:"primary"`
	Characteristics []LocalCharacteristic `json:"characteristics,omitempty"`
}

// LocalCharacteristic is one of a local service's, Go's handle naming it
// in requests, "notify" and "subscription" events.
type LocalCharacteristic struct {
	Handle      uint32 `json:"handle"`
	UUID        string `json:"uuid"`
	Properties  int    `json:"properties"`
	Permissions int    `json:"permissions"`
	// Value is a fixed value the host answers reads with itself (iOS
	// wants such a characteristic read-only); without one, reads come to
	// Go.
	Value       []byte            `json:"value,omitempty"`
	Descriptors []LocalDescriptor `json:"descriptors,omitempty"`
}

// LocalDescriptor is a local characteristic's, with its fixed value
// (the CCCD is the host's own: never one of these).
type LocalDescriptor struct {
	UUID  string `json:"uuid"`
	Value []byte `json:"value,omitempty"`
}

// Respond answers a "read" or "write" event.
type Respond struct {
	Server  uint32 `json:"server"`
	Request uint32 `json:"request"`
	Value   []byte `json:"value,omitempty"`
	// Error is an ATT error code (0: success): 0x01 to 0xff.
	Error int `json:"error,omitempty"`
}

// Notify sends a characteristic's value to its subscribers.
type Notify struct {
	Server uint32 `json:"server"`
	Handle uint32 `json:"handle"`
	Value  []byte `json:"value"`
	// Centrals are the device IDs to send it to (none: every subscriber).
	Centrals []string `json:"centrals,omitempty"`
}

// Restored is what the system restored at launch.
type Restored struct {
	// Devices are the peripherals iOS restored, connected or connecting
	// (CBCentralManagerRestoredStatePeripheralsKey): Go connects to them
	// again ("connect" with their IDs answers at once when connected).
	Devices []Device `json:"devices,omitempty"`
	// ScanServices are the services of the scan iOS restored.
	ScanServices []string `json:"scanServices,omitempty"`
	// Advertisements are those Android's background scan delivered while
	// no Go code ran.
	Advertisements []Advertisement `json:"advertisements,omitempty"`
}
