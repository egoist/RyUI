//go:build linux && !android

package gtk

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// GStreamer (ryui/audio's players and recorder) and geocode-glib
// (ryui/location's place names), opened the first time they're used: a
// system without them answers an error.

// Gst is GStreamer's core, bound by LoadGst.
var Gst struct {
	InitCheck          func(argc, argv Ptr, err *Ptr) bool
	ElementFactoryMake func(factory string, name Ptr) Ptr
	ElementFactoryFind func(name string) Ptr
	ParseLaunch        func(desc string, err *Ptr) Ptr
	ElementSetState    func(e Ptr, state int32) int32
	ElementGetState    func(e Ptr, state, pending *int32, timeout uint64) int32
	QueryPosition      func(e Ptr, format int32, cur *int64) bool
	QueryDuration      func(e Ptr, format int32, dur *int64) bool
	SeekSimple         func(e Ptr, format, flags int32, pos int64) bool
	Seek               func(e Ptr, rate float64, format, flags, startType int32, start int64, stopType int32, stop int64) bool
	ElementGetBus      func(e Ptr) Ptr
	BusAddSignalWatch  func(bus Ptr)
	BusRemoveSignalWat func(bus Ptr)
	ObjectUnref        func(o Ptr)
	SendEvent          func(e, ev Ptr) bool
	EventNewEOS        func() Ptr
	MessageGetStruct   func(m Ptr) Ptr
	StructureGetName   func(s Ptr) Ptr
	StructureToString  func(s Ptr) Ptr
	MessageParseError  func(m Ptr, err *Ptr, debug *Ptr)
	BinGetByName       func(bin Ptr, name string) Ptr
}

// GStreamer's constants.
const (
	GstStateNull    = 1
	GstStateReady   = 2
	GstStatePaused  = 3
	GstStatePlaying = 4

	GstFormatTime = 3

	GstSeekFlagFlush    = 1 << 0
	GstSeekFlagAccurate = 1 << 1
	GstSeekTypeNone     = 0
	GstSeekTypeSet      = 1

	GstStateChangeFailure = 0
)

var modGstOnce sync.Once
var modGstErr error

// LoadGst opens and initializes GStreamer, once.
func LoadGst() error {
	modGstOnce.Do(func() {
		lib, err := modOpenLib("libgstreamer-1.0.so.0", "libgstreamer-1.0.so")
		if err != nil {
			modGstErr = fmt.Errorf("GStreamer isn't installed (%v)", err)
			return
		}
		g := &Gst
		if err := modBindLib(lib, []binding{
			{&g.InitCheck, "gst_init_check"},
			{&g.ElementFactoryMake, "gst_element_factory_make"},
			{&g.ElementFactoryFind, "gst_element_factory_find"},
			{&g.ParseLaunch, "gst_parse_launch"},
			{&g.ElementSetState, "gst_element_set_state"},
			{&g.ElementGetState, "gst_element_get_state"},
			{&g.QueryPosition, "gst_element_query_position"},
			{&g.QueryDuration, "gst_element_query_duration"},
			{&g.SeekSimple, "gst_element_seek_simple"},
			{&g.Seek, "gst_element_seek"},
			{&g.ElementGetBus, "gst_element_get_bus"},
			{&g.BusAddSignalWatch, "gst_bus_add_signal_watch"},
			{&g.BusRemoveSignalWat, "gst_bus_remove_signal_watch"},
			{&g.ObjectUnref, "gst_object_unref"},
			{&g.SendEvent, "gst_element_send_event"},
			{&g.EventNewEOS, "gst_event_new_eos"},
			{&g.MessageGetStruct, "gst_message_get_structure"},
			{&g.StructureGetName, "gst_structure_get_name"},
			{&g.StructureToString, "gst_structure_to_string"},
			{&g.MessageParseError, "gst_message_parse_error"},
			{&g.BinGetByName, "gst_bin_get_by_name"},
		}); err != nil {
			modGstErr = err
			return
		}
		var gerr Ptr
		if !g.InitCheck(0, 0, &gerr) {
			modGstErr = fmt.Errorf("GStreamer didn't start: %v", GError(gerr))
		}
	})
	return modGstErr
}

// GstHas reports whether GStreamer has an element (its plugin is
// installed).
func GstHas(factory string) bool {
	f := Gst.ElementFactoryFind(factory)
	if f == 0 {
		return false
	}
	Gst.ObjectUnref(f)
	return true
}

// GstMessageError is an error message's text.
func GstMessageError(msg Ptr) error {
	var gerr, debug Ptr
	Gst.MessageParseError(msg, &gerr, &debug)
	if debug != 0 {
		Free(debug)
	}
	if err := GError(gerr); err != nil {
		return err
	}
	return errors.New("GStreamer: an error")
}

// GstMessageStructure is an element message's structure: its name and
// its text form ("level, rms=(double){ -20.1 }, ...").
func GstMessageStructure(msg Ptr) (name, text string) {
	s := Gst.MessageGetStruct(msg)
	if s == 0 {
		return "", ""
	}
	return GoString(Gst.StructureGetName(s)), GoStringFree(Gst.StructureToString(s))
}

func modOpenLib(names ...string) (uintptr, error) {
	var err error
	for _, name := range names {
		var lib uintptr
		if lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			return lib, nil
		}
	}
	return 0, err
}

func modBindLib(lib uintptr, bs []binding) error {
	for _, b := range bs {
		p, err := purego.Dlsym(lib, b.name)
		if err != nil {
			return fmt.Errorf("%s is missing", b.name)
		}
		purego.RegisterFunc(b.p, p)
	}
	return nil
}

// geocode-glib.

var geocodeAPI struct {
	once         sync.Once
	err          error
	locationNew  func(lat, lon, acc float64) Ptr
	reverseNew   func(loc Ptr) Ptr
	resolveAsync func(r, cancellable, cb, data Ptr)
	resolveFin   func(r, res Ptr, err *Ptr) Ptr
	name, area   func(p Ptr) Ptr
	town, state  func(p Ptr) Ptr
	country      func(p Ptr) Ptr
}

// GeocodePlace is a place's names, as geocode-glib (Nominatim) has them.
type GeocodePlace struct{ Name, Area, Town, State, Country string }

// GeocodeReverse names the place at a position, asynchronously: done
// runs on the main thread.
func GeocodeReverse(lat, lon float64, done func(GeocodePlace, error)) {
	g := &geocodeAPI
	g.once.Do(func() {
		lib, err := modOpenLib("libgeocode-glib-2.so.0", "libgeocode-glib.so.0")
		if err != nil {
			g.err = errors.New("location: no geocoder (geocode-glib isn't installed)")
			return
		}
		g.err = modBindLib(lib, []binding{
			{&g.locationNew, "geocode_location_new"},
			{&g.reverseNew, "geocode_reverse_new_for_location"},
			{&g.resolveAsync, "geocode_reverse_resolve_async"},
			{&g.resolveFin, "geocode_reverse_resolve_finish"},
			{&g.name, "geocode_place_get_name"},
			{&g.area, "geocode_place_get_area"},
			{&g.town, "geocode_place_get_town"},
			{&g.state, "geocode_place_get_state"},
			{&g.country, "geocode_place_get_country"},
		})
	})
	if g.err != nil {
		done(GeocodePlace{}, g.err)
		return
	}
	loc := g.locationNew(lat, lon, -1) // GEOCODE_LOCATION_ACCURACY_UNKNOWN
	rev := g.reverseNew(loc)
	Async(func(a AsyncCall) {
		g.resolveAsync(rev, 0, a.Callback, a.Data)
	}, func(_, res Ptr) {
		var gerr Ptr
		p := g.resolveFin(rev, res, &gerr)
		ObjectUnref(rev)
		ObjectUnref(loc)
		if p == 0 {
			err := GError(gerr)
			if err == nil {
				err = errors.New("no name for this place")
			}
			done(GeocodePlace{}, err)
			return
		}
		pl := GeocodePlace{Name: GoString(g.name(p)), Area: GoString(g.area(p)), Town: GoString(g.town(p)), State: GoString(g.state(p)), Country: GoString(g.country(p))}
		ObjectUnref(p)
		done(pl, nil)
	})
}
