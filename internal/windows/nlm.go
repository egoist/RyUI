//go:build windows

package windows

import (
	"errors"
	"strings"
	"unsafe"
)

// The network, as the Network List Manager sees it: connectivity, the
// connections' adapters (Wi-Fi, cellular, a cable), the cost, and
// connectivity changes (INetworkListManagerEvents, through a connection
// point, on this thread's message loop).

var (
	clsidNetworkListManager = NewGUID("DCB00C01-570F-4A9B-8D69-199FDBA5723B")
	iidNetworkListManager   = NewGUID("DCB00000-570F-4A9B-8D69-199FDBA5723B")
	iidNetworkListEvents    = NewGUID("DCB00001-570F-4A9B-8D69-199FDBA5723B")
	iidNetworkCostManager   = NewGUID("DCB00008-570F-4A9B-8D69-199FDBA5723B")
	iidConnectionPointCont  = NewGUID("B196B284-BAB4-101A-B69C-00AA00341D07")

	iphlpapi                 = systemDLL("iphlpapi.dll")
	procGetAdaptersAddresses = iphlpapi.NewProc("GetAdaptersAddresses")
)

// NetState is the network now: connected, its kind (0 none, 1 Wi-Fi, 2
// cellular, 3 a cable, 4 another), metered, constrained (over or near
// its data limit).
type NetState struct {
	Connected, Metered, Constrained bool
	Type                            byte
}

// NetworkList is the Network List Manager, made on this thread.
type NetworkList struct {
	nlm     uintptr
	cookies [1]uint32
	points  [1]uintptr
}

// NewNetworkList makes the manager; changed, if not nil, is called (on
// this thread) as connectivity changes.
func NewNetworkList(changed func()) (*NetworkList, error) {
	var nlm uintptr
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidNetworkListManager)), 0, 0x17, // CLSCTX_ALL
		uintptr(unsafe.Pointer(&iidNetworkListManager)), uintptr(unsafe.Pointer(&nlm)))
	if Failed(hr) || nlm == 0 {
		return nil, errors.New("the Network List Manager isn't there")
	}
	l := &NetworkList{nlm: nlm}
	if changed != nil {
		cpc := QueryInterface(nlm, &iidConnectionPointCont)
		if cpc != 0 {
			// Connectivity's events (one method, as a delegate's). The cost's
			// interface has two.
			for i, iid := range []GUID{iidNetworkListEvents} {
				var cp uintptr
				if Failed(ComCall(cpc, 4, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&cp)))) || cp == 0 { // FindConnectionPoint
					continue
				}
				sink := NewDelegate(iid, func(a, b uintptr) { changed() })
				if Failed(ComCall(cp, 5, sink, uintptr(unsafe.Pointer(&l.cookies[i])))) { // Advise
					Release(cp)
				} else {
					l.points[i] = cp
				}
				Release(sink)
			}
			Release(cpc)
		}
	}
	return l, nil
}

// Close stops the changes and lets the manager go.
func (l *NetworkList) Close() {
	for i, cp := range l.points {
		if cp != 0 {
			ComCall(cp, 6, uintptr(l.cookies[i])) // Unadvise
			Release(cp)
		}
	}
	Release(l.nlm)
}

// NLM_CONNECTIVITY's and NLM_CONNECTION_COST's bits.
const (
	nlmConnected = 0x10 | 0x20 | 0x40 | 0x100 | 0x200 | 0x400 // IPv4 and IPv6 subnet, local network, internet
	nlmInternet  = 0x40 | 0x400
	costFixed    = 0x2
	costVariable = 0x4
	costOver     = 0x10000
	costRoaming  = 0x40000
	costNear     = 0x80000
)

// State is the network now.
func (l *NetworkList) State() NetState {
	var conn uint32
	ComCall(l.nlm, 13, uintptr(unsafe.Pointer(&conn))) // GetConnectivity
	s := NetState{Connected: conn&nlmConnected != 0}
	if !s.Connected {
		return s
	}
	s.Type = 4
	// The connection with the internet, else any: its adapter's kind.
	if adapter, ok := l.primaryAdapter(); ok {
		s.Type = adapterKind(adapter)
	}
	if cm := QueryInterface(l.nlm, &iidNetworkCostManager); cm != 0 {
		var cost uint32
		if !Failed(ComCall(cm, 3, uintptr(unsafe.Pointer(&cost)), 0)) { // GetCost
			s.Metered = cost&(costFixed|costVariable|costRoaming|costOver) != 0
			s.Constrained = cost&(costOver|costNear) != 0
		}
		Release(cm)
	}
	return s
}

// primaryAdapter is the adapter of the connection to the internet (else
// of the first connected one).
func (l *NetworkList) primaryAdapter() (GUID, bool) {
	var enum uintptr
	if Failed(ComCall(l.nlm, 9, uintptr(unsafe.Pointer(&enum)))) || enum == 0 { // GetNetworkConnections
		return GUID{}, false
	}
	defer Release(enum)
	var best GUID
	found := false
	for range 64 {
		var c uintptr
		var got uint32
		if ComCall(enum, 8, 1, uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&got))) != 0 || got == 0 || c == 0 { // Next
			break
		}
		var conn uint32
		var id GUID
		ComCall(c, 10, uintptr(unsafe.Pointer(&conn))) // GetConnectivity
		ComCall(c, 12, uintptr(unsafe.Pointer(&id)))   // GetAdapterId
		Release(c)
		if conn&nlmInternet != 0 {
			return id, true
		}
		if conn&nlmConnected != 0 && !found {
			best, found = id, true
		}
	}
	return best, found
}

// adapterKind is an adapter's kind, from its interface type
// (GetAdaptersAddresses).
func adapterKind(id GUID) byte {
	size := uint32(16 << 10)
	var buf []byte
	for range 3 {
		buf = make([]byte, size)
		r, _, _ := procGetAdaptersAddresses.Call(0, 0x0e, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))) // AF_UNSPEC; no unicast, anycast, multicast, DNS
		if r == 0 {
			break
		}
		if r != 111 { // ERROR_BUFFER_OVERFLOW
			return 4
		}
	}
	want := strings.ToLower(id.Signature())
	for p := uintptr(unsafe.Pointer(&buf[0])); p != 0; p = *(*uintptr)(native(p + 8)) {
		name := strings.ToLower(GoAnsi(*(*uintptr)(native(p + 16))))
		if name != want {
			continue
		}
		switch *(*uint32)(native(p + 100)) { // IfType
		case 71: // IEEE 802.11
			return 1
		case 243, 244: // WWAN
			return 2
		case 6: // Ethernet
			return 3
		}
		return 4
	}
	return 4
}

// GoAnsi reads a NUL-terminated ANSI string.
func GoAnsi(p uintptr) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); i < 1024; i++ {
		c := *(*byte)(native(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}
