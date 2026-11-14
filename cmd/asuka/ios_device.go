package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/devtunnel"
	"github.com/egoist/asuka/internal/usbmux"
)

// asuka dev on an iPhone or iPad: devicectl (Xcode's CoreDevice) finds,
// installs on and launches the device, by USB or Wi-Fi. The player, or
// the app itself, is built for iphoneos and signed for development with
// the team's profiles, which Xcode makes as needed (xcodebuild's
// automatic provisioning). The player reaches the Go code on this Mac
// through tunnels over USB (package devtunnel, through usbmuxd), else
// over the local network.

// The player's ports on the phone, on its loopback: the app's asset
// URLs name the first, as on the simulator; the Mac's tunnels arrive on
// the second.
const (
	deviceAssetsPort = devicePort + 1
	deviceTunnelPort = devicePort + 2
)

// deviceKeyword is what --device takes for the one iPhone or iPad
// connected, whatever its name.
const deviceKeyword = "iphone"

// An iosDevice is a physical iPhone or iPad, as devicectl lists it.
type iosDevice struct {
	Name, UDID, Identifier string
	Model, OS, Type        string // "iPhone 16", "27.2", "iPhone"
	Paired                 bool
	// State is "connected" (CoreDevice's tunnel is up), "available" (it
	// can be) or "unavailable" (not reachable: unplugged, off the
	// network).
	State string
	// Transport is "wired", "localNetwork" or "" (not connected).
	Transport string
	// DevMode is "enabled", "disabled" or "" (unknown: an older iOS).
	DevMode string
}

func (d *iosDevice) String() string {
	s := d.Name
	if d.Model != "" {
		s += " (" + d.Model
		if d.OS != "" {
			s += ", iOS " + d.OS
		}
		s += ")"
	}
	return s
}

// parseDevicectl reads devicectl list devices' JSON: the physical iPhones
// and iPads in it (Xcode 26 lists the simulators too). It reads both the
// current properties and the fields older Xcodes have.
func parseDevicectl(data []byte) ([]iosDevice, error) {
	var doc struct {
		Result struct {
			Devices []struct {
				Identifier           string
				ConnectionProperties struct {
					PairingState, TunnelState, TransportType string
				}
				DeviceProperties struct {
					Name, OSVersionNumber, DeveloperModeStatus string
				}
				HardwareProperties struct {
					UDID, Reality, Platform, DeviceType, MarketingName string
				}
				Properties struct {
					Connection struct {
						PairingState, State, TransportType string
					}
					Hardware struct {
						UDID, Reality, Platform, DeviceType, MarketingName string
					}
					Software struct {
						OSVersionNumber struct{ StringValue string }
					}
					State struct {
						Name                string
						DeveloperModeStatus map[string]any
					}
				}
			}
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("devicectl's devices: %v", err)
	}
	var out []iosDevice
	for _, x := range doc.Result.Devices {
		p, hw, c := x.Properties, x.HardwareProperties, x.ConnectionProperties
		reality := firstOf(p.Hardware.Reality, hw.Reality)
		platform := firstOf(p.Hardware.Platform, hw.Platform)
		kind := firstOf(p.Hardware.DeviceType, hw.DeviceType)
		if reality != "physical" || platform != "iOS" || (kind != "iPhone" && kind != "iPad" && kind != "iPod") {
			continue
		}
		d := iosDevice{
			Name:       firstOf(p.State.Name, x.DeviceProperties.Name),
			UDID:       firstOf(p.Hardware.UDID, hw.UDID),
			Identifier: x.Identifier,
			Model:      firstOf(p.Hardware.MarketingName, hw.MarketingName),
			OS:         firstOf(p.Software.OSVersionNumber.StringValue, x.DeviceProperties.OSVersionNumber),
			Type:       kind,
			Paired:     firstOf(p.Connection.PairingState, c.PairingState) == "paired",
			Transport:  firstOf(p.Connection.TransportType, c.TransportType),
			DevMode:    x.DeviceProperties.DeveloperModeStatus,
		}
		for k := range p.State.DeveloperModeStatus {
			d.DevMode = k // {"enabled": {...}}
		}
		// The tunnel's state, else the connection's: devicectl's
		// "connected", "available (paired)" and "unavailable".
		switch s := firstOf(c.TunnelState, p.Connection.State); s {
		case "connected":
			d.State = "connected"
		case "", "unavailable":
			d.State = "unavailable"
		default: // "disconnected", "available": reachable, the tunnel not up
			d.State = "available"
		}
		out = append(out, d)
	}
	return out, nil
}

// listIOSDevices lists the physical devices devicectl knows.
func listIOSDevices() ([]iosDevice, error) {
	data, err := devicectlJSON("list", "devices")
	if err != nil {
		return nil, err
	}
	return parseDevicectl(data)
}

// devicectlJSON runs a devicectl command and returns its JSON output.
func devicectlJSON(args ...string) ([]byte, error) {
	f, err := os.CreateTemp("", "asuka-devicectl-*.json")
	if err != nil {
		return nil, err
	}
	f.Close()
	defer os.Remove(f.Name())
	out, err := exec.Command("xcrun", append(append([]string{"devicectl"}, args...), "--json-output", f.Name(), "-q")...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("devicectl %s: %s", strings.Join(args, " "), devicectlError(out, err))
	}
	return os.ReadFile(f.Name())
}

// devicectlError is what a failed devicectl said: its error lines.
func devicectlError(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		if errors.Is(err, exec.ErrNotFound) {
			return "xcrun isn't installed (install Xcode)"
		}
		return err.Error()
	}
	if strings.Contains(s, "unable to find utility \"devicectl\"") {
		return "devicectl isn't installed: asuka dev on a device needs Xcode 15 or later"
	}
	return s
}

// nameKey is a device's name as typed: devicectl's names have a
// typographic apostrophe ("Kim’s iPhone") a keyboard doesn't.
func nameKey(s string) string {
	return strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(strings.TrimSpace(s)))
}

// pickIOSDevice returns the device want names (its name, UDID or
// CoreDevice identifier), or for deviceKeyword the one connected; nil,
// nil if want names none (it may be a simulator's name).
func pickIOSDevice(want string, devs []iosDevice) (*iosDevice, error) {
	if nameKey(want) != deviceKeyword {
		for i := range devs {
			d := &devs[i]
			if strings.EqualFold(d.UDID, want) || strings.EqualFold(d.Identifier, want) || nameKey(d.Name) == nameKey(want) {
				return d, usable(d)
			}
		}
		return nil, nil
	}
	var ok []*iosDevice
	var problems []error
	for i := range devs {
		if err := usable(&devs[i]); err != nil {
			problems = append(problems, err)
		} else {
			ok = append(ok, &devs[i])
		}
	}
	switch {
	case len(ok) == 1:
		return ok[0], nil
	case len(ok) > 1:
		var names []string
		for _, d := range ok {
			names = append(names, fmt.Sprintf("%q (%s)", d.Name, d.UDID))
		}
		return nil, fmt.Errorf("%d devices are connected: name one with --device: %s", len(ok), strings.Join(names, ", "))
	case len(problems) == 1:
		return nil, problems[0]
	case len(problems) > 1:
		return nil, fmt.Errorf("no device is ready: %v", errors.Join(problems...))
	}
	return nil, errors.New("no iPhone or iPad is connected: plug one in by USB and unlock it (the first time, tap Trust on it); a device that has been paired with this Mac in Xcode's Devices window works over Wi-Fi too")
}

// usable says what keeps asuka dev from running on d, if anything.
func usable(d *iosDevice) error {
	switch {
	case !d.Paired:
		return fmt.Errorf("%s isn't paired with this Mac: plug it in by USB, unlock it and tap Trust (or pair it in Xcode's Devices window, Window › Devices and Simulators)", d.Name)
	case d.State == "unavailable":
		return fmt.Errorf("%s isn't reachable: plug it in by USB, or wake and unlock it on this Mac's Wi-Fi network", d.Name)
	case d.DevMode == "disabled":
		return fmt.Errorf("%s has Developer Mode off: turn it on in its Settings › Privacy & Security › Developer Mode, and restart it as it asks", d.Name)
	}
	return nil
}

// findIOSDevice returns the physical device --device names, nil for a
// simulator (no --device, or a simulator's name).
func findIOSDevice(want string) (*iosDevice, error) {
	if want == "" {
		return nil, nil
	}
	devs, err := listIOSDevices()
	if err != nil {
		if nameKey(want) == deviceKeyword {
			return nil, err
		}
		return nil, nil // the simulators say whether there's one of that name
	}
	return pickIOSDevice(want, devs)
}

// Teams

// devTeam is the team that signs for a device: --team, ASUKA_TEAM_ID,
// asuka.json's ios.team, else the one of the keychain's Apple
// Development identities.
func devTeam(flag string, cfg *appConfig) (string, error) {
	if t := firstOf(flag, os.Getenv("ASUKA_TEAM_ID"), cfg.IOS.Team); t != "" {
		return t, nil
	}
	simulatorIdentity("") // reads the keychain's Apple Development identities
	return pickTeam(simIdentities, identities())
}

// pickTeam is the one team of the identities (SHA-1 and team), whose
// names are names (by SHA-1).
func pickTeam(ids [][2]string, names map[string]string) (string, error) {
	var teams []string
	var which []string
	for _, id := range ids {
		if id[1] == "" || slices.Contains(teams, id[1]) {
			continue
		}
		teams = append(teams, id[1])
		which = append(which, fmt.Sprintf("%s (%s)", id[1], names[id[0]]))
	}
	switch len(teams) {
	case 1:
		return teams[0], nil
	case 0:
		return "", errors.New("no Apple Development certificate in the keychain to sign for the device: add your Apple ID in Xcode › Settings › Accounts (a free one works), then name its team with --team TEAMID or asuka.json's ios.team (Xcode makes the certificate as it signs)")
	}
	sort.Strings(which)
	return "", fmt.Errorf("the keychain has Apple Development certificates of %d teams: pick one with --team, ASUKA_TEAM_ID or asuka.json's ios.team: %s", len(teams), strings.Join(which, ", "))
}

// Signing

// iosDevBuild is buildIOS's development build for a device.
type iosDevBuild struct {
	signer *devSigner
	bundle string // the signed .app buildIOS made
}

// pickProfile is buildIOS's profile for the bundle ID id: for a device,
// the signer's (made if needed).
func (o *buildOptions) pickProfile(team, id, want string) (*profile, error) {
	if o.dev != nil {
		return o.dev.signer.profile(id)
	}
	return pickProfile(team, id, want, o.distribution)
}

// devSigner finds the development profiles for a device, and has Xcode
// make them when they're missing or lack the device or a capability.
type devSigner struct {
	team, udid string
	cfg        *appConfig
	ids        []string // the bundle IDs to provision together
	// optional: the player, which goes without the capabilities the
	// team's profiles don't have (a free team has no push).
	optional bool
	// work is where the provisioning project goes.
	work    string
	verbose bool
	tried   map[string]bool // provisioned this session
	bare    bool            // provisioned without capabilities (optional)
	told    map[string]bool
}

func newDevSigner(team, udid string, cfg *appConfig, work string, optional, verbose bool) *devSigner {
	s := &devSigner{team: team, udid: udid, cfg: cfg, work: work, optional: optional, verbose: verbose, tried: map[string]bool{}, told: map[string]bool{}}
	s.ids = []string{cfg.ID}
	if cfg.ShareTarget.on {
		s.ids = append(s.ids, cfg.shareID())
	}
	if cfg.widgetExtension() {
		s.ids = append(s.ids, cfg.widgetID())
	}
	if cfg.pushRewrite {
		s.ids = append(s.ids, cfg.notificationID())
	}
	return s
}

// wants is what the bundle ID id's profile must grant: the entitlements
// asked of Xcode for it.
func (s *devSigner) wants(id string) map[string]any {
	m := map[string]any{}
	if g := s.cfg.appGroup(); g != "" {
		m["com.apple.security.application-groups"] = []string{g}
	}
	if id != s.cfg.ID {
		return m // an extension: the App Group alone
	}
	if ds := s.cfg.associatedDomains(false); len(ds) > 0 {
		m["com.apple.developer.associated-domains"] = ds
	}
	for k, v := range s.cfg.entitlements {
		m[k] = v
	}
	return m
}

// lacks are the entitlements of want that the profile p doesn't grant.
func lacks(p *profile, want map[string]any) []string {
	have, err := profileEntitlements(p.path)
	if err != nil {
		return nil // the signature says so
	}
	var missing []string
	for k, v := range want {
		h, ok := have[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		if k == "com.apple.security.application-groups" {
			hs, _ := h.([]any)
			for _, g := range v.([]string) {
				if !slices.Contains(hs, any(g)) {
					missing = append(missing, k)
					break
				}
			}
		}
	}
	sort.Strings(missing)
	return missing
}

// profileEntitlements are a profile's entitlements.
func profileEntitlements(path string) (map[string]any, error) {
	raw, err := exec.Command("security", "cms", "-D", "-i", path).Output()
	if err != nil {
		return nil, err
	}
	c := exec.Command("plutil", "-extract", "Entitlements", "json", "-o", "-", "-")
	c.Stdin = strings.NewReader(string(raw))
	js, err := c.Output()
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(js, &m)
}

// profile is the development profile for the bundle ID id, for the
// device: found, else made by Xcode.
func (s *devSigner) profile(id string) (*profile, error) {
	if !slices.Contains(s.ids, id) {
		s.ids = append(s.ids, id) // known once the Go code is built
	}
	p, err := pickProfileFor(s.team, id, "", "development", s.udid)
	var missing []string
	if err == nil {
		missing = lacks(p, s.wants(id))
	}
	if (err != nil || len(missing) > 0) && !s.tried[id] {
		if perr := s.provision(); perr != nil {
			if err != nil {
				return nil, perr
			}
			fmt.Fprintln(os.Stderr, "asuka:", perr) // the profile there may do
		}
		p, err = pickProfileFor(s.team, id, "", "development", s.udid)
		if err == nil {
			missing = lacks(p, s.wants(id))
		}
	}
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		if !s.optional {
			return nil, fmt.Errorf("the profile %q lacks %s, which %s needs: turn the capabilities on for %s at developer.apple.com (Identifiers), or in Xcode", p.name, strings.Join(missing, ", "), id, id)
		}
		s.goWithout(id, missing)
	}
	return p, nil
}

// goWithout takes the capabilities missing from the player's profile
// for id out of its build, saying so once.
func (s *devSigner) goWithout(id string, missing []string) {
	for _, k := range missing {
		if k == "com.apple.security.application-groups" {
			// No App Group: no share sheet, whose extension shares the
			// app's files through it.
			s.cfg.ShareTarget.on, s.cfg.shareSheet = false, false
			continue
		}
		delete(s.cfg.entitlements, k)
	}
	if key := strings.Join(missing, ","); !s.told[key] {
		s.told[key] = true
		fmt.Printf("asuka: the team's profile for %s lacks %s: the player on this device goes without them\n", id, strings.Join(missing, ", "))
	}
}

// provision has Xcode make (or update) the profiles of the bundle IDs
// not yet provisioned, with the capabilities their entitlements need,
// registering the device with the team if it isn't: xcodebuild's
// automatic signing, on a project of one empty app per bundle ID, with
// the Xcode account's team or the App Store Connect API key.
func (s *devSigner) provision() error {
	var targets []provisionTarget
	for _, id := range s.ids {
		if s.tried[id] {
			continue
		}
		s.tried[id] = true
		ents := s.wants(id)
		if s.bare {
			ents = nil
		}
		targets = append(targets, provisionTarget{id: id, entitlements: ents})
	}
	if len(targets) == 0 {
		return nil
	}
	var ids []string
	for _, t := range targets {
		ids = append(ids, t.id)
	}
	fmt.Printf("asuka: making development profiles for %s with Xcode (team %s)\n", strings.Join(ids, ", "), s.team)
	err := s.xcodebuild(targets)
	if err != nil && s.optional && !s.bare {
		// The player: a team without some capability (a free one) can
		// still have the bare App IDs.
		s.bare = true
		for i := range targets {
			targets[i].entitlements = nil
		}
		if s.xcodebuild(targets) == nil {
			return nil
		}
	}
	return err
}

type provisionTarget struct {
	id           string
	entitlements map[string]any
}

func (s *devSigner) xcodebuild(targets []provisionTarget) error {
	dir := filepath.Join(s.work, "provision")
	os.RemoveAll(dir)
	proj, err := writeProvisionProject(dir, s.team, targets)
	if err != nil {
		return err
	}
	auth := xcodebuildAuth(s.cfg, dir)
	run := func(dest string) ([]byte, error) {
		args := append([]string{"-project", proj, "-scheme", "Provision", "-configuration", "Debug",
			"-destination", dest, "-derivedDataPath", filepath.Join(dir, "build"),
			"-allowProvisioningUpdates", "-allowProvisioningDeviceRegistration"}, auth...)
		args = append(args, "build")
		if s.verbose {
			fmt.Fprintln(os.Stderr, "+ xcodebuild", strings.Join(args, " "))
		}
		c := exec.Command("xcodebuild", args...)
		c.Dir = dir
		return c.CombinedOutput()
	}
	out, err := run("platform=iOS,id=" + s.udid)
	if err != nil && strings.Contains(string(out), "Unable to find a destination") {
		// Xcode doesn't see the device (it hasn't prepared it): its
		// profiles have the team's devices, this one if it's registered.
		out, err = run("generic/platform=iOS")
	}
	if err != nil {
		return xcodebuildError(out)
	}
	return nil
}

// xcodebuildAuth are xcodebuild's flags for the App Store Connect API key,
// when one is set (for publish): it signs in place of an Xcode account.
func xcodebuildAuth(cfg *appConfig, dir string) []string {
	if firstOf(os.Getenv("ASUKA_ASC_KEY_ID"), cfg.IOS.AppStoreConnect.KeyID) == "" {
		return nil
	}
	key, err := loadASCKey(cfg)
	if err != nil || len(key.p8) == 0 {
		if err != nil {
			fmt.Fprintln(os.Stderr, "asuka:", err)
		}
		return nil
	}
	path := filepath.Join(dir, "AuthKey_"+key.id+".p8")
	if err := os.WriteFile(path, key.p8, 0o600); err != nil {
		return nil
	}
	args := []string{"-authenticationKeyPath", path, "-authenticationKeyID", key.id}
	if key.issuer != "" {
		args = append(args, "-authenticationKeyIssuerID", key.issuer)
	}
	return args
}

// xcodebuildError is xcodebuild's failure: its error lines, and what to
// do about the usual ones.
func xcodebuildError(out []byte) error {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "error:") && !slices.Contains(lines, l) {
			lines = append(lines, l)
		}
	}
	msg := strings.Join(lines, "\n")
	if msg == "" {
		msg = strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); len(msg) > 2000 && i > 0 {
			msg = msg[len(msg)-2000:]
		}
	}
	hint := ""
	switch {
	case strings.Contains(msg, "No Accounts") || strings.Contains(msg, "No account for team") || strings.Contains(msg, "not signed in"):
		hint = "\nsign in to Xcode with the team's Apple ID (Xcode › Settings › Accounts), or set an App Store Connect API key (ASUKA_ASC_KEY_ID, ASUKA_ASC_ISSUER_ID, ASUKA_ASC_KEY)"
	case strings.Contains(msg, "is not available") && strings.Contains(msg, "bundle identifier"),
		strings.Contains(msg, "cannot be registered to your development team"):
		hint = "\nthe bundle ID belongs to another team: change asuka.json's id"
	}
	return fmt.Errorf("Xcode couldn't make the development profiles:\n%s%s", msg, hint)
}

// writeProvisionProject writes an Xcode project of one empty app per
// target, signed automatically by the team with the target's
// entitlements, and a scheme that builds them all.
func writeProvisionProject(dir, team string, targets []provisionTarget) (string, error) {
	proj := filepath.Join(dir, "Provision.xcodeproj")
	if err := os.MkdirAll(filepath.Join(proj, "xcshareddata", "xcschemes"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.swift"), []byte("// Signed for its profile only.\n"), 0o644); err != nil {
		return "", err
	}
	n := 0
	oid := func() string { n++; return fmt.Sprintf("A5A5A5A5A5A5A5A5%08X", n) }
	root, mainGroup, file, projList, projConf := oid(), oid(), oid(), oid(), oid()
	var objs, targetIDs, attrs, entries strings.Builder
	for i, t := range targets {
		name := fmt.Sprintf("App%d", i)
		ents := filepath.Join(dir, name+".entitlements")
		if err := os.WriteFile(ents, []byte(plist(t.entitlements)), 0o644); err != nil {
			return "", err
		}
		tid, list, conf, phase, build := oid(), oid(), oid(), oid(), oid()
		fmt.Fprintf(&targetIDs, "%s, ", tid)
		fmt.Fprintf(&attrs, "\t\t\t\t\t%s = {DevelopmentTeam = %s; ProvisioningStyle = Automatic; };\n", tid, team)
		fmt.Fprintf(&objs, `		%[1]s = {isa = PBXNativeTarget; buildConfigurationList = %[2]s; buildPhases = (%[4]s); buildRules = (); dependencies = (); name = %[6]s; productName = %[6]s; productType = "com.apple.product-type.application"; };
		%[2]s = {isa = XCConfigurationList; buildConfigurations = (%[3]s); defaultConfigurationIsVisible = 0; defaultConfigurationName = Debug; };
		%[3]s = {isa = XCBuildConfiguration; name = Debug; buildSettings = {
			CODE_SIGN_ENTITLEMENTS = %[6]s.entitlements;
			CODE_SIGN_IDENTITY = "Apple Development";
			CODE_SIGN_STYLE = Automatic;
			DEVELOPMENT_TEAM = %[8]s;
			GENERATE_INFOPLIST_FILE = YES;
			INFOPLIST_KEY_UILaunchScreen_Generation = YES;
			IPHONEOS_DEPLOYMENT_TARGET = %[9]s;
			PRODUCT_BUNDLE_IDENTIFIER = "%[7]s";
			PRODUCT_NAME = %[6]s;
			SDKROOT = iphoneos;
			SWIFT_VERSION = 5.0;
			TARGETED_DEVICE_FAMILY = "1,2";
		}; };
		%[4]s = {isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = (%[5]s); runOnlyForDeploymentPostprocessing = 0; };
		%[5]s = {isa = PBXBuildFile; fileRef = %[10]s; };
`, tid, list, conf, phase, build, name, t.id, team, iosMin, file)
		fmt.Fprintf(&entries, `         <BuildActionEntry buildForTesting = "NO" buildForRunning = "YES" buildForProfiling = "NO" buildForArchiving = "NO" buildForAnalyzing = "NO">
            <BuildableReference BuildableIdentifier = "primary" BlueprintIdentifier = "%s" BuildableName = "%s.app" BlueprintName = "%s" ReferencedContainer = "container:Provision.xcodeproj"></BuildableReference>
         </BuildActionEntry>
`, tid, name, name)
	}
	pbx := fmt.Sprintf(`// !$*UTF8*$!
{
	archiveVersion = 1;
	classes = {};
	objectVersion = 56;
	objects = {
		%[1]s = {isa = PBXProject; attributes = {LastUpgradeCheck = 1500; TargetAttributes = {
%[2]s				}; }; buildConfigurationList = %[3]s; compatibilityVersion = "Xcode 14.0"; developmentRegion = en; hasScannedForEncodings = 0; knownRegions = (en, Base); mainGroup = %[4]s; projectDirPath = ""; projectRoot = ""; targets = (%[5]s); };
		%[4]s = {isa = PBXGroup; children = (%[6]s); sourceTree = "<group>"; };
		%[6]s = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = main.swift; sourceTree = "<group>"; };
		%[3]s = {isa = XCConfigurationList; buildConfigurations = (%[7]s); defaultConfigurationIsVisible = 0; defaultConfigurationName = Debug; };
		%[7]s = {isa = XCBuildConfiguration; name = Debug; buildSettings = {SDKROOT = iphoneos; IPHONEOS_DEPLOYMENT_TARGET = %[9]s; SWIFT_VERSION = 5.0; ONLY_ACTIVE_ARCH = YES; }; };
%[8]s	};
	rootObject = %[1]s;
}
`, root, attrs.String(), projList, mainGroup, targetIDs.String(), file, projConf, objs.String(), iosMin)
	if err := os.WriteFile(filepath.Join(proj, "project.pbxproj"), []byte(pbx), 0o644); err != nil {
		return "", err
	}
	scheme := `<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion = "1500" version = "1.7">
   <BuildAction parallelizeBuildables = "YES" buildImplicitDependencies = "YES">
      <BuildActionEntries>
` + entries.String() + `      </BuildActionEntries>
   </BuildAction>
</Scheme>
`
	return proj, os.WriteFile(filepath.Join(proj, "xcshareddata", "xcschemes", "Provision.xcscheme"), []byte(scheme), 0o644)
}

// The device

// devicectlRun runs devicectl, its output shown with -v.
func devicectlRun(verbose bool, args ...string) error {
	if verbose {
		fmt.Fprintln(os.Stderr, "+ xcrun devicectl", strings.Join(args, " "))
	}
	out, err := exec.Command("xcrun", append([]string{"devicectl"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("devicectl %s: %s", args[1], devicectlError(out, err))
	}
	return nil
}

// installOnDevice installs the .app on the device.
func installOnDevice(d *iosDevice, bundle string, verbose bool) error {
	return devicectlRun(verbose, "device", "install", "app", "--device", d.UDID, bundle, "-q")
}

// launchOnDevice launches the app id on the device, with env, in place of
// a running one, and returns its process ID.
func launchOnDevice(d *iosDevice, id string, env map[string]string) (int, error) {
	args := []string{"device", "process", "launch", "--device", d.UDID, "--terminate-existing"}
	if len(env) > 0 {
		js, _ := json.Marshal(env)
		args = append(args, "--environment-variables", string(js))
	}
	data, err := devicectlJSON(append(args, id)...)
	if err != nil {
		return 0, launchError(d, err)
	}
	var doc struct {
		Result struct {
			Process struct{ ProcessIdentifier int }
		}
	}
	json.Unmarshal(data, &doc)
	return doc.Result.Process.ProcessIdentifier, nil
}

// launchError explains the launches iOS refuses.
func launchError(d *iosDevice, err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "Locked") || strings.Contains(s, "locked"):
		return fmt.Errorf("%s is locked: unlock it, and asuka dev launches the app (%v)", d.Name, err)
	case strings.Contains(s, "invalid code signature") || strings.Contains(s, "profile has not been explicitly trusted") || strings.Contains(s, "untrusted"):
		return fmt.Errorf("%s doesn't trust the developer yet: in its Settings › General › VPN & Device Management, trust your Apple Development certificate, then run asuka dev again (%v)", d.Name, err)
	}
	return err
}

// terminateOnDevice stops the process pid on the device.
func terminateOnDevice(d *iosDevice, pid int) {
	if pid > 0 {
		exec.Command("xcrun", "devicectl", "device", "process", "terminate", "--device", d.UDID, "--pid", strconv.Itoa(pid), "-q").Run()
	}
}

// installedOnDevice reports whether the app id is installed on the device.
func installedOnDevice(d *iosDevice, id string) bool {
	data, err := devicectlJSON("device", "info", "apps", "--device", d.UDID)
	if err != nil {
		return false
	}
	var doc struct {
		Result struct {
			Apps []struct{ BundleIdentifier string }
		}
	}
	json.Unmarshal(data, &doc)
	for _, a := range doc.Result.Apps {
		if a.BundleIdentifier == id {
			return true
		}
	}
	return false
}

// devIOSDevice runs the app on a physical device: in the player, or
// built with its Go code.
func devIOSDevice(app *goApp, o *devOptions, d *iosDevice, stop <-chan os.Signal) error {
	team, err := devTeam(o.team, app.cfg)
	if err != nil {
		return err
	}
	how := map[string]string{"wired": "USB", "localNetwork": "Wi-Fi"}[d.Transport]
	if how != "" {
		how = ", over " + how
	}
	fmt.Printf("asuka: %s on %s%s, signed by team %s\n", app.name, d, how, team)
	tc, err := newIOSToolchainFor("iphoneos", o.verbose)
	if err != nil {
		return err
	}
	if usePlayer(app, o, "ios-device") {
		return devIOSDevicePlayer(app, tc, o, d, team, stop)
	}
	work, err := workDir(app, "ios-device")
	if err != nil {
		return err
	}
	signer := newDevSigner(team, d.UDID, app.cfg, work, false, o.verbose)
	var logs *exec.Cmd
	stopLogs := func() {
		if logs != nil && logs.Process != nil {
			logs.Process.Signal(os.Interrupt) // devicectl passes it to the app
			done := make(chan struct{})
			go func() { logs.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				logs.Process.Kill()
				<-done
			}
		}
		logs = nil
	}
	build := func() error {
		start := time.Now()
		bo := &buildOptions{platform: platformIOS, distribution: "development", verbose: o.verbose, dev: &iosDevBuild{signer: signer}}
		if err := buildIOS(app, bo); err != nil {
			return err
		}
		stopLogs()
		if err := installOnDevice(d, bo.dev.bundle, o.verbose); err != nil {
			return err
		}
		fmt.Printf("asuka: built and installed in %.1fs\n", time.Since(start).Seconds())
		if o.once {
			_, err := launchOnDevice(d, app.bundleID, nil)
			return err
		}
		// Its standard output and error, here, until it exits.
		logs = exec.Command("xcrun", "devicectl", "device", "process", "launch", "--device", d.UDID, "--terminate-existing", "--console", app.bundleID)
		logs.Stdout, logs.Stderr = os.Stdout, os.Stderr
		return logs.Start()
	}
	if err := build(); err != nil {
		if o.once {
			return err
		}
		fmt.Fprintln(os.Stderr, "asuka:", err)
	}
	if o.once {
		return nil
	}
	defer stopLogs()
	fmt.Println("asuka: watching for changes (Ctrl-C to stop)")
	natives, _ := nativeFor(app, "ios", tc.goarch)
	w := newWatcher(append([]string{app.moduleDir, app.dir}, nativeDirsOf(natives, "ios")...)...)
	for {
		select {
		case <-stop:
			return nil
		case <-w.wake:
			if !w.changed() {
				continue
			}
			fmt.Println("asuka: change detected, rebuilding")
			if err := build(); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
			}
		}
	}
}

// devicePlayerConfig is the player's on a device: its bundle ID the
// team's own (an App ID is one team's), with the Local Network's purpose
// string for Wi-Fi.
func devicePlayerConfig(team string) *appConfig {
	c := playerConfig()
	c.ID = playerID + "." + strings.ToLower(team)
	c.Name = playerName
	c.Version, c.Build = "1.0", 1
	c.Orientations = []string{"portrait", "landscape"}
	return c
}

func devIOSDevicePlayer(app *goApp, tc *iosToolchain, o *devOptions, d *iosDevice, team string, stop <-chan os.Signal) error {
	start := time.Now()
	work, err := workDir(app, "ios")
	if err != nil {
		return err
	}
	// The Go code, as on the simulator: on this Mac's loopback, which the
	// tunnels and the network's relays reach.
	port := freePort(devicePort)
	proc := &hostProcess{app: app, work: work, remote: true, env: []string{
		fmt.Sprintf("ASUKA_REMOTE=127.0.0.1:%d", port),
		"ASUKA_STATE=" + filepath.Join(work, "state.json"),
		"ASUKA_DATA=" + filepath.Join(work, "data"),
		fmt.Sprintf("ASUKA_ASSETS_URL=http://127.0.0.1:%d/", deviceAssetsPort), // the player forwards its loopback's
		fmt.Sprintf("ASUKA_ASSETS_ADDR=127.0.0.1:%d", port+1),
	}}
	if err := proc.inheritRemote(port); err != nil {
		return err
	}
	if err := proc.build(); err != nil {
		return err
	}
	dir := playerDir(work, "ios-device")
	unlock, err := lockShared(dir)
	if err != nil {
		return err
	}
	release := sync.OnceFunc(unlock)
	defer release()
	cfg := devicePlayerConfig(team)
	player := &goApp{dir: dir, name: playerName, bundleID: cfg.ID, cfg: cfg, pkg: playerPkg, pkgIn: app.dir}
	lib, err := tc.buildGo(player, dir)
	if err != nil {
		return err
	}
	hostObj, err := tc.buildHost(dir, nil, allFeatures())
	if err != nil {
		return err
	}
	sum := fileHash(hostObj, lib) + "\n" + team
	marker := filepath.Join(dir, "installed-"+d.UDID)
	if prev, _ := os.ReadFile(marker); string(prev) != sum || !installedOnDevice(d, cfg.ID) {
		pwork, err := workDir(player, "ios-device")
		if err != nil {
			return err
		}
		bo := &buildOptions{platform: platformIOS, distribution: "development", verbose: o.verbose,
			dev: &iosDevBuild{signer: newDevSigner(team, d.UDID, cfg, pwork, true, o.verbose)}}
		if err := buildIOS(player, bo); err != nil {
			return fmt.Errorf("the player: %w", err)
		}
		fmt.Println("asuka: installing the player on", d.Name)
		if err := installOnDevice(d, bo.dev.bundle, o.verbose); err != nil {
			return err
		}
		os.WriteFile(marker, []byte(sum), 0o644)
	}
	release()

	link := newDeviceLink(d, port, port+1)
	if err := link.start(); err != nil {
		return err
	}
	defer link.close()
	if err := proc.restart(); err != nil {
		return err
	}
	pid, err := launchOnDevice(d, cfg.ID, map[string]string{"ASUKA_REMOTE": link.spec().String()})
	if err != nil {
		proc.stop()
		return err
	}
	fmt.Printf("asuka: running in the player in %.1fs; changes reload without reinstalling\n", time.Since(start).Seconds())
	go link.hint(15 * time.Second)
	return watchHost(proc, stop, func() { terminateOnDevice(d, pid) })
}

// deviceLink is how the player on a device reaches the Go code: tunnels
// over USB, and relays on this Mac's network addresses for Wi-Fi. It
// says which the player took.
type deviceLink struct {
	dev                *iosDevice
	remote, assets     int // the Go code's ports, on the loopback
	usb                *devtunnel.Server
	dialUSB            func() (net.Conn, error) // a tunnel (default: usbmuxd's)
	lnRemote, lnAssets net.Listener
	mu                 sync.Mutex
	last               string // how the player last connected
	connected          chan struct{}
	once               sync.Once
}

func newDeviceLink(d *iosDevice, remote, assets int) *deviceLink {
	return &deviceLink{dev: d, remote: remote, assets: assets, connected: make(chan struct{})}
}

func (l *deviceLink) start() error {
	if l.dialUSB == nil {
		mux := &usbmux.Client{}
		l.dialUSB = func() (net.Conn, error) {
			u, ok, err := mux.Find(l.dev.UDID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("not on usbmuxd")
			}
			return mux.Dial(u.ID, deviceTunnelPort)
		}
	}
	target := func(p int) string { return fmt.Sprintf("127.0.0.1:%d", p) }
	l.usb = &devtunnel.Server{
		Dial:    l.dialUSB,
		Targets: map[byte]string{devtunnel.KindRemote: target(l.remote), devtunnel.KindAssets: target(l.assets)},
		Used: func(k byte) {
			if k == devtunnel.KindRemote {
				l.took("USB")
			}
		},
	}
	l.usb.Start()
	// On every address, for the network: the same ports from run to run
	// when they're free, so a player launched again from the home screen
	// finds them.
	var err error
	if l.lnRemote, err = listenAll(deviceTunnelPort + 1); err != nil {
		return err
	}
	if l.lnAssets, err = listenAll(deviceTunnelPort + 2); err != nil {
		return err
	}
	go devtunnel.Relay(l.lnRemote, target(l.remote), func(from net.Addr) { l.took("Wi-Fi") })
	go devtunnel.Relay(l.lnAssets, target(l.assets), nil)
	return nil
}

func (l *deviceLink) close() {
	l.usb.Close()
	if l.lnRemote != nil {
		l.lnRemote.Close()
	}
	if l.lnAssets != nil {
		l.lnAssets.Close()
	}
}

// took notes how the player connected, and says so when it changes.
func (l *deviceLink) took(how string) {
	l.mu.Lock()
	changed := l.last != how
	l.last = how
	l.mu.Unlock()
	l.once.Do(func() { close(l.connected) })
	if changed {
		fmt.Printf("asuka: the player connected over %s\n", how)
	}
}

// how is how the player last connected ("" not yet).
func (l *deviceLink) how() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last
}

// spec is what the player is launched with.
func (l *deviceLink) spec() devtunnel.Spec {
	hosts := lanHosts()
	s := devtunnel.Spec{Tunnel: deviceTunnelPort, AssetsPort: deviceAssetsPort}
	rp := l.lnRemote.Addr().(*net.TCPAddr).Port
	ap := l.lnAssets.Addr().(*net.TCPAddr).Port
	for _, h := range hosts {
		s.Remote = append(s.Remote, net.JoinHostPort(h, strconv.Itoa(rp)))
		s.Assets = append(s.Assets, net.JoinHostPort(h, strconv.Itoa(ap)))
	}
	return s
}

// hint says what to check when the player hasn't connected after wait.
func (l *deviceLink) hint(wait time.Duration) {
	select {
	case <-l.connected:
	case <-time.After(wait):
		fmt.Printf("asuka: the player hasn't connected yet. By USB, check the cable and that %s is unlocked; over Wi-Fi, it must be on this Mac's network, allowed to find devices on it (tap Allow when it asks, or its Settings › Apps › asuka › Local Network), and this Mac's firewall must let asuka take connections\n", l.dev.Name)
	}
}

// listenAll listens on every address, on port if it's free.
func listenAll(port int) (net.Listener, error) {
	if ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port)); err == nil {
		return ln, nil
	}
	return net.Listen("tcp", ":0")
}

// lanHosts are this Mac's names for the phone on the network: its IPv4
// addresses, then its Bonjour name, which follows a new address.
func lanHosts() []string {
	var ifs []lanIface
	list, _ := net.Interfaces()
	for _, i := range list {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				ifs = append(ifs, lanIface{name: i.Name, ip: n.IP})
			}
		}
	}
	name, _ := output("scutil", "--get", "LocalHostName")
	return orderLANHosts(ifs, name)
}

type lanIface struct {
	name string
	ip   net.IP
}

// orderLANHosts orders the addresses the phone may reach: IPv4 on Wi-Fi
// and Ethernet (en*) first, then others, then name.local.
func orderLANHosts(ifs []lanIface, name string) []string {
	var first, rest []string
	for _, i := range ifs {
		ip := i.ip.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		// Not the virtual ones: AirDrop's, VMs' bridges, the CoreDevice
		// tunnels'.
		if strings.HasPrefix(i.name, "awdl") || strings.HasPrefix(i.name, "llw") || strings.HasPrefix(i.name, "utun") ||
			strings.HasPrefix(i.name, "bridge") || strings.HasPrefix(i.name, "anpi") || strings.HasPrefix(i.name, "vmenet") {
			continue
		}
		if strings.HasPrefix(i.name, "en") {
			first = append(first, ip.String())
		} else {
			rest = append(rest, ip.String())
		}
	}
	out := append(first, rest...)
	if name != "" {
		out = append(out, name+".local")
	}
	return out
}
