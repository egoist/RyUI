//go:build windows

package windows

import (
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// Toasts: Windows's notifications, through WinRT's
// ToastNotificationManager, for an app that isn't packaged. Such an app
// is known to the shell by an AppUserModelID: on a shortcut to it in the
// user's Start menu (System.AppUserModel.ID), as Win32 apps that show
// toasts make, and in the user's registry (Classes\AppUserModelId\<id>:
// its display name and icon, as the Windows App SDK does); the process
// takes that ID.

var (
	iidToastManagerStatics  = NewGUID("50AC103F-D235-4598-BBEF-98FE4D1A3AD4")
	iidToastManagerStatics2 = NewGUID("7AB93C52-0E48-4750-BA9D-1A4113981847")
	iidToastFactory         = NewGUID("04124B20-82C6-4229-B109-FD9ED4662B53")
	iidScheduledToastFact   = NewGUID("E7BED191-0BB9-4189-8394-31761B476FD7")
	iidToastNotification2   = NewGUID("9DFB9FD1-143A-490E-90BF-B9FBA7132DE7")
	iidScheduledToast       = NewGUID("79F577F8-0DE7-48CD-9740-9B370490C838")
	iidScheduledToast2      = NewGUID("A66EA09C-31B4-43B0-B5DD-7A40E85363B1")
	iidXmlDocument          = NewGUID("F7F3A506-1E87-42D6-BCFB-B8C809FA5494")
	iidXmlDocumentIO        = NewGUID("6CD0E74E-EE65-4489-9EBF-CA43E87BA637")

	procSetAppUserModelID = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procRegCreateKeyEx    = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueEx     = advapi32.NewProc("RegSetValueExW")
)

// toastGroup is the group of the app's toasts, which their tags (the
// notifications' IDs) are in.
const toastGroup = "asuka"

// Toaster shows an app's toasts.
type Toaster struct {
	aumid    string
	notifier uintptr
	statics  uintptr
}

// RegisterAppID registers aumid for an unpackaged app (its display name
// and icon, a file): a Start menu shortcut to exe named name, made if
// there's none or it's another's, with the toasts' activator clsid, and
// the user's registry; and has this process take it.
func RegisterAppID(aumid, name, icon, exe string, clsid GUID) error {
	if err := startShortcut(aumid, name, exe, clsid); err != nil {
		return err
	}
	var k uintptr
	path := `Software\Classes\AppUserModelId\` + aumid
	if r, _, _ := procRegCreateKeyEx.Call(hkeyCurrentUser, Str(path), 0, 0, 0, 0x20006, 0, uintptr(unsafe.Pointer(&k)), 0); r != 0 { // KEY_WRITE
		return fmt.Errorf("registering %s: %d", aumid, r)
	}
	set := func(name, v string) {
		u := UTF16(v)
		n := (len(UTF16Units(v)) + 1) * 2
		procRegSetValueEx.Call(k, Str(name), 0, 1, uintptr(unsafe.Pointer(u)), uintptr(n)) // REG_SZ
	}
	set("DisplayName", name)
	if icon != "" {
		set("IconUri", icon)
	}
	procRegCloseKey.Call(k)
	if Has(procSetAppUserModelID) {
		procSetAppUserModelID.Call(Str(aumid))
	}
	return nil
}

// NewToaster makes the notifier of aumid (registered: RegisterAppID).
func NewToaster(aumid string) (*Toaster, error) {
	statics, err := ActivationFactory("Windows.UI.Notifications.ToastNotificationManager", iidToastManagerStatics)
	if err != nil {
		return nil, err
	}
	id := NewHString(aumid)
	defer id.Delete()
	var n uintptr
	if hr := ComCall(statics, 7, uintptr(id), uintptr(unsafe.Pointer(&n))); Failed(hr) || n == 0 { // CreateToastNotifierWithId
		Release(statics)
		return nil, fmt.Errorf("toasts: %#x", uint32(hr))
	}
	return &Toaster{aumid: aumid, notifier: n, statics: statics}, nil
}

// Enabled reports whether the user lets the app show toasts
// (NotificationSetting Enabled).
// A newly registered app's setting isn't found until the shell has seen
// its shortcut: its toasts are on, as every app's are at first.
func (t *Toaster) Enabled() bool {
	var s int32
	hr := ComCall(t.notifier, 8, uintptr(unsafe.Pointer(&s))) // get_Setting
	return uint32(hr) == 0x80070490 || !Failed(hr) && s == 0  // ERROR_NOT_FOUND
}

// xmlDocument is an XmlDocument of xml: its IXmlDocument.
func xmlDocument(xml string) (uintptr, error) {
	doc, err := ActivateInstance("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return 0, err
	}
	defer Release(doc)
	io := QueryInterface(doc, &iidXmlDocumentIO)
	if io == 0 {
		return 0, errors.New("toasts: no XmlDocument")
	}
	s := NewHString(xml)
	hr := ComCall(io, 6, uintptr(s)) // LoadXml
	s.Delete()
	Release(io)
	if Failed(hr) {
		return 0, fmt.Errorf("toasts: bad XML: %#x", uint32(hr))
	}
	return QueryInterface(doc, &iidXmlDocument), nil
}

// Show shows a toast of xml now, tagged id; at, if not 0, schedules it
// for then (Unix milliseconds). quiet puts it in the notification center
// without a banner.
func (t *Toaster) Show(xml, id string, at int64, quiet bool) error {
	doc, err := xmlDocument(xml)
	if err != nil {
		return err
	}
	defer Release(doc)
	tag, group := NewHString(id), NewHString(toastGroup)
	defer tag.Delete()
	defer group.Delete()
	if at != 0 {
		f, err := ActivationFactory("Windows.UI.Notifications.ScheduledToastNotification", iidScheduledToastFact)
		if err != nil {
			return err
		}
		defer Release(f)
		// A DateTime: 100 ns since 1601.
		when := (at + 11644473600000) * 10000
		var st uintptr
		if hr := ComCall(f, 6, doc, uintptr(when), uintptr(unsafe.Pointer(&st))); Failed(hr) || st == 0 { // CreateScheduledToastNotification
			return fmt.Errorf("toasts: %#x", uint32(hr))
		}
		defer Release(st)
		ComCall(st, 10, uintptr(tag)) // put_Id
		if s2 := QueryInterface(st, &iidScheduledToast2); s2 != 0 {
			ComCall(s2, 6, uintptr(tag))   // put_Tag
			ComCall(s2, 8, uintptr(group)) // put_Group
			if quiet {
				ComCall(s2, 10, 1) // put_SuppressPopup
			}
			Release(s2)
		}
		if hr := ComCall(t.notifier, 9, st); Failed(hr) { // AddToSchedule
			return fmt.Errorf("toasts: %#x", uint32(hr))
		}
		return nil
	}
	f, err := ActivationFactory("Windows.UI.Notifications.ToastNotification", iidToastFactory)
	if err != nil {
		return err
	}
	defer Release(f)
	var n uintptr
	if hr := ComCall(f, 6, doc, uintptr(unsafe.Pointer(&n))); Failed(hr) || n == 0 { // CreateToastNotification
		return fmt.Errorf("toasts: %#x", uint32(hr))
	}
	defer Release(n)
	if n2 := QueryInterface(n, &iidToastNotification2); n2 != 0 {
		ComCall(n2, 6, uintptr(tag))   // put_Tag
		ComCall(n2, 8, uintptr(group)) // put_Group
		if quiet {
			ComCall(n2, 10, 1) // put_SuppressPopup
		}
		Release(n2)
	}
	if hr := ComCall(t.notifier, 6, n); Failed(hr) { // Show
		return fmt.Errorf("toasts: %#x", uint32(hr))
	}
	return nil
}

// Cancel takes a scheduled toast off the schedule.
func (t *Toaster) Cancel(id string) {
	var list uintptr
	if Failed(ComCall(t.notifier, 11, uintptr(unsafe.Pointer(&list)))) || list == 0 { // GetScheduledToastNotifications
		return
	}
	defer Release(list)
	var n uint32
	ComCall(list, 7, uintptr(unsafe.Pointer(&n))) // IVectorView.get_Size
	for i := range n {
		var st uintptr
		if Failed(ComCall(list, 6, uintptr(i), uintptr(unsafe.Pointer(&st)))) || st == 0 { // GetAt
			continue
		}
		var hs HString
		ComCall(st, 11, uintptr(unsafe.Pointer(&hs))) // get_Id
		if hs.String() == id {
			ComCall(t.notifier, 10, st) // RemoveFromSchedule
		}
		hs.Delete()
		Release(st)
	}
}

// history is the Action Center's history of the app's toasts.
func (t *Toaster) history() uintptr {
	s2 := QueryInterface(t.statics, &iidToastManagerStatics2)
	if s2 == 0 {
		return 0
	}
	defer Release(s2)
	var h uintptr
	ComCall(s2, 6, uintptr(unsafe.Pointer(&h))) // get_History
	return h
}

// Remove takes the toast tagged id out of the Action Center.
func (t *Toaster) Remove(id string) {
	h := t.history()
	if h == 0 {
		return
	}
	defer Release(h)
	tag, group, app := NewHString(id), NewHString(toastGroup), NewHString(t.aumid)
	ComCall(h, 8, uintptr(tag), uintptr(group), uintptr(app)) // RemoveGroupedTagWithId
	tag.Delete()
	group.Delete()
	app.Delete()
}

// RemoveAll empties the app's toasts in the Action Center.
func (t *Toaster) RemoveAll() {
	h := t.history()
	if h == 0 {
		return
	}
	defer Release(h)
	app := NewHString(t.aumid)
	ComCall(h, 12, uintptr(app)) // ClearWithId
	app.Delete()
}

// Close lets the notifier go.
func (t *Toaster) Close() {
	Release(t.notifier)
	Release(t.statics)
}

var procDrawIconEx = user32.NewProc("DrawIconEx")

// WriteIconPNG writes an icon, size pixels square, as a PNG file.
func WriteIconPNG(icon uintptr, size int32, path string) error {
	dc := CreateCompatibleDC(0)
	defer DeleteDC(dc)
	bmp, px := DIB(size, size)
	if bmp == 0 {
		return errors.New("no bitmap")
	}
	defer DeleteObject(bmp)
	old := SelectObject(dc, bmp)
	defer SelectObject(dc, old)
	if r, _, _ := procDrawIconEx.Call(dc, 0, 0, icon, uintptr(size), uintptr(size), 0, 0, 3); r == 0 { // DI_NORMAL
		return errors.New("DrawIconEx failed")
	}
	img := image.NewNRGBA(image.Rect(0, 0, int(size), int(size)))
	for i := 0; i+3 < len(px); i += 4 {
		a := px[i+3]
		un := func(c byte) byte {
			if a == 0 {
				return 0
			}
			return byte(min(int(c)*255/int(a), 255))
		}
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = un(px[i+2]), un(px[i+1]), un(px[i]), a
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

var (
	clsidShellLink      = NewGUID("00021401-0000-0000-C000-000000000046")
	iidShellLinkW       = NewGUID("000214F9-0000-0000-C000-000000000046")
	iidPropertyStore    = NewGUID("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99")
	iidPersistFile      = NewGUID("0000010B-0000-0000-C000-000000000046")
	pkeyAppUserModelFmt = NewGUID("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3")
)

// startShortcut makes the user's Start menu shortcut to exe, name.lnk,
// with the AppUserModelID aumid and the toasts' activator clsid, unless
// it's there with them.
func startShortcut(aumid, name, exe string, clsid GUID) error {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		return errors.New("no APPDATA")
	}
	path := dir + `\Microsoft\Windows\Start Menu\Programs\` + name + ".lnk"
	link := CoCreate(&clsidShellLink, &iidShellLinkW)
	if link == 0 {
		return errors.New("no shell links")
	}
	defer Release(link)
	pf := QueryInterface(link, &iidPersistFile)
	if pf == 0 {
		return errors.New("no shell links")
	}
	defer Release(pf)
	ps := QueryInterface(link, &iidPropertyStore)
	if ps != 0 {
		defer Release(ps)
	}
	type propKey struct {
		Fmt GUID
		PID uint32
	}
	type propVariant struct {
		VT       uint16
		_        [3]uint16
		Val, Pad uintptr
	}
	idKey := propKey{pkeyAppUserModelFmt, pkeyAppUserModelIDPID}
	clsidKey := propKey{pkeyAppUserModelFmt, pkeyToastActivatorCLSID}
	if _, err := os.Stat(path); err == nil && !Failed(ComCall(pf, 5, Str(path), 0)) { // Load, STGM_READ
		buf := make([]uint16, 1024)
		ComCall(link, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0) // GetPath
		same := strings.EqualFold(syscall.UTF16ToString(buf), exe)
		if same && ps != 0 {
			var v propVariant
			if !Failed(ComCall(ps, 5, uintptr(unsafe.Pointer(&idKey)), uintptr(unsafe.Pointer(&v)))) { // GetValue
				same = v.VT == 31 && GoStr(v.Val) == aumid
				procPropVariantClear.Call(uintptr(unsafe.Pointer(&v)))
			}
			v = propVariant{}
			if same && !Failed(ComCall(ps, 5, uintptr(unsafe.Pointer(&clsidKey)), uintptr(unsafe.Pointer(&v)))) {
				same = v.VT == 72 && v.Val != 0 && *(*GUID)(native(v.Val)) == clsid // VT_CLSID
				procPropVariantClear.Call(uintptr(unsafe.Pointer(&v)))
			}
		}
		if same {
			return nil
		}
	}
	ComCall(link, 20, Str(exe)) // SetPath
	if ps != 0 {
		id := UTF16(aumid)
		pv := propVariant{VT: 31, Val: uintptr(unsafe.Pointer(id))}                   // VT_LPWSTR
		ComCall(ps, 6, uintptr(unsafe.Pointer(&idKey)), uintptr(unsafe.Pointer(&pv))) // SetValue
		c := clsid
		pc := propVariant{VT: 72, Val: uintptr(unsafe.Pointer(&c))} // VT_CLSID
		ComCall(ps, 6, uintptr(unsafe.Pointer(&clsidKey)), uintptr(unsafe.Pointer(&pc)))
		ComCall(ps, 7) // Commit
		runtime.KeepAlive(id)
		runtime.KeepAlive(&c)
	}
	if hr := ComCall(pf, 6, Str(path), 1); Failed(hr) { // Save
		return fmt.Errorf("the Start menu's shortcut: %#x", uint32(hr))
	}
	return nil
}

var (
	iidToastHistory2    = NewGUID("3BC3D253-2F31-4092-9129-8AD5ABF067DA")
	iidXmlNodeSerialize = NewGUID("5CC5B382-E6DD-4991-ABEF-06D8D2E7BD0C")
)

// ToastShown is one of the app's toasts in the notification center: its
// tag and its XML's launch attribute (the toast's arguments).
type ToastShown struct {
	Tag, Launch string
}

// Showing is the app's toasts in the notification center, oldest first.
func (t *Toaster) Showing() ([]ToastShown, error) {
	h := t.history()
	if h == 0 {
		return nil, errNoHistory
	}
	defer Release(h)
	h2 := QueryInterface(h, &iidToastHistory2)
	if h2 == 0 {
		return nil, errNoHistory
	}
	defer Release(h2)
	app := NewHString(t.aumid)
	defer app.Delete()
	var list uintptr
	if hr := ComCall(h2, 7, uintptr(app), uintptr(unsafe.Pointer(&list))); Failed(hr) || list == 0 { // GetHistoryWithId
		return nil, fmt.Errorf("toasts' history: %#x", uint32(hr))
	}
	defer Release(list)
	var n uint32
	ComCall(list, 7, uintptr(unsafe.Pointer(&n))) // get_Size
	out := []ToastShown{}
	for i := range n {
		var tn uintptr
		if Failed(ComCall(list, 6, uintptr(i), uintptr(unsafe.Pointer(&tn)))) || tn == 0 { // GetAt
			continue
		}
		var s ToastShown
		if n2 := QueryInterface(tn, &iidToastNotification2); n2 != 0 {
			var tag HString
			ComCall(n2, 7, uintptr(unsafe.Pointer(&tag))) // get_Tag
			s.Tag = tag.String()
			tag.Delete()
			Release(n2)
		}
		var doc uintptr
		if !Failed(ComCall(tn, 6, uintptr(unsafe.Pointer(&doc)))) && doc != 0 { // get_Content
			if ser := QueryInterface(doc, &iidXmlNodeSerialize); ser != 0 {
				var x HString
				ComCall(ser, 6, uintptr(unsafe.Pointer(&x))) // GetXml
				s.Launch = toastLaunch(x.String())
				x.Delete()
				Release(ser)
			}
			Release(doc)
		}
		Release(tn)
		out = append(out, s)
	}
	return out, nil
}

// toastLaunch is a toast's XML's launch attribute.
func toastLaunch(doc string) string {
	var t struct {
		Launch string `xml:"launch,attr"`
	}
	xml.Unmarshal([]byte(doc), &t)
	return t.Launch
}
