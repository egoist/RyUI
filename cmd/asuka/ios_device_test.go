package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/internal/devtunnel"
)

func testDevices(t *testing.T) []iosDevice {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "devicectl", "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	devs, err := parseDevicectl(data)
	if err != nil {
		t.Fatal(err)
	}
	return devs
}

func TestParseDevicectl(t *testing.T) {
	devs := testDevices(t)
	want := []iosDevice{
		{Name: "Kim’s iPhone", UDID: "00008140-0011223344556677", Identifier: "11111111-2222-3333-4444-555555555555", Model: "iPhone 16", OS: "27.2", Type: "iPhone",
			Paired: true, State: "connected", Transport: "wired", DevMode: "enabled"},
		// Only the fields older Xcodes have.
		{Name: "Studio iPad", UDID: "00008112-000A0B0C0D0E0F10", Identifier: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", Model: "iPad Air 11-inch (M2)", OS: "18.1", Type: "iPad",
			Paired: true, State: "available", Transport: "localNetwork", DevMode: "enabled"},
		{Name: "Old iPhone", UDID: "00008101-0001020304050607", Identifier: "99999999-8888-7777-6666-555555555555", Model: "iPhone 12", OS: "17.5", Type: "iPhone",
			Paired: true, State: "unavailable", DevMode: "enabled"},
		{Name: "Guest iPhone", UDID: "00008120-0A0B0C0D0E0F1011", Identifier: "12121212-3434-5656-7878-909090909090", Model: "iPhone 15", OS: "26.0", Type: "iPhone",
			State: "available", Transport: "wired"},
		{Name: "Work iPhone", UDID: "00008130-0102030405060708", Identifier: "ABABABAB-CDCD-EFEF-0101-232323232323", Model: "iPhone 15 Pro", OS: "26.1", Type: "iPhone",
			Paired: true, State: "connected", Transport: "wired", DevMode: "disabled"},
	}
	if !reflect.DeepEqual(devs, want) {
		t.Fatalf("devices:\n%+v\nwant\n%+v", devs, want)
	}
	if s := devs[0].String(); s != "Kim’s iPhone (iPhone 16, iOS 27.2)" {
		t.Fatalf("String: %q", s)
	}
}

func TestPickIOSDevice(t *testing.T) {
	devs := testDevices(t)
	pick := func(want string, devs []iosDevice) (string, string) {
		d, err := pickIOSDevice(want, devs)
		name, msg := "", ""
		if d != nil {
			name = d.Name
		}
		if err != nil {
			msg = err.Error()
		}
		return name, msg
	}
	// By name (typed with a plain apostrophe), UDID or identifier.
	for _, want := range []string{"kim's iphone", "Kim’s iPhone", "00008140-0011223344556677", "11111111-2222-3333-4444-555555555555"} {
		if name, err := pick(want, devs); name != "Kim’s iPhone" || err != "" {
			t.Errorf("%q: %q %s", want, name, err)
		}
	}
	// A name that isn't a device's is a simulator's, maybe.
	if name, err := pick("iPhone 17 Pro", devs); name != "" || err != "" {
		t.Errorf("a simulator's name: %q %s", name, err)
	}
	// Named, but not ready: why.
	for want, why := range map[string]string{
		"Old iPhone":   "isn't reachable",
		"Guest iPhone": "isn't paired",
		"Work iPhone":  "Developer Mode off",
	} {
		if _, err := pick(want, devs); !strings.Contains(err, why) {
			t.Errorf("%s: %s", want, err)
		}
	}
	// "iphone": the one ready, else which to name, else why none is.
	if _, err := pick("iphone", devs); !strings.Contains(err, "2 devices are connected") || !strings.Contains(err, "Studio iPad") {
		t.Errorf("two ready: %s", err)
	}
	if name, err := pick("iPhone", devs[:1]); name != "Kim’s iPhone" || err != "" {
		t.Errorf("one ready: %q %s", name, err)
	}
	if name, err := pick("iphone", append([]iosDevice{devs[2]}, devs[0])); name != "Kim’s iPhone" || err != "" {
		t.Errorf("one ready, one not: %q %s", name, err)
	}
	if _, err := pick("iphone", devs[2:3]); !strings.Contains(err, "Old iPhone isn't reachable") {
		t.Errorf("one, not ready: %s", err)
	}
	if _, err := pick("iphone", devs[2:5]); !strings.Contains(err, "no device is ready") || !strings.Contains(err, "Work iPhone") {
		t.Errorf("none ready: %s", err)
	}
	if _, err := pick("iphone", nil); !strings.Contains(err, "no iPhone or iPad is connected") {
		t.Errorf("none: %s", err)
	}
}

func TestPickTeam(t *testing.T) {
	names := map[string]string{"A": "Apple Development: Kim (X1)", "B": "Apple Development: Kim (X2)", "C": "Apple Development: Lee (X3)"}
	if team, err := pickTeam([][2]string{{"A", "TEAM111111"}, {"B", "TEAM111111"}}, names); team != "TEAM111111" || err != nil {
		t.Fatalf("one team: %q %v", team, err)
	}
	if _, err := pickTeam([][2]string{{"A", "TEAM111111"}, {"C", "TEAM222222"}}, names); err == nil || !strings.Contains(err.Error(), "TEAM222222 (Apple Development: Lee (X3))") {
		t.Fatalf("two teams: %v", err)
	}
	if _, err := pickTeam(nil, names); err == nil || !strings.Contains(err.Error(), "--team") {
		t.Fatalf("none: %v", err)
	}
	// The flag, the environment, asuka.json, in that order.
	t.Setenv("ASUKA_TEAM_ID", "ENVTEAM000")
	cfg := &appConfig{}
	cfg.IOS.Team = "CFGTEAM000"
	if team, _ := devTeam("FLAGTEAM00", cfg); team != "FLAGTEAM00" {
		t.Fatalf("flag: %q", team)
	}
	if team, _ := devTeam("", cfg); team != "ENVTEAM000" {
		t.Fatalf("environment: %q", team)
	}
	t.Setenv("ASUKA_TEAM_ID", "")
	if team, _ := devTeam("", cfg); team != "CFGTEAM000" {
		t.Fatalf("asuka.json: %q", team)
	}
}

func TestOrderLANHosts(t *testing.T) {
	ifs := []lanIface{
		{"bridge100", net.ParseIP("192.168.64.1")},
		{"en7", net.ParseIP("10.0.0.5")},
		{"en0", net.ParseIP("fe80::1")},
		{"en0", net.ParseIP("192.168.1.20")},
		{"utun4", net.ParseIP("10.8.0.2")},
		{"awdl0", net.ParseIP("169.254.3.3")},
		{"gpd0", net.ParseIP("172.16.0.9")},
	}
	got := orderLANHosts(ifs, "Kims-Mac")
	want := []string{"10.0.0.5", "192.168.1.20", "172.16.0.9", "Kims-Mac.local"}
	if !slices.Equal(got, want) {
		t.Fatalf("hosts: %v", got)
	}
}

func TestDevicePlayerConfig(t *testing.T) {
	c := devicePlayerConfig("ABCDE12345")
	if c.ID != "dev.asuka.player.abcde12345" || c.shareID() != "dev.asuka.player.abcde12345.share" || c.appGroup() != "group.dev.asuka.player.abcde12345" {
		t.Fatalf("IDs: %s %s %s", c.ID, c.shareID(), c.appGroup())
	}
	if c.IOS.Usage["NSLocalNetworkUsageDescription"] == "" {
		t.Fatal("no Local Network purpose string")
	}
	s := newDevSigner("ABCDE12345", "00008140-0011223344556677", c, t.TempDir(), true, false)
	if !slices.Equal(s.ids, []string{c.ID, c.shareID(), c.widgetID()}) {
		t.Fatalf("the IDs provisioned together: %v", s.ids)
	}
	app := s.wants(c.ID)
	if _, ok := app["aps-environment"]; !ok {
		t.Fatalf("the player's entitlements: %v", app)
	}
	if g, _ := app["com.apple.security.application-groups"].([]string); !slices.Equal(g, []string{c.appGroup()}) {
		t.Fatalf("the player's App Group: %v", app)
	}
	if ext := s.wants(c.shareID()); len(ext) != 1 {
		t.Fatalf("the share extension's: %v", ext)
	}
	// A team without App Groups: no share sheet, and no group to sign with.
	s.goWithout(c.ID, []string{"aps-environment", "com.apple.security.application-groups"})
	if c.ShareTarget.on || c.appGroup() != "" {
		t.Fatal("the share target stayed without an App Group")
	}
	if _, ok := c.entitlements["aps-environment"]; ok {
		t.Fatal("kept push without its capability")
	}
}

func TestProvisionProject(t *testing.T) {
	dir := t.TempDir()
	proj, err := writeProvisionProject(dir, "ABCDE12345", []provisionTarget{
		{id: "com.example.app", entitlements: map[string]any{"aps-environment": "development", "com.apple.security.application-groups": []string{"group.com.example.app"}}},
		{id: "com.example.app.share", entitlements: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	pbx, _ := os.ReadFile(filepath.Join(proj, "project.pbxproj"))
	for _, s := range []string{`PRODUCT_BUNDLE_IDENTIFIER = "com.example.app";`, `PRODUCT_BUNDLE_IDENTIFIER = "com.example.app.share";`,
		"DEVELOPMENT_TEAM = ABCDE12345;", "CODE_SIGN_STYLE = Automatic;", "CODE_SIGN_ENTITLEMENTS = App1.entitlements;"} {
		if !strings.Contains(string(pbx), s) {
			t.Errorf("the project lacks %s", s)
		}
	}
	ents, _ := os.ReadFile(filepath.Join(dir, "App0.entitlements"))
	if !strings.Contains(string(ents), "<key>aps-environment</key>") || !strings.Contains(string(ents), "group.com.example.app") {
		t.Errorf("entitlements: %s", ents)
	}
	if runtime.GOOS != "darwin" {
		return
	}
	if _, err := exec.LookPath("xcodebuild"); err != nil {
		t.Skip("no Xcode")
	}
	// Xcode reads it: its targets, and the scheme that builds them.
	out, err := exec.Command("xcodebuild", "-list", "-project", proj).CombinedOutput()
	if err != nil {
		t.Fatalf("xcodebuild -list: %v\n%s", err, out)
	}
	for _, s := range []string{"App0", "App1", "Provision"} {
		if !strings.Contains(string(out), s) {
			t.Errorf("xcodebuild -list doesn't show %s:\n%s", s, out)
		}
	}
	out, err = exec.Command("xcodebuild", "-project", proj, "-scheme", "Provision", "-showBuildSettings", "-destination", "generic/platform=iOS").CombinedOutput()
	if err != nil {
		t.Fatalf("xcodebuild -showBuildSettings: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PRODUCT_BUNDLE_IDENTIFIER = com.example.app.share") {
		t.Errorf("build settings:\n%s", out)
	}
}

func TestXcodebuildError(t *testing.T) {
	out := []byte("Command line invocation:\n...\n/x/Provision.xcodeproj: error: No Accounts: Add a new account in Accounts settings. (in target 'App0' from project 'Provision')\n" +
		"/x/Provision.xcodeproj: error: No Accounts: Add a new account in Accounts settings. (in target 'App0' from project 'Provision')\n** BUILD FAILED **\n")
	err := xcodebuildError(out).Error()
	if strings.Count(err, "No Accounts") != 1 || !strings.Contains(err, "Xcode › Settings › Accounts") {
		t.Fatalf("error: %s", err)
	}
}

// greeter answers each line with name and the line.
func greeter(t *testing.T, name string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					io.WriteString(c, name+":"+l)
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func greet(t *testing.T, c net.Conn, line string) string {
	t.Helper()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	io.WriteString(c, line+"\n")
	l, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(l)
}

func freeLoopbackPort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// The player on a device reaches the Go code and its assets through the
// link: by tunnels, as over USB, and through the relays, as over Wi-Fi.
func TestDeviceLink(t *testing.T) {
	core, assets := greeter(t, "core"), greeter(t, "assets")
	pool := freeLoopbackPort(t) // the phone's tunnel port, here
	l := newDeviceLink(&iosDevice{Name: "Test iPhone"}, core, assets)
	l.dialUSB = func() (net.Conn, error) { return net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", pool)) }
	if err := l.start(); err != nil {
		t.Fatal(err)
	}
	defer l.close()
	spec := l.spec()
	if spec.Tunnel != deviceTunnelPort || spec.AssetsPort != deviceAssetsPort || len(spec.Remote) != len(spec.Assets) {
		t.Fatalf("spec: %+v", spec)
	}
	// Over USB.
	usb := spec
	usb.Tunnel, usb.AssetsPort = pool, freeLoopbackPort(t)
	d, err := devtunnel.NewDialer(usb)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	c, err := d.Remote()
	if err != nil {
		t.Fatal(err)
	}
	if got := greet(t, c, "hi"); got != "core:hi" {
		t.Fatalf("over USB: %q", got)
	}
	c.Close()
	if last := l.how(); last != "USB" {
		t.Fatalf("connected over %q", last)
	}
	a, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", usb.AssetsPort))
	if err != nil {
		t.Fatal(err)
	}
	if got := greet(t, a, "logo.png"); got != "assets:logo.png" {
		t.Fatalf("an asset over USB: %q", got)
	}
	a.Close()
	// Over Wi-Fi: the relays, at this machine's addresses (or its
	// loopback, without a network).
	rp := l.lnRemote.Addr().(*net.TCPAddr).Port
	wifi := devtunnel.Spec{Remote: append(spec.Remote, fmt.Sprintf("127.0.0.1:%d", rp))}
	d2, err := devtunnel.NewDialer(wifi)
	if err != nil {
		t.Fatal(err)
	}
	c, err = d2.Remote()
	if err != nil {
		t.Fatal(err)
	}
	if got := greet(t, c, "yo"); got != "core:yo" {
		t.Fatalf("over Wi-Fi: %q", got)
	}
	c.Close()
	if last := l.how(); last != "Wi-Fi" {
		t.Fatalf("connected over %q", last)
	}
}
