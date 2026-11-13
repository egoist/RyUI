package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mac provisioning profiles. Push (com.apple.developer.aps-environment),
// Sign in with Apple and App Attest are entitlements only a provisioning
// profile grants: a Mac app signed with them carries its profile, as
// Contents/embedded.provisionprofile, or macOS won't open it. A Developer
// ID profile is for an app distributed outside the store (signed with a
// Developer ID Application certificate, notarized), a Mac App Store one
// for the store (signed with Apple Distribution, in a .pkg signed with
// Mac Installer Distribution), a development one for the Macs it lists.

// macProfile is a decoded Mac provisioning profile (.provisionprofile).
type macProfile struct {
	path, name, uuid, team, appID string
	kind                          string // developer-id, app-store, development
	expires                       time.Time
	certs                         []string       // SHA-1s of the certificates it allows
	entitlements                  map[string]any // what it grants
	// signed: Apple's (a CMS message); else a plain plist, which only an
	// ad hoc build takes (to look at the bundle it makes: macOS won't
	// open it).
	signed       bool
	identity     string // the SHA-1 of the keychain identity that signs with it
	identityName string
}

// macProfileFeatures are the entitlements only a profile grants that a
// Mac app's packages need: their iOS name (appConfig.entitlements', as
// the packages' imports say), their Mac name, and what they're for.
var macProfileFeatures = []struct{ ios, mac, what string }{
	{"aps-environment", "com.apple.developer.aps-environment", "Push Notifications (asuka/push)"},
	{"com.apple.developer.applesignin", "com.apple.developer.applesignin", "Sign in with Apple (asuka/appleauth)"},
	{"com.apple.developer.devicecheck.appattest-environment", "com.apple.developer.devicecheck.appattest-environment", "App Attest (asuka/integrity)"},
}

// macProfileNeeds are what the app's packages need a profile for (their
// descriptions), none if nothing.
func macProfileNeeds(cfg *appConfig) []string {
	var out []string
	for _, f := range macProfileFeatures {
		if _, ok := cfg.entitlements[f.ios]; ok {
			out = append(out, f.what)
		}
	}
	return out
}

// decodeProfile is a profile's property list: a .provisionprofile is
// one signed by Apple (CMS), which security cms decodes; a plain plist
// is taken as it is, unsigned.
func decodeProfile(path string) (data []byte, signed bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if t := bytes.TrimSpace(raw); bytes.HasPrefix(t, []byte("<?xml")) || bytes.HasPrefix(t, []byte("<plist")) {
		return raw, false, nil
	}
	out, err := exec.Command("security", "cms", "-D", "-i", path).Output()
	if err != nil {
		return nil, false, fmt.Errorf("%s: not a provisioning profile (security cms can't decode it)", path)
	}
	return out, true, nil
}

// readMacProfile reads the Mac provisioning profile at path.
func readMacProfile(path string) (*macProfile, error) {
	data, signed, err := decodeProfile(path)
	if err != nil {
		return nil, err
	}
	v, err := parsePlist(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: not a provisioning profile (its plist isn't a dictionary)", path)
	}
	p, err := macProfileFrom(path, m)
	if err != nil {
		return nil, err
	}
	p.signed = signed
	return p, nil
}

// macProfileFrom reads a profile's decoded plist m.
func macProfileFrom(path string, m map[string]any) (*macProfile, error) {
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	p := &macProfile{path: path, name: str(m, "Name"), uuid: str(m, "UUID")}
	if teams, _ := m["TeamIdentifier"].([]any); len(teams) > 0 {
		p.team, _ = teams[0].(string)
	}
	p.entitlements, _ = m["Entitlements"].(map[string]any)
	if p.entitlements == nil {
		return nil, fmt.Errorf("%s: not a provisioning profile (no Entitlements)", path)
	}
	// A Mac profile's app ID is com.apple.application-identifier; an iOS
	// one's, application-identifier.
	if p.appID = str(p.entitlements, "com.apple.application-identifier"); p.appID == "" {
		p.appID = str(p.entitlements, "application-identifier")
	}
	if p.team == "" {
		p.team = str(p.entitlements, "com.apple.developer.team-identifier")
	}
	if p.team == "" || p.appID == "" {
		return nil, fmt.Errorf("%s: a provisioning profile without a team or an app ID", path)
	}
	if platforms, ok := m["Platform"].([]any); ok && !slices.Contains(platforms, any("OSX")) {
		var names []string
		for _, s := range platforms {
			names = append(names, fmt.Sprint(s))
		}
		return nil, fmt.Errorf("%s (%q) is a profile for %s, not the Mac: make a Mac one (a .provisionprofile) at developer.apple.com", path, p.name, strings.Join(names, ", "))
	}
	p.expires, _ = m["ExpirationDate"].(time.Time)
	switch {
	case m["ProvisionsAllDevices"] == true:
		p.kind = "developer-id"
	case m["ProvisionedDevices"] != nil:
		p.kind = "development"
	default:
		p.kind = "app-store"
	}
	certs, _ := m["DeveloperCertificates"].([]any)
	for _, c := range certs {
		if der, ok := c.([]byte); ok {
			sum := sha1.Sum(der)
			p.certs = append(p.certs, strings.ToUpper(hex.EncodeToString(sum[:])))
		}
	}
	return p, nil
}

// macProfileFiles are the Mac profiles Xcode downloaded.
func macProfileFiles() []string {
	var files []string
	for _, d := range profileDirs() {
		m, _ := filepath.Glob(filepath.Join(d, "*.provisionprofile"))
		files = append(files, m...)
	}
	return files
}

// pickMacProfile finds the profile to sign the app id with: want (a
// path, name or UUID) if given, else the closest unexpired match of the
// kind (developer-id; app-store for the Mac App Store) for id and team
// (if given) whose certificate is in the keychain (ids, by SHA-1). A
// profile want names may have no certificate in the keychain: the
// caller decides (an ad hoc build, a -sign identity).
func pickMacProfile(team, id, want string, appStore bool, ids map[string]string) (*macProfile, error) {
	kind := "developer-id"
	if appStore {
		kind = "app-store"
	}
	var all []*macProfile
	explicit := want != ""
	if want != "" && exists(want) {
		p, err := readMacProfile(want)
		if err != nil {
			return nil, err
		}
		all, want = []*macProfile{p}, ""
	} else {
		for _, f := range macProfileFiles() {
			if p, err := readMacProfile(f); err == nil {
				all = append(all, p)
			}
		}
	}
	var best *macProfile
	bestScore := 0
	var near []string
	for _, p := range all {
		if want != "" && p.name != want && p.uuid != want {
			continue
		}
		score := (&profile{appID: p.appID}).matches(team, id)
		if score == 0 {
			if explicit {
				return nil, fmt.Errorf("the profile %q is for %s, not %s (team %s)", p.name, p.appID, id, orAny(team))
			}
			continue
		}
		if p.kind != kind {
			if explicit && (p.kind == "app-store") != appStore {
				if appStore {
					return nil, fmt.Errorf("the profile %q is a %s profile: -appstore needs a Mac App Store one", p.name, p.kind)
				}
				return nil, fmt.Errorf("the profile %q is a Mac App Store profile: build with -appstore, or use a Developer ID one", p.name)
			}
			if !explicit {
				near = append(near, fmt.Sprintf("%q (%s)", p.name, p.kind))
				continue
			}
		}
		if !p.expires.IsZero() && p.expires.Before(time.Now()) {
			if explicit {
				return nil, fmt.Errorf("the profile %q expired on %s: make it again at developer.apple.com", p.name, p.expires.Format("2006-01-02"))
			}
			near = append(near, fmt.Sprintf("%q (expired %s)", p.name, p.expires.Format("2006-01-02")))
			continue
		}
		for _, c := range p.certs {
			if name, ok := ids[c]; ok {
				p.identity, p.identityName = c, name
				break
			}
		}
		if p.identity == "" && !explicit {
			near = append(near, fmt.Sprintf("%q (its certificate isn't in the keychain)", p.name))
			continue
		}
		if score > bestScore {
			best, bestScore = p, score
		}
	}
	if best != nil {
		return best, nil
	}
	if explicit && want != "" {
		return nil, fmt.Errorf("no Mac provisioning profile named %q installed (looked in %s)", want, strings.Join(profileDirs(), ", "))
	}
	msg := fmt.Sprintf("no %s Mac provisioning profile for %s", kind, id)
	if len(near) > 0 {
		sort.Strings(near)
		msg += "; close ones: " + strings.Join(near, ", ")
	}
	return nil, errors.New(msg + ". Make one for the app ID at developer.apple.com (Profiles: macOS, " +
		map[bool]string{false: "Developer ID", true: "Mac App Store Connect"}[appStore] + ") and download it, or pass -profile")
}

func orAny(team string) string {
	if team == "" {
		return "any"
	}
	return team
}

// macIdentity is what signs an app with prof: sign (-sign: a name, part
// of one or a SHA-1; "-" ad hoc), which the profile must allow, else the
// keychain identity the profile allows. An unsigned profile (a plain
// plist) is signed ad hoc without -sign. It returns the identity
// (a SHA-1, or what -sign says, or "-") and its name, to print.
func macIdentity(sign string, prof *macProfile, ids map[string]string) (string, string, error) {
	if sign == "-" {
		return "-", "ad hoc", nil
	}
	if sign == "" {
		if prof.identity != "" {
			return prof.identity, prof.identityName, nil
		}
		if !prof.signed {
			return "-", "ad hoc", nil
		}
		return "", "", fmt.Errorf("none of the certificates the profile %q allows is in the keychain: install one (Xcode › Settings › Accounts › Manage Certificates), or pass -sign", prof.name)
	}
	var matched []string
	for sha, name := range ids {
		if strings.EqualFold(sha, sign) || strings.Contains(name, sign) {
			matched = append(matched, sha)
		}
	}
	if len(matched) == 0 || !prof.signed || len(prof.certs) == 0 {
		return sign, sign, nil // codesign says what's wrong with it
	}
	sort.Strings(matched)
	for _, sha := range matched {
		if slices.Contains(prof.certs, sha) {
			return sha, ids[sha], nil
		}
	}
	return "", "", fmt.Errorf("the profile %q doesn't allow %q (its certificate isn't one the profile lists): sign with the profile's certificate, or make the profile again with this one", prof.name, ids[matched[0]])
}

// macEntitlements are the entitlements a Mac app is signed with: the
// hardened runtime's for what its purpose strings ask for, and with a
// profile, its app ID and team and what its packages need of the
// profile's (push, Sign in with Apple, App Attest: an error if the
// profile lacks one); for the Mac App Store, the App Sandbox and what a
// Asuka app does in it (the network, the files the user picks).
func macEntitlements(cfg *appConfig, prof *macProfile, appStore bool) (map[string]any, error) {
	ents := map[string]any{}
	for k := range cfg.IOS.Usage {
		if e := macResourceEntitlements[k]; e != "" {
			ents[e] = true
		}
	}
	if appStore {
		ents["com.apple.security.app-sandbox"] = true
		ents["com.apple.security.network.client"] = true
		ents["com.apple.security.files.user-selected.read-write"] = true
	}
	if prof == nil {
		return ents, nil
	}
	ents["com.apple.application-identifier"] = prof.team + "." + cfg.ID // a wildcard profile's becomes the app's
	ents["com.apple.developer.team-identifier"] = prof.team
	var missing []string
	for _, f := range macProfileFeatures {
		if _, used := cfg.entitlements[f.ios]; !used {
			continue
		}
		v, ok := prof.entitlements[f.mac]
		if !ok {
			v, ok = prof.entitlements[f.ios]
		}
		if !ok {
			missing = append(missing, f.what)
			continue
		}
		ents[f.mac] = plistEntitlement(v)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the profile %q lacks %s, which the app's packages need: turn the capabilities on for %s at developer.apple.com (Identifiers), then make the profile again",
			prof.name, strings.Join(missing, ", "), cfg.ID)
	}
	// A development profile lets a debugger attach (the notary refuses it).
	if prof.kind == "development" {
		if v, ok := prof.entitlements["com.apple.security.get-task-allow"]; ok {
			ents["com.apple.security.get-task-allow"] = plistEntitlement(v)
		}
	}
	return ents, nil
}

// plistEntitlement is a profile's value as plist() writes it.
func plistEntitlement(v any) any {
	switch x := v.(type) {
	case int64:
		return int(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plistEntitlement(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = plistEntitlement(e)
		}
		return out
	}
	return v
}

// installerIdentity is the keychain's identity that signs a Mac App
// Store .pkg for team: Mac Installer Distribution (3rd Party Mac
// Developer Installer, as it was called).
func installerIdentity(team string) string {
	out, _ := exec.Command("security", "find-identity", "-v").Output()
	return pickInstaller(string(out), team)
}

func pickInstaller(findIdentity, team string) string {
	for _, l := range strings.Split(findIdentity, "\n") {
		_, rest, ok := strings.Cut(l, `"`)
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimSpace(rest), `"`)
		if (strings.HasPrefix(name, "3rd Party Mac Developer Installer:") || strings.HasPrefix(name, "Mac Installer Distribution:")) &&
			(team == "" || strings.HasSuffix(name, "("+team+")")) {
			return name
		}
	}
	return ""
}

// parsePlist decodes an XML property list: dictionaries (map[string]any),
// arrays ([]any), strings, integers (int64), reals (float64), booleans,
// dates (time.Time) and data ([]byte).
func parsePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("not a property list")
		}
		if err != nil {
			return nil, err
		}
		if s, ok := tok.(xml.StartElement); ok {
			if s.Name.Local != "plist" {
				return nil, fmt.Errorf("not a property list (<%s>)", s.Name.Local)
			}
			start, end, err := plistNext(d)
			if err != nil {
				return nil, err
			}
			if end {
				return nil, errors.New("an empty property list")
			}
			return plistValue(d, start)
		}
	}
}

// plistNext is the next element's start, or end: the enclosing one ends.
func plistNext(d *xml.Decoder) (xml.StartElement, bool, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return xml.StartElement{}, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return t, false, nil
		case xml.EndElement:
			return xml.StartElement{}, true, nil
		}
	}
}

func plistValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	text := func() (string, error) {
		var s string
		err := d.DecodeElement(&s, &start)
		return s, err
	}
	switch start.Name.Local {
	case "dict":
		m := map[string]any{}
		for {
			k, end, err := plistNext(d)
			if err != nil {
				return nil, err
			}
			if end {
				return m, nil
			}
			if k.Name.Local != "key" {
				return nil, fmt.Errorf("<%s> in a dict where a key goes", k.Name.Local)
			}
			var key string
			if err := d.DecodeElement(&key, &k); err != nil {
				return nil, err
			}
			v, end, err := plistNext(d)
			if err != nil {
				return nil, err
			}
			if end {
				return nil, fmt.Errorf("the key %q has no value", key)
			}
			if m[key], err = plistValue(d, v); err != nil {
				return nil, err
			}
		}
	case "array":
		a := []any{}
		for {
			e, end, err := plistNext(d)
			if err != nil {
				return nil, err
			}
			if end {
				return a, nil
			}
			v, err := plistValue(d, e)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
	case "true", "false":
		return start.Name.Local == "true", d.Skip()
	case "string":
		return text()
	case "integer":
		s, err := text()
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	case "real":
		s, err := text()
		if err != nil {
			return nil, err
		}
		return strconv.ParseFloat(strings.TrimSpace(s), 64)
	case "date":
		s, err := text()
		if err != nil {
			return nil, err
		}
		return time.Parse(time.RFC3339, strings.TrimSpace(s))
	case "data":
		s, err := text()
		if err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
	}
	return nil, fmt.Errorf("<%s> isn't a property list's", start.Name.Local)
}
