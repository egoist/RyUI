//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
)

// asuka/sdk/contacts on a Mac, with the Contacts framework as on iOS:
// "request" answers u8 access (not asked, denied, limited, full), "list"
// (JSON: name, limit) and "pick" contacts as iOS's JSON, "add" (a
// contact's JSON) u8 saved. macOS has no new-contact screen: "add" asks
// with an alert over the window (the contact's name, phones and emails)
// and saves it on Add. "pick" is the system's CNContactPicker, a popover
// from the top of the window. macOS asks for access once, with the app's
// NSContactsUsageDescription.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["contacts"] = h.contactsModule() })
}

// Seams: what asks the system, which tests replace (a test binary must
// never meet a permission prompt).
var (
	// contactsStatus is CNContactStore's authorization status for contacts.
	contactsStatus = func() int {
		if !contactsLoad() {
			return 1
		}
		return SendInt(Class("CNContactStore"), "authorizationStatusForEntityType:", 0)
	}
	// contactsRequest asks the user, then calls done on the main thread.
	contactsRequest = func(done func()) {
		var b objc.Block
		b = NewBlock(func(_ objc.Block, ok bool, err ID) {
			theHost.Post(func() {
				b.Release()
				done()
			})
		})
		Send(contactsStore(), "requestAccessForEntityType:completionHandler:", 0, uintptr(b))
	}
)

var contactsStoreObj ID

func contactsLoad() bool {
	return framework("Contacts") != 0 && HasClass("CNContactStore")
}

func contactsStore() ID {
	if contactsStoreObj == 0 {
		contactsStoreObj = New("CNContactStore")
	}
	return contactsStoreObj
}

// contactsAccess is the status as asuka/sdk/contacts' Access: not asked,
// denied, limited, full.
func contactsAccess() byte {
	switch contactsStatus() {
	case 3: // authorized
		return 3
	case 1, 2: // restricted, denied
		return 1
	case 0:
		return 0
	}
	return 2 // limited
}

// contactsKeys are the keys a contact is fetched with.
func contactsKeys() ID {
	lib := framework("Contacts")
	keys := []ID{Send(Class("CNContactFormatter"), "descriptorForRequiredKeysForStyle:", 0)}
	for _, k := range []string{"CNContactGivenNameKey", "CNContactFamilyNameKey", "CNContactOrganizationNameKey", "CNContactJobTitleKey",
		"CNContactPhoneNumbersKey", "CNContactEmailAddressesKey", "CNContactBirthdayKey"} {
		keys = append(keys, Const(lib, k))
	}
	return Array(keys...)
}

// contactsJSON is a contact as iOS's JSON (the keys it doesn't have
// empty).
func contactsJSON(c ID) map[string]any {
	lib := framework("Contacts")
	has := func(k string) bool { return SendBool(c, "isKeyAvailable:", uintptr(Const(lib, k))) }
	str := func(k, sel string) string {
		if !has(k) {
			return ""
		}
		return GoString(Send(c, sel))
	}
	label := func(l ID) string {
		if l == 0 {
			return ""
		}
		return GoString(Send(Class("CNLabeledValue"), "localizedStringForLabel:", uintptr(l)))
	}
	items := func(k, sel string, phone bool) []map[string]string {
		out := []map[string]string{}
		if !has(k) {
			return out
		}
		for _, lv := range ArrayItems(Send(c, sel)) {
			v := Send(lv, "value")
			if phone {
				v = Send(v, "stringValue")
			}
			out = append(out, map[string]string{"label": label(Send(lv, "label")), "value": GoString(v)})
		}
		return out
	}
	birthday := ""
	if has("CNContactBirthdayKey") {
		if b := Send(c, "birthday"); b != 0 {
			birthday = contactsBirthday(SendInt(b, "year"), SendInt(b, "month"), SendInt(b, "day"))
		}
	}
	name := ""
	if s := Send(Class("CNContactFormatter"), "stringFromContact:style:", uintptr(c), 0); s != 0 {
		name = GoString(s)
	}
	return map[string]any{
		"id":           GoString(Send(c, "identifier")),
		"name":         name,
		"givenName":    str("CNContactGivenNameKey", "givenName"),
		"familyName":   str("CNContactFamilyNameKey", "familyName"),
		"organization": str("CNContactOrganizationNameKey", "organizationName"),
		"jobTitle":     str("CNContactJobTitleKey", "jobTitle"),
		"phones":       items("CNContactPhoneNumbersKey", "phoneNumbers", true),
		"emails":       items("CNContactEmailAddressesKey", "emailAddresses", false),
		"birthday":     birthday,
		"note":         "", // the note needs an entitlement Apple grants on request
	}
}

// nsUndefined is NSDateComponentUndefined.
const nsUndefined = int(^uint(0) >> 1)

// contactsBirthday is a birthday as iOS's JSON has it: YYYY-MM-DD, or
// --MM-DD without a year; "" without a month or day.
func contactsBirthday(year, month, day int) string {
	if month == nsUndefined || day == nsUndefined || month <= 0 || day <= 0 {
		return ""
	}
	y := "-"
	if year != nsUndefined && year > 0 {
		y = fmt.Sprintf("%04d", year)
	}
	return y + fmt.Sprintf("-%02d-%02d", month, day)
}

// contactsArgs is "add"'s JSON.
type contactsArgs struct {
	Name         string `json:"name"`
	GivenName    string `json:"givenName"`
	FamilyName   string `json:"familyName"`
	Organization string `json:"organization"`
	JobTitle     string `json:"jobTitle"`
	Phones       []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"phones"`
	Emails []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"emails"`
	Birthday string `json:"birthday"`
}

// parseBirthday reads YYYY-MM-DD or --MM-DD.
func parseBirthday(s string) (year, month, day int, ok bool) {
	p := strings.Split(s, "-")
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch len(p) {
	case 3:
		year, month, day = num(p[0]), num(p[1]), num(p[2])
	case 4: // --MM-DD
		month, day = num(p[2]), num(p[3])
	default:
		return 0, 0, 0, false
	}
	return year, month, day, month > 0 && day > 0
}

// contactsBuild is a new CNMutableContact (autoreleased) of "add"'s JSON.
func contactsBuild(o contactsArgs) ID {
	lib := framework("Contacts")
	c := Autorelease(New("CNMutableContact"))
	given, family := o.GivenName, o.FamilyName
	if given == "" && family == "" {
		given = o.Name
	}
	Send(c, "setGivenName:", uintptr(String(given)))
	Send(c, "setFamilyName:", uintptr(String(family)))
	Send(c, "setOrganizationName:", uintptr(String(o.Organization)))
	Send(c, "setJobTitle:", uintptr(String(o.JobTitle)))
	labeled := func(label string, def string, v ID) ID {
		l := Const(lib, def)
		if label != "" {
			l = String(label)
		}
		return Send(Class("CNLabeledValue"), "labeledValueWithLabel:value:", uintptr(l), uintptr(v))
	}
	var phones, emails []ID
	for _, p := range o.Phones {
		phones = append(phones, labeled(p.Label, "CNLabelPhoneNumberMobile", Send(Class("CNPhoneNumber"), "phoneNumberWithStringValue:", uintptr(String(p.Value)))))
	}
	for _, e := range o.Emails {
		emails = append(emails, labeled(e.Label, "CNLabelHome", String(e.Value)))
	}
	Send(c, "setPhoneNumbers:", uintptr(Array(phones...)))
	Send(c, "setEmailAddresses:", uintptr(Array(emails...)))
	if y, m, d, ok := parseBirthday(o.Birthday); ok {
		dc := Autorelease(New("NSDateComponents"))
		if y > 0 {
			Send(dc, "setYear:", uintptr(y))
		}
		Send(dc, "setMonth:", uintptr(m))
		Send(dc, "setDay:", uintptr(d))
		Send(c, "setBirthday:", uintptr(dc))
	}
	return c
}

// contactsAlertText is the add alert's message and informative text: the
// name, then the organization, phones and emails, a line each.
func contactsAlertText(o contactsArgs) (title, info string) {
	title = strings.TrimSpace(o.GivenName + " " + o.FamilyName)
	if title == "" {
		title = o.Name
	}
	if title == "" {
		title = o.Organization
	}
	var lines []string
	if o.Organization != "" && title != o.Organization {
		org := o.Organization
		if o.JobTitle != "" {
			org = o.JobTitle + " · " + org
		}
		lines = append(lines, org)
	}
	item := func(label, v string) string {
		if label == "" {
			return v
		}
		return label + ": " + v
	}
	for _, p := range o.Phones {
		lines = append(lines, item(p.Label, p.Value))
	}
	for _, e := range o.Emails {
		lines = append(lines, item(e.Label, e.Value))
	}
	if o.Birthday != "" {
		lines = append(lines, o.Birthday)
	}
	return title, strings.Join(lines, "\n")
}

// contactsAllowed asks if it's not been asked, then calls then with
// whether contacts may be read and written.
func contactsAllowed(then func(bool)) {
	if contactsStatus() == 0 {
		contactsRequest(func() { then(contactsAccess() >= 2) })
		return
	}
	then(contactsAccess() >= 2)
}

// contactsSave saves a contact, once the user said Add.
func contactsSave(o contactsArgs, reply func([]byte, error)) {
	contactsAllowed(func(ok bool) {
		if !ok {
			reply(nil, errNotAllowed)
			return
		}
		WithPool(func() {
			req := New("CNSaveRequest")
			defer Release(req)
			Send(req, "addContact:toContainerWithIdentifier:", uintptr(contactsBuild(o)), 0)
			var err ID
			if !SendBool(contactsStore(), "executeSaveRequest:error:", uintptr(req), uintptr(unsafe.Pointer(&err))) {
				reply(nil, errorText(err, "couldn't save the contact"))
				return
			}
			reply([]byte{1}, nil)
		})
	})
}

var (
	contactsPicker   ID
	contactsPicking  func([]byte, error)
	contactsDelegate ID
)

func contactsPickerDelegate() ID {
	if contactsDelegate == 0 {
		DefineClass("AsukaContactsPickerDelegate", "NSObject", []string{"CNContactPickerDelegate"}, []objc.MethodDef{
			Method("contactPicker:didSelectContact:", func(self ID, _ SEL, p, c ID) { contactsPicked(c) }),
			Method("contactPickerDidClose:", func(self ID, _ SEL, p ID) { contactsPicked(0) }),
		})
		contactsDelegate = New("AsukaContactsPickerDelegate")
	}
	return contactsDelegate
}

// contactsPicked answers pick: the contact, fetched again with every key
// if access allows (the picker's carries only the ones it shows), or ""
// once the picker closes without one.
func contactsPicked(c ID) {
	reply := contactsPicking
	if reply == nil {
		return
	}
	contactsPicking = nil
	if c == 0 {
		reply(nil, nil)
		return
	}
	if contactsAccess() == 3 {
		if full := Send(contactsStore(), "unifiedContactWithIdentifier:keysToFetch:error:", uintptr(Send(c, "identifier")), uintptr(contactsKeys()), 0); full != 0 {
			c = full
		}
	}
	b, err := json.Marshal(contactsJSON(c))
	reply(b, err)
}

func (h *Host) contactsModule() map[string]method {
	return map[string]method{
		"request": func(_ []byte, reply func([]byte, error)) {
			if contactsStatus() != 0 {
				reply([]byte{contactsAccess()}, nil)
				return
			}
			contactsRequest(func() { reply([]byte{contactsAccess()}, nil) })
		},
		"list": func(args []byte, reply func([]byte, error)) {
			if contactsAccess() < 2 {
				reply(nil, errNotAllowed)
				return
			}
			var o struct {
				Name  string `json:"name"`
				Limit int    `json:"limit"`
			}
			json.Unmarshal(args, &o)
			store := contactsStore()
			h.offMain(func() ([]byte, error) {
				req := Send(Send(Class("CNContactFetchRequest"), "alloc"), "initWithKeysToFetch:", uintptr(contactsKeys()))
				defer Release(req)
				Send(req, "setSortOrder:", 1) // CNContactSortOrderUserDefault
				if o.Name != "" {
					Send(req, "setPredicate:", uintptr(Send(Class("CNContact"), "predicateForContactsMatchingName:", uintptr(String(o.Name)))))
				}
				out := []map[string]any{}
				each := NewBlock(func(_ objc.Block, c ID, stop *bool) {
					out = append(out, contactsJSON(c))
					if o.Limit > 0 && len(out) >= o.Limit {
						*stop = true
					}
				})
				defer each.Release()
				var err ID
				if !SendBool(store, "enumerateContactsWithFetchRequest:error:usingBlock:", uintptr(req), uintptr(unsafe.Pointer(&err)), uintptr(each)) {
					return nil, errorText(err, "couldn't read the contacts")
				}
				return json.Marshal(out)
			}, reply)
		},
		"pick": func(_ []byte, reply func([]byte, error)) {
			if !contactsLoad() || framework("ContactsUI") == 0 || !HasClass("CNContactPicker") {
				reply(nil, errors.New("no contact picker"))
				return
			}
			if contactsPicking != nil { // one at a time
				contactsPicking(nil, nil)
				contactsPicking = nil
			}
			if contactsPicker == 0 {
				contactsPicker = New("CNContactPicker")
				Send(contactsPicker, "setDelegate:", uintptr(contactsPickerDelegate()))
			}
			contactsPicking = reply
			w := h.keyWin()
			b := MsgRect(w.content, Sel("bounds"))
			// From the top of the window, hanging down (the content is
			// flipped: its max y edge is below).
			msgShowPicker(contactsPicker, Sel("showRelativeToRect:ofView:preferredEdge:"),
				Rect{Origin: Point{X: b.Size.Width / 2, Y: 0}, Size: Size{Width: 1, Height: 1}}, w.content, 3)
		},
		"add": func(args []byte, reply func([]byte, error)) {
			var o contactsArgs
			if err := json.Unmarshal(args, &o); err != nil {
				reply(nil, errors.New("contacts.add: bad arguments"))
				return
			}
			if !contactsLoad() {
				reply(nil, errors.New("no contacts"))
				return
			}
			title, info := contactsAlertText(o)
			h.personalConfirm(title, info, l10n.AddToContacts(h.locale), func(ok bool) {
				if !ok {
					reply([]byte{0}, nil)
					return
				}
				contactsSave(o, reply)
			})
		},
	}
}

// personalConfirm asks with an alert over the window in use: its Add button
// (the default) or Cancel (Escape) calls done with whether it was Add.
func (h *Host) personalConfirm(title, info, add string, done func(bool)) {
	a := New("NSAlert")
	Send(a, "setMessageText:", uintptr(String(title)))
	Send(a, "setInformativeText:", uintptr(String(info)))
	Send(a, "addButtonWithTitle:", uintptr(String(add)))
	cancel := Send(a, "addButtonWithTitle:", uintptr(String(l10n.Cancel(h.locale))))
	Send(cancel, "setKeyEquivalent:", uintptr(String("\x1b")))
	var b objc.Block
	b = NewBlock(func(_ objc.Block, resp int) {
		h.Post(func() {
			b.Release()
			Release(a)
		})
		done(resp == 1000) // NSAlertFirstButtonReturn
	})
	Send(a, "beginSheetModalForWindow:completionHandler:", uintptr(h.keyWin().window), uintptr(b))
}
