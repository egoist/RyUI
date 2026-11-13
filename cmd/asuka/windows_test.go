package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWindowsArch(t *testing.T) {
	for in, want := range map[string]string{"": "amd64", "amd64": "amd64", "arm64": "arm64"} {
		if got, err := windowsArch(in); got != want || err != nil {
			t.Errorf("windowsArch(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := windowsArch("386"); err == nil {
		t.Error("windowsArch(386): no error")
	}
}

// The resources' object is the build's machine's, with its relocations.
func TestWinresMachine(t *testing.T) {
	app := &goApp{name: "Notes", dir: t.TempDir()}
	for arch, machine := range map[string]uint16{"amd64": 0x8664, "arm64": 0xAA64} {
		b, err := winresSyso(app, arch)
		if err != nil {
			t.Fatal(err)
		}
		if m := binary.LittleEndian.Uint16(b); m != machine {
			t.Errorf("%s: machine %#x, want %#x", arch, m, machine)
		}
	}
}

// Without a certificate the .exe isn't signed; with one but no password
// set, or no file, it says so.
func TestSignWindows(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "App.exe")
	os.WriteFile(exe, []byte("MZ"), 0o644)
	t.Setenv("ASUKA_WINDOWS_CERT", "")
	if signed, err := signWindows(exe, "App", false); signed || err != nil {
		t.Fatalf("no certificate: %v, %v", signed, err)
	}
	t.Setenv("ASUKA_WINDOWS_CERT", filepath.Join(t.TempDir(), "missing.pfx"))
	if _, err := signWindows(exe, "App", false); err == nil || !strings.Contains(err.Error(), "ASUKA_WINDOWS_CERT") {
		t.Fatalf("a missing certificate: %v", err)
	}
	pfx := filepath.Join(t.TempDir(), "cert.pfx")
	os.WriteFile(pfx, []byte("x"), 0o600)
	t.Setenv("ASUKA_WINDOWS_CERT", pfx)
	os.Unsetenv("ASUKA_WINDOWS_CERT_PASSWORD")
	if _, err := signWindows(exe, "App", false); err == nil || !strings.Contains(err.Error(), "ASUKA_WINDOWS_CERT_PASSWORD") {
		t.Fatalf("no password: %v", err)
	}
	t.Setenv("ASUKA_WINDOWS_CERT_PASSWORD", "secret")
	ts := defaultTimestamps[0]
	tool, args, err := windowsSigner(exe, "App", pfx, "secret", ts)
	if err != nil {
		t.Skipf("no signer here: %v", err)
	}
	joined := strings.Join(args, " ")
	if runtime.GOOS == "windows" {
		if !strings.Contains(joined, "/fd SHA256 /tr "+ts+" /td SHA256") {
			t.Errorf("%s %s", tool, joined)
		}
	} else if !strings.Contains(joined, "-h sha256") || !strings.Contains(joined, "-ts "+ts) {
		t.Errorf("%s %s", tool, joined)
	}
	if r := strings.Join(redact(args, "secret"), " "); strings.Contains(r, "secret") {
		t.Errorf("the password is printed: %s", r)
	}
}

func TestTimestampServers(t *testing.T) {
	if got := timestampServers(""); !slices.Equal(got, defaultTimestamps) {
		t.Errorf("default: %v", got)
	}
	if got := timestampServers("None"); got != nil {
		t.Errorf("none: %v", got)
	}
	if got := timestampServers(" http://a , http://b,"); !slices.Equal(got, []string{"http://a", "http://b"}) {
		t.Errorf("a list: %v", got)
	}
	if !timestampFailed("HTTP failure: Failed to get data from http://x\nRFC 3161 timestamping failed") ||
		!timestampFailed("SignTool Error: The specified timestamp server either could not be reached or returned an invalid response.") ||
		timestampFailed("Failed to read PKCS#12 file") {
		t.Error("timestampFailed")
	}
}

// With osslsigncode (and openssl, for a self-signed certificate), a PE is
// signed and the signature verifies against the certificate; with no
// timestamp server answering, signing fails, saying so.
func TestSignWindowsPE(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signtool signs on Windows")
	}
	if _, err := exec.LookPath("osslsigncode"); err != nil {
		t.Skip("no osslsigncode")
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("no openssl")
	}
	dir := t.TempDir()
	key, crt, pfx := filepath.Join(dir, "key.pem"), filepath.Join(dir, "cert.pem"), filepath.Join(dir, "cert.pfx")
	for _, args := range [][]string{
		{"req", "-x509", "-newkey", "rsa:2048", "-sha256", "-days", "2", "-nodes", "-keyout", key, "-out", crt,
			"-subj", "/CN=Asuka Test", "-addext", "keyUsage=critical,digitalSignature", "-addext", "extendedKeyUsage=codeSigning"},
		{"pkcs12", "-export", "-out", pfx, "-inkey", key, "-in", crt, "-passout", "pass:secret"},
	} {
		if b, err := exec.Command("openssl", args...).CombinedOutput(); err != nil {
			t.Skipf("openssl %s: %v\n%s", args[0], err, b)
		}
	}
	exe := filepath.Join(dir, "App.exe")
	if err := os.WriteFile(exe, tinyPE(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASUKA_WINDOWS_CERT", pfx)
	t.Setenv("ASUKA_WINDOWS_CERT_PASSWORD", "secret")

	// No server answers (nothing listens on port 1): a clear error, the
	// .exe as it was.
	t.Setenv("ASUKA_WINDOWS_TIMESTAMP", "http://127.0.0.1:1,http://127.0.0.1:2")
	if _, err := signWindows(exe, "App", false); err == nil || !strings.Contains(err.Error(), "no timestamp server answered (http://127.0.0.1:1, http://127.0.0.1:2)") ||
		!strings.Contains(err.Error(), "ASUKA_WINDOWS_TIMESTAMP") {
		t.Fatalf("no timestamp server: %v", err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, tinyPE()) {
		t.Fatal("the .exe changed though signing failed")
	}
	if _, err := os.Stat(exe + ".signed"); err == nil {
		t.Error("osslsigncode's output is left behind")
	}

	// Without a timestamp: signed.
	t.Setenv("ASUKA_WINDOWS_TIMESTAMP", "none")
	signed, err := signWindows(exe, "App", false)
	if err != nil || !signed {
		t.Fatalf("signing: %v, %v", signed, err)
	}
	out, err := exec.Command("osslsigncode", "verify", "-CAfile", crt, "-in", exe).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Signature verification: ok") || !strings.Contains(string(out), "Text description: App") {
		t.Fatalf("verify: %v\n%s", err, out)
	}
}

// tinyPE is the smallest PE32+ osslsigncode signs: headers and one
// section of code (ret).
func tinyPE() []byte {
	const fileAlign, sectAlign = 0x200, 0x1000
	b := make([]byte, 2*fileAlign)
	le := binary.LittleEndian
	copy(b, "MZ")
	le.PutUint32(b[0x3c:], 0x40) // e_lfanew
	copy(b[0x40:], "PE\x00\x00")
	coff := b[0x44:]
	le.PutUint16(coff[0:], 0x8664) // machine: amd64
	le.PutUint16(coff[2:], 1)      // sections
	le.PutUint16(coff[16:], 240)   // optional header's size
	le.PutUint16(coff[18:], 0x22)  // executable, large address aware
	opt := b[0x58:]
	le.PutUint16(opt[0:], 0x20b)        // PE32+
	le.PutUint32(opt[4:], fileAlign)    // size of code
	le.PutUint32(opt[16:], sectAlign)   // entry point
	le.PutUint32(opt[20:], sectAlign)   // base of code
	le.PutUint64(opt[24:], 0x140000000) // image base
	le.PutUint32(opt[32:], sectAlign)
	le.PutUint32(opt[36:], fileAlign)
	le.PutUint16(opt[40:], 6)           // OS version
	le.PutUint16(opt[48:], 6)           // subsystem version
	le.PutUint32(opt[56:], 2*sectAlign) // size of image
	le.PutUint32(opt[60:], fileAlign)   // size of headers
	le.PutUint16(opt[68:], 2)           // subsystem: GUI
	le.PutUint16(opt[70:], 0x8160)      // DLL characteristics: ASLR, DEP, high entropy, terminal server aware
	le.PutUint64(opt[72:], 0x100000)
	le.PutUint64(opt[80:], 0x1000)
	le.PutUint64(opt[88:], 0x100000)
	le.PutUint64(opt[96:], 0x1000)
	le.PutUint32(opt[108:], 16) // data directories
	sect := b[0x58+240:]
	copy(sect, ".text")
	le.PutUint32(sect[8:], 1)           // virtual size
	le.PutUint32(sect[12:], sectAlign)  // virtual address
	le.PutUint32(sect[16:], fileAlign)  // raw size
	le.PutUint32(sect[20:], fileAlign)  // raw offset
	le.PutUint32(sect[36:], 0x60000020) // code, execute, read
	b[fileAlign] = 0xc3                 // ret
	return b
}

// A Windows build carries asuka.json's ID and scheme, which the host
// registers and runs once by.
func TestWindowsLdflags(t *testing.T) {
	app := &goApp{name: "notes", cfg: &appConfig{ID: "com.example.notes", Scheme: "notes", Version: "1.2", Build: 3}}
	f := windowsLdflags(app, true)
	for _, want := range []string{
		`"github.com/egoist/asuka/internal/boot.ID=com.example.notes"`,
		`"github.com/egoist/asuka/internal/boot.Scheme=notes"`,
		"-H windowsgui",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("windowsLdflags: %s, want %s in it", f, want)
		}
	}
	if strings.Contains(f, "MultipleInstances") {
		t.Errorf("a single instance's: %s", f)
	}
	app.cfg.Scheme = ""
	if f := windowsLdflags(app, false); strings.Contains(f, "boot.Scheme") {
		t.Errorf("windowsLdflags without a scheme: %s", f)
	}
	no := false
	app.cfg.SingleInstance = &no
	if f := windowsLdflags(app, false); !strings.Contains(f, `"github.com/egoist/asuka/internal/boot.MultipleInstances=1"`) {
		t.Errorf("singleInstance false: %s", f)
	}
}

// SQLite's library for Windows needs only what every Windows 10 and 11
// has: the Universal C Runtime (its api-ms-win-crt sets) and KERNEL32,
// nothing of MinGW's (libgcc, libwinpthread); and it exports only its
// entry points.
func TestWindowsSQLiteImports(t *testing.T) {
	for arch, machine := range map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64} {
		dll := filepath.Join(t.TempDir(), "asqlite.dll")
		if filepath.Base(dll) != "asqlite.dll" {
			t.Errorf("%s: %s", arch, dll)
		}
		if err := writeTestPluginLib("windows-"+arch, dll); err != nil {
			t.Fatal(err)
		}
		f, err := pe.Open(dll)
		if err != nil {
			t.Fatal(err)
		}
		if f.Machine != machine {
			t.Errorf("%s: machine %#x", arch, f.Machine)
		}
		libs, err := f.ImportedLibraries()
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range libs {
			l = strings.ToLower(l)
			if l != "kernel32.dll" && !strings.HasPrefix(l, "api-ms-win-crt-") {
				t.Errorf("%s: imports %s", arch, l)
			}
		}
	}
}

// A Windows build of an app that uses asuka/sqlite puts the library for
// its architecture beside the .exe (sqlite/locate_windows.go), and is
// built without the copy the package embeds, with GOFLAGS' tags.
func TestBuildWindowsSQLite(t *testing.T) {
	t.Setenv("ASUKA_WINDOWS_CERT", "")
	t.Setenv("GOFLAGS", "-tags=extra")
	app := sqliteTestApp(t)
	for arch, machine := range map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64} {
		out := t.TempDir()
		if err := buildWindows(app, &buildOptions{out: out, arch: arch}); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(out, sanitizeExe(app.displayName()))
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if want := []string{"asqlite.dll", "sqliteapp.exe"}; !slices.Equal(names, want) {
			t.Errorf("%s: %v, want %v", arch, names, want)
		}
		f, err := pe.Open(filepath.Join(dir, "asqlite.dll"))
		if err != nil {
			t.Fatal(err)
		}
		if f.Machine != machine {
			t.Errorf("%s: the library's machine is %#x", arch, f.Machine)
		}
		f.Close()
		if tags := buildTags(t, filepath.Join(dir, "sqliteapp.exe")); tags != "extra,"+pluginTag {
			t.Errorf("%s: tags %q", arch, tags)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(app.dir, "*.syso")); len(m) > 0 {
		t.Errorf("left in the app: %v", m)
	}
}
