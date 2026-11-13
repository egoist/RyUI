//go:build linux && !android

package gtk

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

// Evolution Data Server: the address books and calendars GNOME's apps
// share (GNOME Contacts, GNOME Calendar, Evolution, the online
// accounts'), through libedataserver, libebook and libecal (and
// libical-glib), loaded the first time they're used: a system without
// them answers an error. Its calls are blocking D-Bus calls to EDS's
// factories, made off the main thread (each locks its goroutine to its
// thread while it calls); contacts cross as vCards and events as
// iCalendar text, which Go reads.

var eds struct {
	mu        sync.Mutex
	server    error // libedataserver
	book      error // libebook
	cal       error // libecal, libical-glib
	loadedSrv bool
	loadedBk  bool
	loadedCal bool
	registry  Ptr
	books     map[string]Ptr // EBookClient by source UID
	cals      map[string]Ptr // ECalClient by source UID

	registryNewSync    func(cancellable Ptr, err *Ptr) Ptr
	listEnabled        func(reg Ptr, ext string) Ptr
	refDefaultBook     func(reg Ptr) Ptr
	refDefaultCalendar func(reg Ptr) Ptr
	refSource          func(reg Ptr, uid string) Ptr
	sourceGetUID       func(src Ptr) Ptr
	sourceGetName      func(src Ptr) Ptr
	sourceGetParent    func(src Ptr) Ptr
	sourceGetExtension func(src Ptr, ext string) Ptr
	selectableColor    func(ext Ptr) Ptr
	clientIsReadonly   func(client Ptr) bool

	bookConnectSync func(src Ptr, wait uint32, cancellable Ptr, err *Ptr) Ptr
	bookGetContacts func(client Ptr, sexp string, out *Ptr, cancellable Ptr, err *Ptr) bool
	bookAddContact  func(client, contact Ptr, opflags uint32, uid *Ptr, cancellable Ptr, err *Ptr) bool
	contactFromCard func(vcard string) Ptr
	vcardToString   func(evc Ptr, format int32) Ptr

	calConnectSync     func(src Ptr, kind int32, wait uint32, cancellable Ptr, err *Ptr) Ptr
	calInstancesSync   func(client Ptr, start, end int64, cancellable, cb, data Ptr)
	calCreateSync      func(client, comp Ptr, opflags uint32, uid *Ptr, cancellable Ptr, err *Ptr) bool
	compFromString     func(s string) Ptr
	compAsString       func(comp Ptr) Ptr
	timeIsDate         func(t Ptr) bool
	timeIsUTC          func(t Ptr) bool
	timeGetDate        func(t Ptr, y, m, d *int32)
	timeGetTime        func(t Ptr, h, m, s *int32)
	timeGetTimezone    func(t Ptr) Ptr
	timeAsTimetWithZon func(t, zone Ptr) int64

	instanceCB  uintptr
	instanceFns map[uintptr]func(comp, start, end Ptr)
	nextFn      uintptr
}

type libFunc struct {
	p    any
	name string
}

// openLib opens the first of the names that's installed.
func openLib(names ...string) (uintptr, error) {
	var err error
	for _, n := range names {
		var h uintptr
		if h, err = purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			return h, nil
		}
	}
	return 0, fmt.Errorf("%s isn't installed (%v)", names[0], err)
}

func bindLib(lib uintptr, fs []libFunc) error {
	for _, f := range fs {
		p, err := purego.Dlsym(lib, f.name)
		if err != nil {
			return fmt.Errorf("evolution-data-server is too old (no %s)", f.name)
		}
		purego.RegisterFunc(f.p, p)
	}
	return nil
}

// loadServer opens libedataserver. eds.mu held.
func loadServer() error {
	if eds.loadedSrv {
		return eds.server
	}
	eds.loadedSrv = true
	lib, err := openLib("libedataserver-1.2.so.27", "libedataserver-1.2.so.26", "libedataserver-1.2.so.25", "libedataserver-1.2.so.24", "libedataserver-1.2.so")
	if err != nil {
		eds.server = fmt.Errorf("evolution-data-server: %v", err)
		return eds.server
	}
	eds.server = bindLib(lib, []libFunc{
		{&eds.registryNewSync, "e_source_registry_new_sync"},
		{&eds.listEnabled, "e_source_registry_list_enabled"},
		{&eds.refDefaultBook, "e_source_registry_ref_default_address_book"},
		{&eds.refDefaultCalendar, "e_source_registry_ref_default_calendar"},
		{&eds.refSource, "e_source_registry_ref_source"},
		{&eds.sourceGetUID, "e_source_get_uid"},
		{&eds.sourceGetName, "e_source_get_display_name"},
		{&eds.sourceGetParent, "e_source_get_parent"},
		{&eds.sourceGetExtension, "e_source_get_extension"},
		{&eds.selectableColor, "e_source_selectable_get_color"},
		{&eds.clientIsReadonly, "e_client_is_readonly"},
	})
	eds.books, eds.cals = map[string]Ptr{}, map[string]Ptr{}
	return eds.server
}

// LoadEBook opens Evolution Data Server's contacts libraries, once.
func LoadEBook() error {
	eds.mu.Lock()
	defer eds.mu.Unlock()
	if err := loadServer(); err != nil {
		return err
	}
	if eds.loadedBk {
		return eds.book
	}
	eds.loadedBk = true
	lib, err := openLib("libebook-1.2.so.21", "libebook-1.2.so.20", "libebook-1.2.so.19", "libebook-1.2.so")
	if err == nil {
		var cs uintptr
		cs, err = openLib("libebook-contacts-1.2.so.4", "libebook-contacts-1.2.so.3", "libebook-contacts-1.2.so")
		if err == nil {
			err = bindLib(lib, []libFunc{
				{&eds.bookConnectSync, "e_book_client_connect_sync"},
				{&eds.bookGetContacts, "e_book_client_get_contacts_sync"},
				{&eds.bookAddContact, "e_book_client_add_contact_sync"},
			})
		}
		if err == nil {
			err = bindLib(cs, []libFunc{
				{&eds.contactFromCard, "e_contact_new_from_vcard"},
				{&eds.vcardToString, "e_vcard_to_string"},
			})
		}
	}
	if err != nil {
		eds.book = fmt.Errorf("evolution-data-server's contacts: %v", err)
	}
	return eds.book
}

// LoadECal opens Evolution Data Server's calendar libraries, once.
func LoadECal() error {
	eds.mu.Lock()
	defer eds.mu.Unlock()
	if err := loadServer(); err != nil {
		return err
	}
	if eds.loadedCal {
		return eds.cal
	}
	eds.loadedCal = true
	ical, err := openLib("libical-glib.so.3", "libical-glib.so")
	var lib uintptr
	if err == nil {
		lib, err = openLib("libecal-2.0.so.3", "libecal-2.0.so.2", "libecal-2.0.so.1", "libecal-2.0.so")
	}
	if err == nil {
		err = bindLib(lib, []libFunc{
			{&eds.calConnectSync, "e_cal_client_connect_sync"},
			{&eds.calInstancesSync, "e_cal_client_generate_instances_sync"},
			{&eds.calCreateSync, "e_cal_client_create_object_sync"},
		})
	}
	if err == nil {
		err = bindLib(ical, []libFunc{
			{&eds.compFromString, "i_cal_component_new_from_string"},
			{&eds.compAsString, "i_cal_component_as_ical_string"},
			{&eds.timeIsDate, "i_cal_time_is_date"},
			{&eds.timeIsUTC, "i_cal_time_is_utc"},
			{&eds.timeGetDate, "i_cal_time_get_date"},
			{&eds.timeGetTime, "i_cal_time_get_time"},
			{&eds.timeGetTimezone, "i_cal_time_get_timezone"},
			{&eds.timeAsTimetWithZon, "i_cal_time_as_timet_with_zone"},
		})
	}
	if err != nil {
		eds.cal = fmt.Errorf("evolution-data-server's calendars: %v", err)
		return eds.cal
	}
	eds.instanceFns = map[uintptr]func(comp, start, end Ptr){}
	// ECalRecurInstanceCb: (icomp, instance_start, instance_end,
	// user_data, cancellable, error) → keep going.
	eds.instanceCB = purego.NewCallback(func(comp, start, end, data, cancellable, gerr uintptr) uintptr {
		eds.mu.Lock()
		f := eds.instanceFns[data]
		eds.mu.Unlock()
		if f != nil {
			f(comp, start, end)
		}
		return 1
	})
	return nil
}

// registry is EDS's source registry, made once. eds.mu held.
func registry() (Ptr, error) {
	if eds.registry != 0 {
		return eds.registry, nil
	}
	var gerr Ptr
	r := eds.registryNewSync(0, &gerr)
	if r == 0 {
		return 0, edsError(gerr, "no evolution-data-server registry")
	}
	eds.registry = r
	return r, nil
}

// edsError is a GError as an error (freed), else fallback.
func edsError(gerr Ptr, fallback string) error {
	if err := GError(gerr); err != nil {
		return err
	}
	return errors.New(fallback)
}

// EDSSource is an address book or a calendar.
type EDSSource struct {
	UID, Name, Account, Color string
	Default                   bool
}

// Extensions of the sources.
const (
	EDSAddressBook = "Address Book"
	EDSCalendar    = "Calendar"
)

// edsCall runs f with eds.mu held, its goroutine locked to its thread.
func edsCall(f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	eds.mu.Lock()
	defer eds.mu.Unlock()
	return f()
}

// EDSSources are the enabled sources of an extension (EDSAddressBook,
// EDSCalendar). Off the main thread.
func EDSSources(ext string) ([]EDSSource, error) {
	var out []EDSSource
	err := edsCall(func() error {
		reg, err := registry()
		if err != nil {
			return err
		}
		def := Ptr(0)
		if ext == EDSAddressBook {
			def = eds.refDefaultBook(reg)
		} else {
			def = eds.refDefaultCalendar(reg)
		}
		defUID := ""
		if def != 0 {
			defUID = GoString(eds.sourceGetUID(def))
			ObjectUnref(def)
		}
		list := eds.listEnabled(reg, ext)
		for l := list; l != 0; l = *At[Ptr](l + 8) {
			src := *At[Ptr](l)
			s := EDSSource{UID: GoString(eds.sourceGetUID(src)), Name: GoString(eds.sourceGetName(src))}
			s.Default = s.UID == defUID
			if p := GoString(eds.sourceGetParent(src)); p != "" {
				if ps := eds.refSource(reg, p); ps != 0 {
					s.Account = GoString(eds.sourceGetName(ps))
					ObjectUnref(ps)
				}
			}
			if ext == EDSCalendar {
				if e := eds.sourceGetExtension(src, ext); e != 0 {
					s.Color = GoString(eds.selectableColor(e))
				}
			}
			out = append(out, s)
			ObjectUnref(src)
		}
		if list != 0 {
			Call(Sym("g_list_free"), list)
		}
		return nil
	})
	return out, err
}

// client is a connected client of a source (cached): connect is
// e_book_client_connect_sync's or e_cal_client_connect_sync's. uid ""
// is the default source. eds.mu held.
func client(cache map[string]Ptr, uid string, ext string, connect func(src Ptr, err *Ptr) Ptr) (Ptr, error) {
	reg, err := registry()
	if err != nil {
		return 0, err
	}
	var src Ptr
	if uid == "" {
		if ext == EDSAddressBook {
			src = eds.refDefaultBook(reg)
		} else {
			src = eds.refDefaultCalendar(reg)
		}
		if src == 0 {
			return 0, errors.New("no default " + map[string]string{EDSAddressBook: "address book", EDSCalendar: "calendar"}[ext])
		}
		uid = GoString(eds.sourceGetUID(src))
	} else {
		src = eds.refSource(reg, uid)
		if src == 0 {
			return 0, fmt.Errorf("no %s %q", map[string]string{EDSAddressBook: "address book", EDSCalendar: "calendar"}[ext], uid)
		}
	}
	defer ObjectUnref(src)
	if c := cache[uid]; c != 0 {
		return c, nil
	}
	var gerr Ptr
	c := connect(src, &gerr)
	if c == 0 {
		return 0, edsError(gerr, "couldn't open "+uid)
	}
	cache[uid] = c
	return c, nil
}

func bookClient(uid string) (Ptr, error) {
	return client(eds.books, uid, EDSAddressBook, func(src Ptr, err *Ptr) Ptr { return eds.bookConnectSync(src, 30, 0, err) })
}

func calClient(uid string) (Ptr, error) {
	return client(eds.cals, uid, EDSCalendar, func(src Ptr, err *Ptr) Ptr { return eds.calConnectSync(src, 0, 30, 0, err) }) // E_CAL_CLIENT_SOURCE_TYPE_EVENTS
}

// EDSContacts are the vCards (3.0) of an address book's contacts that
// match a query (EDS's S-expression). Off the main thread.
func EDSContacts(uid, sexp string) ([]string, error) {
	var out []string
	err := edsCall(func() error {
		c, err := bookClient(uid)
		if err != nil {
			return err
		}
		var list, gerr Ptr
		if !eds.bookGetContacts(c, sexp, &list, 0, &gerr) {
			return edsError(gerr, "couldn't read the contacts")
		}
		for l := list; l != 0; l = *At[Ptr](l + 8) {
			contact := *At[Ptr](l)
			out = append(out, GoStringFree(eds.vcardToString(contact, 1))) // EVC_FORMAT_VCARD_30
			ObjectUnref(contact)
		}
		if list != 0 {
			Call(Sym("g_slist_free"), list)
		}
		return nil
	})
	return out, err
}

// EDSAddContact saves a contact (a vCard) in an address book (""
// the default); it answers its UID. Off the main thread.
func EDSAddContact(uid, vcard string) (string, error) {
	var added string
	err := edsCall(func() error {
		c, err := bookClient(uid)
		if err != nil {
			return err
		}
		contact := eds.contactFromCard(vcard)
		if contact == 0 {
			return errors.New("not a vCard")
		}
		defer ObjectUnref(contact)
		var out, gerr Ptr
		if !eds.bookAddContact(c, contact, 0, &out, 0, &gerr) {
			return edsError(gerr, "couldn't save the contact")
		}
		added = GoStringFree(out)
		return nil
	})
	return added, err
}

// EDSReadonly reports whether a calendar can't be written to.
func EDSCalendarReadonly(uid string) (bool, error) {
	var ro bool
	err := edsCall(func() error {
		c, err := calClient(uid)
		if err != nil {
			return err
		}
		ro = eds.clientIsReadonly(c)
		return nil
	})
	return ro, err
}

// EDSInstance is an occurrence of an event: its component (iCalendar
// text) and its times (ms since 1970; an all-day one's days at UTC
// midnight, its end the day after the last).
type EDSInstance struct {
	ICal           string
	StartMs, EndMs int64
	AllDay         bool
}

// EDSEvents are a calendar's occurrences of events between from and to
// (each of a repeating event's). Off the main thread.
func EDSEvents(uid string, from, to time.Time) ([]EDSInstance, error) {
	var out []EDSInstance
	err := edsCall(func() error {
		c, err := calClient(uid)
		if err != nil {
			return err
		}
		eds.nextFn++
		id := eds.nextFn
		eds.instanceFns[id] = func(comp, start, end Ptr) {
			s, allDay := icalMs(start)
			e, _ := icalMs(end)
			if end == 0 {
				e = s
			}
			out = append(out, EDSInstance{ICal: GoStringFree(eds.compAsString(comp)), StartMs: s, EndMs: e, AllDay: allDay})
		}
		eds.mu.Unlock() // the callback takes it
		eds.calInstancesSync(c, from.Unix(), to.Unix(), 0, eds.instanceCB, id)
		eds.mu.Lock()
		delete(eds.instanceFns, id)
		return nil
	})
	return out, err
}

// icalMs is an ICalTime in ms since 1970: a date at UTC midnight, a
// floating time in the local zone.
func icalMs(t Ptr) (int64, bool) {
	if t == 0 {
		return 0, false
	}
	var y, mo, d, h, mi, s int32
	eds.timeGetDate(t, &y, &mo, &d)
	if eds.timeIsDate(t) {
		return time.Date(int(y), time.Month(mo), int(d), 0, 0, 0, 0, time.UTC).UnixMilli(), true
	}
	eds.timeGetTime(t, &h, &mi, &s)
	if eds.timeIsUTC(t) {
		return time.Date(int(y), time.Month(mo), int(d), int(h), int(mi), int(s), 0, time.UTC).UnixMilli(), false
	}
	if z := eds.timeGetTimezone(t); z != 0 {
		return eds.timeAsTimetWithZon(t, z) * 1000, false
	}
	return time.Date(int(y), time.Month(mo), int(d), int(h), int(mi), int(s), 0, time.Local).UnixMilli(), false
}

// EDSAddEvent saves an event (a VEVENT's iCalendar text) in a calendar
// ("" the default); it answers its UID. Off the main thread.
func EDSAddEvent(uid, vevent string) (string, error) {
	var added string
	err := edsCall(func() error {
		c, err := calClient(uid)
		if err != nil {
			return err
		}
		comp := eds.compFromString(vevent)
		if comp == 0 {
			return errors.New("not an event")
		}
		defer ObjectUnref(comp)
		var out, gerr Ptr
		if !eds.calCreateSync(c, comp, 0, &out, 0, &gerr) {
			return edsError(gerr, "couldn't save the event")
		}
		added = GoStringFree(out)
		return nil
	})
	return added, err
}

// The contact picker's rows (libadwaita's).
var (
	AdwActionRowNew               func() Ptr
	AdwActionRowSetSubtitle       func(r Ptr, s string)
	AdwPreferencesRowSetTitle     func(r Ptr, s string)
	AdwPreferencesRowSetUseMarkup func(r Ptr, m bool)
	SearchEntrySetPlaceholder     func(e Ptr, s string)
)

func init() {
	bindings = append(bindings,
		binding{&AdwActionRowNew, "adw_action_row_new"},
		binding{&AdwActionRowSetSubtitle, "adw_action_row_set_subtitle"},
		binding{&AdwPreferencesRowSetTitle, "adw_preferences_row_set_title"},
		binding{&AdwPreferencesRowSetUseMarkup, "adw_preferences_row_set_use_markup"},
		binding{&SearchEntrySetPlaceholder, "gtk_search_entry_set_placeholder_text"},
	)
}
