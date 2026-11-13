//go:build windows

package windows

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"
)

// Speech: SAPI 5's voice (ISpVoice), speaking asynchronously, its events
// (an utterance's stream ending) posted to a window as a message.

var (
	clsidSpVoice       = NewGUID("96749377-3391-11D2-9EE3-00C04F797396")
	iidSpVoice         = NewGUID("6C44DF74-72B9-4992-A1EC-EF996E0422D4")
	clsidSpObjectToken = NewGUID("EF411752-3736-4CB4-9C8C-8EF4CCB58EFE")
	iidSpObjectToken   = NewGUID("14056589-E16C-11D2-BB90-00C04F8EE6C0")
)

// ISpVoice's methods.
const (
	spSetNotifyWindowMessage = 4
	spSetInterest            = 10
	spGetEvents              = 11
	spPause                  = 16
	spResume                 = 17
	spSetVoice               = 18
	spSpeak                  = 20
	spGetStatus              = 22
	spSetRate                = 28
	spSetVolume              = 30
)

// Voice is SAPI's voice.
type Voice struct{ v uintptr }

// NewVoice makes a voice that posts message msg to hwnd as it has events.
func NewVoice(hwnd uintptr, msg uint32) (*Voice, error) {
	v := CoCreate(&clsidSpVoice, &iidSpVoice)
	if v == 0 {
		return nil, errors.New("SAPI's voice isn't there")
	}
	ComCall(v, spSetNotifyWindowMessage, hwnd, uintptr(msg), 0, 0)
	// SPFEI(SPEI_END_INPUT_STREAM) | SPFEI_FLAGCHECK: (1<<2) | (1<<30) | (1<<33).
	const interest = 1<<2 | 1<<30 | 1<<33
	ComCall(v, spSetInterest, interest, interest)
	return &Voice{v: v}, nil
}

// Speak speaks text (SAPI's XML, if xml) after what's queued, or instead
// of it if purge, and returns its stream's number.
func (v *Voice) Speak(text string, xml, purge bool) (uint32, error) {
	flags := uintptr(1) // SPF_ASYNC
	if purge {
		flags |= 2 // SPF_PURGEBEFORESPEAK
	}
	if xml {
		flags |= 8 // SPF_IS_XML
	} else {
		flags |= 0x10 // SPF_IS_NOT_XML
	}
	var stream uint32
	p := UTF16(text)
	if hr := ComCall(v.v, spSpeak, uintptr(unsafe.Pointer(p)), flags, uintptr(unsafe.Pointer(&stream))); Failed(hr) {
		return 0, errors.New("SAPI couldn't speak")
	}
	return stream, nil
}

// Stop stops speaking, and drops what's queued.
func (v *Voice) Stop() { ComCall(v.v, spSpeak, 0, 2|1, 0) } // SPF_PURGEBEFORESPEAK | SPF_ASYNC

func (v *Voice) Pause()  { ComCall(v.v, spPause) }
func (v *Voice) Resume() { ComCall(v.v, spResume) }

// SetRate sets the rate, -10 to 10 (0 normal, 10 three times as fast).
func (v *Voice) SetRate(r int) { ComCall(v.v, spSetRate, uintptr(int32(min(max(r, -10), 10)))) }

// SetVolume sets the volume, 0 to 100.
func (v *Voice) SetVolume(vol int) { ComCall(v.v, spSetVolume, uintptr(uint16(min(max(vol, 0), 100)))) }

// SetVoice uses the voice of a token ID (a key of Speech's Voices\Tokens),
// or the default ("").
func (v *Voice) SetVoice(id string) bool {
	if id == "" {
		return !Failed(ComCall(v.v, spSetVoice, 0)) // the default
	}
	t := CoCreate(&clsidSpObjectToken, &iidSpObjectToken)
	if t == 0 {
		return false
	}
	defer Release(t)
	if Failed(ComCall(t, 15, 0, Str(id), 0)) { // SetId
		return false
	}
	return !Failed(ComCall(v.v, spSetVoice, t))
}

// Speaking reports whether the voice is speaking (or paused in its
// speech).
func (v *Voice) Speaking() bool {
	// SPVOICESTATUS: the current and last streams, the last result, then
	// the running state (SPRS_IS_SPEAKING 2).
	var st [64]uint32
	ComCall(v.v, spGetStatus, uintptr(unsafe.Pointer(&st[0])), 0)
	return st[3]&2 != 0
}

// Ended is the streams that ended since asked last.
func (v *Voice) Ended() []uint32 {
	var out []uint32
	for range 64 {
		// SPEVENT: u16 id, u16 lParam's type, u32 stream, u64 offset, wParam, lParam.
		var ev struct {
			ID, ParamType uint16
			Stream        uint32
			Offset        uint64
			W, L          uintptr
		}
		var got uint32
		if Failed(ComCall(v.v, spGetEvents, 1, uintptr(unsafe.Pointer(&ev)), uintptr(unsafe.Pointer(&got)))) || got == 0 {
			break
		}
		if ev.ID == 2 { // SPEI_END_INPUT_STREAM
			out = append(out, ev.Stream)
		}
		if ev.ParamType == 1 || ev.ParamType == 2 { // SPET_LPARAM_IS_TOKEN or _OBJECT: a reference
			Release(ev.L)
		}
	}
	return out
}

// Close lets the voice go.
func (v *Voice) Close() { Release(v.v) }

// VoiceInfo is an installed voice: its token ID, name and language (a
// BCP 47 tag).
type VoiceInfo struct{ ID, Name, Language string }

// Voices are SAPI's installed voices (Speech's Voices\Tokens, the 64-bit
// registry view).
func Voices() []VoiceInfo {
	const base = `SOFTWARE\Microsoft\Speech\Voices\Tokens`
	var out []VoiceInfo
	for _, key := range regSubkeys(0x80000002, base) {
		path := base + `\` + key
		name := regValue(0x80000002, path, "")
		lcids := regValue(0x80000002, path+`\Attributes`, "Language")
		lang := ""
		if l := strings.Split(lcids, ";")[0]; l != "" {
			lang = lcidName(l)
		}
		out = append(out, VoiceInfo{ID: `HKEY_LOCAL_MACHINE\` + path, Name: name, Language: lang})
	}
	return out
}

var (
	procRegOpenKeyEx    = advapi32.NewProc("RegOpenKeyExW")
	procRegEnumKeyEx    = advapi32.NewProc("RegEnumKeyExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procLCIDToLocale    = kernel32.NewProc("LCIDToLocaleName")
)

const keyRead64 = 0x20019 | 0x0100 // KEY_READ | KEY_WOW64_64KEY

func regSubkeys(root uintptr, path string) []string {
	var k uintptr
	if r, _, _ := procRegOpenKeyEx.Call(root, Str(path), 0, keyRead64, uintptr(unsafe.Pointer(&k))); r != 0 {
		return nil
	}
	defer procRegCloseKey.Call(k)
	var out []string
	for i := 0; i < 256; i++ {
		var buf [256]uint16
		n := uint32(len(buf))
		if r, _, _ := procRegEnumKeyEx.Call(k, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0); r != 0 {
			break
		}
		out = append(out, syscall.UTF16ToString(buf[:n]))
	}
	return out
}

func regValue(root uintptr, path, name string) string {
	var k uintptr
	if r, _, _ := procRegOpenKeyEx.Call(root, Str(path), 0, keyRead64, uintptr(unsafe.Pointer(&k))); r != 0 {
		return ""
	}
	defer procRegCloseKey.Call(k)
	var buf [512]uint16
	n := uint32(len(buf) * 2)
	if r, _, _ := procRegQueryValueEx.Call(k, StrOpt(name), 0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n/2])
}

// lcidName is the BCP 47 tag of a hexadecimal LCID ("409": en-US).
func lcidName(hex string) string {
	var lcid uint64
	for _, c := range strings.ToLower(hex) {
		switch {
		case c >= '0' && c <= '9':
			lcid = lcid*16 + uint64(c-'0')
		case c >= 'a' && c <= 'f':
			lcid = lcid*16 + uint64(c-'a'+10)
		}
	}
	var buf [85]uint16
	n, _, _ := procLCIDToLocale.Call(uintptr(lcid), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}
