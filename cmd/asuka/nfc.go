package main

import (
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// nfcPkg is asuka/sdk/nfc's import path: an app that imports it gets what
// its sessions and tags need (featurePermissions, addNFC).
const nfcPkg = "github.com/egoist/asuka/sdk/nfc"

// nfcConfig is asuka.json's "nfc": what asuka/sdk/nfc needs of the
// platforms beyond the permissions every app that imports it has.
//
//	"nfc": {
//	  "iso7816": ["D2760000850101"],
//	  "felica": ["8008"],
//	  "ndef": [{"uri": "https://example.com/tags/"}, {"mime": "application/vnd.example.ticket"}, {"external": "example.com:ticket"}],
//	  "tech": [["IsoDep"], ["NfcA", "MifareUltralight"]]
//	}
type nfcConfig struct {
	// ISO7816 are the applications (AIDs, in hex) iOS selects as it finds
	// an ISO 7816 card, in order: Info.plist's
	// com.apple.developer.nfc.readersession.iso7816.select-identifiers.
	// A session with Poll finds only the cards with one of them.
	ISO7816 []string `json:"iso7816"`
	// FeliCa are the system codes (4 hex digits) iOS polls FeliCa cards
	// for: com.apple.developer.nfc.readersession.felica.systemcodes,
	// without which a session that polls FeliCa fails.
	FeliCa []string `json:"felica"`
	// NDEF are the tags that open the app on Android, by their message's
	// first record (NDEF_DISCOVERED): a URI (its scheme, host and path
	// prefix; it comes to ui.OnOpenURL), a MIME type (text records are
	// text/plain), an external type.
	NDEF []nfcFilter `json:"ndef"`
	// Tech are the tags that open the app on Android by their
	// technologies (TECH_DISCOVERED): each list, technologies a tag must
	// all have (android.nfc.tech's classes: "IsoDep", "NfcA",
	// "MifareUltralight"...).
	Tech [][]string `json:"tech"`
	// Required makes the app one for phones with NFC alone: Google Play
	// doesn't offer it to others (uses-feature android.hardware.nfc).
	Required bool `json:"required"`
}

// nfcFilter is one of Android's NDEF_DISCOVERED filters: one of its
// fields.
type nfcFilter struct {
	URI      string `json:"uri"`
	MIME     string `json:"mime"`
	External string `json:"external"`
}

// nfcTechs are android.nfc.tech's classes.
var nfcTechs = []string{"IsoDep", "MifareClassic", "MifareUltralight", "Ndef", "NdefFormatable", "NfcA", "NfcB", "NfcBarcode", "NfcF", "NfcV"}

// check checks the config, and writes Tech's names as Android's classes.
func (n *nfcConfig) check() error {
	for _, aid := range n.ISO7816 {
		b, err := hex.DecodeString(aid)
		if err != nil || len(b) < 5 || len(b) > 16 {
			return fmt.Errorf("iso7816: %q (want an AID: 5 to 16 bytes, in hex)", aid)
		}
	}
	for _, c := range n.FeliCa {
		b, err := hex.DecodeString(c)
		if err != nil || len(b) != 2 || strings.EqualFold(c, "ffff") {
			return fmt.Errorf("felica: %q (want a system code: 4 hex digits, not the wildcard FFFF)", c)
		}
	}
	for _, f := range n.NDEF {
		set := 0
		for _, v := range []string{f.URI, f.MIME, f.External} {
			if v != "" {
				set++
			}
		}
		if set != 1 {
			return fmt.Errorf("ndef: each filter is one of uri, mime or external")
		}
		switch {
		case f.URI != "":
			u, err := url.Parse(f.URI)
			if err != nil || u.Scheme == "" {
				return fmt.Errorf("ndef: uri %q (want a URI with its scheme)", f.URI)
			}
		case f.MIME != "":
			if t, sub, ok := strings.Cut(f.MIME, "/"); !ok || t == "" || sub == "" {
				return fmt.Errorf("ndef: mime %q (want a MIME type, as text/plain)", f.MIME)
			}
		default:
			if d, t, ok := strings.Cut(f.External, ":"); !ok || d == "" || t == "" {
				return fmt.Errorf("ndef: external %q (want domain:type, as example.com:thing)", f.External)
			}
		}
	}
	for i, list := range n.Tech {
		if len(list) == 0 {
			return fmt.Errorf("tech: an empty list")
		}
		for j, t := range list {
			name := strings.TrimPrefix(t, "android.nfc.tech.")
			k := slices.IndexFunc(nfcTechs, func(c string) bool { return strings.EqualFold(c, name) })
			if k < 0 {
				return fmt.Errorf("tech: %q (want one of android.nfc.tech's: %s)", t, strings.Join(nfcTechs, ", "))
			}
			n.Tech[i][j] = "android.nfc.tech." + nfcTechs[k]
		}
	}
	return nil
}

// addNFC adds, for an app that imports asuka/sdk/nfc (deps; nil: the
// player, which runs any app), what its config asks of the platforms:
// Info.plist's AIDs and system codes, Android's uses-feature and filters.
func (c *appConfig) addNFC(deps []string) {
	if deps != nil && !slices.Contains(deps, nfcPkg) {
		return
	}
	c.nfcOn = true
	feature := fmt.Sprintf(`<uses-feature android:name="android.hardware.nfc" android:required="%t" />`, c.NFC.Required)
	if !slices.Contains(c.androidTop, feature) {
		c.androidTop = append(c.androidTop, feature)
	}
	add := func(k string, vs []string) {
		if len(vs) == 0 {
			return
		}
		if c.plistArrays == nil {
			c.plistArrays = map[string][]string{}
		}
		for _, v := range vs {
			v = strings.ToUpper(v)
			if !slices.Contains(c.plistArrays[k], v) {
				c.plistArrays[k] = append(c.plistArrays[k], v)
			}
		}
	}
	add("com.apple.developer.nfc.readersession.iso7816.select-identifiers", c.NFC.ISO7816)
	add("com.apple.developer.nfc.readersession.felica.systemcodes", c.NFC.FeliCa)
}

// nfcFilters are the Activity's NDEF_DISCOVERED and TECH_DISCOVERED
// intent filters ("" without asuka/sdk/nfc or filters).
func (c *appConfig) nfcFilters() string {
	if !c.nfcOn {
		return ""
	}
	var b strings.Builder
	for _, f := range c.NFC.NDEF {
		var data string
		switch {
		case f.URI != "":
			u, _ := url.Parse(f.URI)
			data = fmt.Sprintf(`<data android:scheme="%s"`, html.EscapeString(strings.ToLower(u.Scheme)))
			if u.Host != "" {
				data += fmt.Sprintf(` android:host="%s"`, html.EscapeString(strings.ToLower(u.Hostname())))
				if p := u.Port(); p != "" {
					data += fmt.Sprintf(` android:port="%s"`, html.EscapeString(p))
				}
			}
			if u.Path != "" && u.Path != "/" {
				data += fmt.Sprintf(` android:pathPrefix="%s"`, html.EscapeString(u.Path))
			}
			data += " />"
		case f.MIME != "":
			data = fmt.Sprintf(`<data android:mimeType="%s" />`, html.EscapeString(strings.ToLower(f.MIME)))
		default:
			// Android's URI for an external type: vnd.android.nfc://ext/domain:type.
			data = fmt.Sprintf(`<data android:scheme="vnd.android.nfc" android:host="ext" android:pathPrefix="/%s" />`, html.EscapeString(strings.ToLower(f.External)))
		}
		fmt.Fprintf(&b, `            <intent-filter>
                <action android:name="android.nfc.action.NDEF_DISCOVERED" />
                <category android:name="android.intent.category.DEFAULT" />
                %s
            </intent-filter>
`, data)
	}
	if len(c.NFC.Tech) > 0 {
		b.WriteString(`            <intent-filter>
                <action android:name="android.nfc.action.TECH_DISCOVERED" />
            </intent-filter>
            <meta-data android:name="android.nfc.action.TECH_DISCOVERED" android:resource="@xml/asuka_nfc_tech" />
`)
	}
	return b.String()
}

// androidNFCRes writes TECH_DISCOVERED's tech lists (xml/asuka_nfc_tech),
// and reports whether it wrote them.
func (c *appConfig) androidNFCRes(res string) (bool, error) {
	if !c.nfcOn || len(c.NFC.Tech) == 0 {
		return false, nil
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<resources>\n")
	for _, list := range c.NFC.Tech {
		b.WriteString("    <tech-list>\n")
		for _, t := range list {
			fmt.Fprintf(&b, "        <tech>%s</tech>\n", html.EscapeString(t))
		}
		b.WriteString("    </tech-list>\n")
	}
	b.WriteString("</resources>\n")
	dir := filepath.Join(res, "xml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(filepath.Join(dir, "asuka_nfc_tech.xml"), []byte(b.String()), 0o644)
}
