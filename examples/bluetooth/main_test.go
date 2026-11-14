package main

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/bluetooth"
	"github.com/egoist/asuka/uitest"
)

// strap is a heart rate strap with a battery.
func strap() *fake.Peripheral {
	return &fake.Peripheral{ID: "AA:BB:CC:00:00:01", Name: "Polar H10", RSSI: -58, Advertised: []string{"180d"},
		Services: []*fake.Service{
			{UUID: "180d", Characteristics: []*fake.Characteristic{{UUID: "2a37", Properties: int(bluetooth.Notify)}}},
			{UUID: "180f", Characteristics: []*fake.Characteristic{{UUID: "2a19", Properties: int(bluetooth.Read), Value: []byte{64}}}},
		}}
}

// The scan lists what it hears, the nearest first; a device tapped
// connects, reads its battery and follows its heart rate; going back
// disconnects.
func TestScanConnectRead(t *testing.T) {
	var bt *fake.Bluetooth
	h10 := strap()
	a := uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) {
		bt = h.Bluetooth()
		bt.Add(h10)
		bt.Add(&fake.Peripheral{ID: "AA:BB:CC:00:00:02", Name: "Lamp", RSSI: -80})
	}))
	a.Expect("Bluetooth: on")
	a.Tap("Scan")
	a.WaitFor("♥ Polar H10")
	a.Expect("Lamp", "-58 dBm", "-80 dBm", "Stop")
	if got := a.Texts(); index(got, "♥ Polar H10") > index(got, "Lamp") {
		t.Errorf("not the nearest first: %q", got)
	}

	a.Tap("♥ Polar H10")
	a.WaitFor("Battery")
	a.Expect("connected", "64%")
	a.Until("subscribed to the heart rate", func() bool { return h10.Characteristic("180d", "2a37").Subscribed })
	h10.Notify("180d", "2a37", []byte{0, 61})
	a.WaitFor("61 bpm")
	h10.Notify("180d", "2a37", []byte{1, 0x2c, 0x01}) // 16-bit: 300
	a.WaitFor("300 bpm")

	a.Back()
	a.Until("disconnected", func() bool { return !h10.Connected() })
	a.Tap("Stop")
	a.Until("the scan stopped", func() bool { return len(bt.Scans) == 0 })
}

// A device that goes shows why, and the scan says when Bluetooth is off.
func TestDisconnectAndOff(t *testing.T) {
	var bt *fake.Bluetooth
	h10 := strap()
	a := uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) {
		bt = h.Bluetooth()
		bt.Add(h10)
	}))
	a.Tap("Scan")
	a.WaitFor("♥ Polar H10")
	a.Tap("♥ Polar H10")
	a.WaitFor("64%")
	bt.Remove(h10)
	a.WaitFor("Disconnected: bluetooth: disconnected: the connection timed out (the peripheral went)")
	a.Back()
	bt.SetState("off")
	a.WaitFor("Bluetooth is off")
	a.Expect("Bluetooth: off", "Scan")
}

// The chooser gives a device, or nothing where the platform has none.
func TestChoose(t *testing.T) {
	var bt *fake.Bluetooth
	a := uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) {
		bt = h.Bluetooth()
		bt.Add(strap())
	}))
	a.Tap("Choose")
	a.WaitFor("Battery") // the device's screen, connected
	a.Back()
	bt.ChooserAvailable = false
	a.Tap("Choose")
	a.WaitFor("bluetooth: not available: this platform has no device chooser")
}

// As a monitor, it serves and advertises the Heart Rate service, and
// notifies its subscribers.
func TestMonitor(t *testing.T) {
	var bt *fake.Bluetooth
	a := uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) { bt = h.Bluetooth() }))
	a.Tap("Be a monitor")
	a.WaitFor("Advertising as Asuka HR: 0 subscribed")
	if len(bt.Servers) != 1 || len(bt.Advertising) != 1 {
		t.Fatalf("servers %v, advertising %v", bt.Servers, bt.Advertising)
	}
	for _, ad := range bt.Advertising {
		if ad.LocalName != "Asuka HR" || len(ad.Services) != 1 || ad.Services[0] != bluetooth.HeartRate.String() {
			t.Errorf("advertising %+v", ad)
		}
	}
	var level []byte
	bt.CentralRead("phone", "2a19", func(v []byte, code int) { level = v })
	a.Until("the battery read", func() bool { return len(level) == 1 && level[0] == 87 })
	bt.CentralSubscribe("phone", "2a37", true)
	a.WaitFor("Advertising as Asuka HR: 1 subscribed")
	a.Until("a beat notified", func() bool { return len(bt.Notified) > 0 })
	a.Tap("Stop monitor")
	a.Until("serving stopped", func() bool { return len(bt.Servers) == 0 && len(bt.Advertising) == 0 })
}

func index(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}
