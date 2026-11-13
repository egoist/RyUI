//go:build linux && !android

package gtk

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// What the host's device modules use beyond GTK and the GLib stack:
// GStreamer's appsink and buffers (the camera's frames, a video's frame),
// GdkPixbuf's loaders (asuka/sdk/imageedit, asuka/sdk/scan), ZBar (codes),
// Tesseract (text), libshumate (maps), D-Bus replies carrying file
// descriptors (the camera portal's PipeWire remote), GIO's file
// monitors, and WebKit's navigation decisions and ephemeral sessions.
// The extra libraries are opened the first time they're used; a system
// without one answers an error.

// GIO, GTK and libadwaita functions, bound at Load.
var Dev struct {
	FileMonitorDirectory      func(f Ptr, flags int32, cancellable Ptr, err *Ptr) Ptr
	FileMonitorCancel         func(m Ptr) bool
	AdwDialogSetDefaultWidget func(d, w Ptr)
	WidgetSetCanTarget        func(w Ptr, can bool)
	UnixFDListStealFDs        func(l Ptr, n *int32) Ptr
	TypeFromName              func(name string) GType
	AdwWindowTitleSetSubtitle func(t Ptr, subtitle string)

	// GdkPixbuf: GTK links it.
	PixbufNewFromFile      func(path string, err *Ptr) Ptr
	PixbufLoaderNew        func() Ptr
	PixbufLoaderWrite      func(l Ptr, buf *byte, n uintptr, err *Ptr) bool
	PixbufLoaderClose      func(l Ptr, err *Ptr) bool
	PixbufLoaderGetPixbuf  func(l Ptr) Ptr
	PixbufApplyOrientation func(p Ptr) Ptr
	PixbufGetWidth         func(p Ptr) int32
	PixbufGetHeight        func(p Ptr) int32
	PixbufGetRowstride     func(p Ptr) int32
	PixbufGetNChannels     func(p Ptr) int32
	PixbufGetHasAlpha      func(p Ptr) bool
	PixbufGetBitsPerSample func(p Ptr) int32
	PixbufReadPixels       func(p Ptr) Ptr
	PixbufGetFileInfo      func(path string, w, h *int32) Ptr
	PixbufGetOption        func(p Ptr, key string) Ptr
}

func init() {
	d := &Dev
	bindings = append(bindings,
		binding{&d.FileMonitorDirectory, "g_file_monitor_directory"},
		binding{&d.FileMonitorCancel, "g_file_monitor_cancel"},
		binding{&d.AdwDialogSetDefaultWidget, "adw_dialog_set_default_widget"},
		binding{&d.WidgetSetCanTarget, "gtk_widget_set_can_target"},
		binding{&d.UnixFDListStealFDs, "g_unix_fd_list_steal_fds"},
		binding{&d.TypeFromName, "g_type_from_name"},
		binding{&d.AdwWindowTitleSetSubtitle, "adw_window_title_set_subtitle"},
		binding{&d.PixbufNewFromFile, "gdk_pixbuf_new_from_file"},
		binding{&d.PixbufLoaderNew, "gdk_pixbuf_loader_new"},
		binding{&d.PixbufLoaderWrite, "gdk_pixbuf_loader_write"},
		binding{&d.PixbufLoaderClose, "gdk_pixbuf_loader_close"},
		binding{&d.PixbufLoaderGetPixbuf, "gdk_pixbuf_loader_get_pixbuf"},
		binding{&d.PixbufApplyOrientation, "gdk_pixbuf_apply_embedded_orientation"},
		binding{&d.PixbufGetWidth, "gdk_pixbuf_get_width"},
		binding{&d.PixbufGetHeight, "gdk_pixbuf_get_height"},
		binding{&d.PixbufGetRowstride, "gdk_pixbuf_get_rowstride"},
		binding{&d.PixbufGetNChannels, "gdk_pixbuf_get_n_channels"},
		binding{&d.PixbufGetHasAlpha, "gdk_pixbuf_get_has_alpha"},
		binding{&d.PixbufGetBitsPerSample, "gdk_pixbuf_get_bits_per_sample"},
		binding{&d.PixbufReadPixels, "gdk_pixbuf_read_pixels"},
		binding{&d.PixbufGetFileInfo, "gdk_pixbuf_get_file_info"},
		binding{&d.PixbufGetOption, "gdk_pixbuf_get_option"},
	)
	raws = append(raws,
		struct {
			p    *uintptr
			name string
		}{&fnBytesNewRaw, "g_bytes_new"},
		struct {
			p    *uintptr
			name string
		}{&fnBytesUnrefRaw, "g_bytes_unref"},
		struct {
			p    *uintptr
			name string
		}{&fnMemoryTextureNewRaw, "gdk_memory_texture_new"},
		struct {
			p    *uintptr
			name string
		}{&fnPictureSetPaintableRaw, "gtk_picture_set_paintable"},
		struct {
			p    *uintptr
			name string
		}{&fnObjectUnrefRaw, "g_object_unref"},
	)
}

var fnBytesNewRaw, fnBytesUnrefRaw, fnMemoryTextureNewRaw, fnPictureSetPaintableRaw, fnObjectUnrefRaw uintptr

// Pixels is an image's pixels as GdkPixbuf decoded them: rows of RGB or
// RGBA, 8 bits a sample, not premultiplied.
type Pixels struct {
	W, H, Stride, Channels int
	Data                   []byte
}

// PixbufPixels copies a pixbuf's pixels (8-bit RGB or RGBA only).
func PixbufPixels(p Ptr) (Pixels, error) {
	d := &Dev
	if d.PixbufGetBitsPerSample(p) != 8 {
		return Pixels{}, errors.New("an image of more than 8 bits a sample")
	}
	px := Pixels{W: int(d.PixbufGetWidth(p)), H: int(d.PixbufGetHeight(p)), Stride: int(d.PixbufGetRowstride(p)), Channels: int(d.PixbufGetNChannels(p))}
	if px.W <= 0 || px.H <= 0 || (px.Channels != 3 && px.Channels != 4) {
		return Pixels{}, errors.New("an empty image")
	}
	// The last row is only as long as its pixels.
	n := px.Stride*(px.H-1) + px.W*px.Channels
	px.Data = GoBytes(d.PixbufReadPixels(p), n)
	return px, nil
}

// DecodePixbuf decodes an image file's or bytes' pixels with GdkPixbuf's
// loaders (JPEG, PNG, WebP, HEIF, TIFF... as installed), turned upright
// as its orientation says. Safe off the main thread.
func DecodePixbuf(path string, data []byte) (Pixels, error) {
	d := &Dev
	var pb Ptr
	var gerr Ptr
	if data == nil {
		pb = d.PixbufNewFromFile(path, &gerr)
		if pb == 0 {
			return Pixels{}, GError(gerr)
		}
	} else {
		if len(data) == 0 {
			return Pixels{}, errors.New("no data")
		}
		l := d.PixbufLoaderNew()
		ok := d.PixbufLoaderWrite(l, &data[0], uintptr(len(data)), &gerr)
		runtime.KeepAlive(data)
		var cerr Ptr
		closed := d.PixbufLoaderClose(l, &cerr)
		if !ok || !closed {
			if gerr == 0 {
				gerr = cerr
			} else if cerr != 0 {
				ErrorFree(cerr)
			}
			ObjectUnref(l)
			return Pixels{}, GError(gerr)
		}
		pb = d.PixbufLoaderGetPixbuf(l)
		if pb != 0 {
			ObjectRef(pb)
		}
		ObjectUnref(l)
		if pb == 0 {
			return Pixels{}, errors.New("not an image")
		}
	}
	defer ObjectUnref(pb)
	up := d.PixbufApplyOrientation(pb)
	if up == 0 {
		return Pixels{}, errors.New("not an image")
	}
	defer ObjectUnref(up)
	return PixbufPixels(up)
}

// PixbufFileInfo is an image file's format and size, without decoding
// it (0 size: not an image GdkPixbuf knows).
func PixbufFileInfo(path string) (w, h int) {
	var iw, ih int32
	if Dev.PixbufGetFileInfo(path, &iw, &ih) == 0 {
		return 0, 0
	}
	return int(iw), int(ih)
}

// A texture of RGBA pixels at p, stride bytes a row (copied), set on a
// picture: no reflection, for each of a camera's frames. Main thread.
func SetPictureRGBA(picture Ptr, p Ptr, w, h, stride int) {
	b := Call(fnBytesNewRaw, p, uintptr(stride*h))
	t := Call(fnMemoryTextureNewRaw, uintptr(w), uintptr(h), uintptr(MemoryR8G8B8A8), b, uintptr(stride))
	Call(fnBytesUnrefRaw, b)
	Call(fnPictureSetPaintableRaw, picture, t)
	Call(fnObjectUnrefRaw, t)
}

// D-Bus replies with file descriptors.

// DBusCallOutFDs is DBusCall for a method that answers file descriptors
// (the reply's handles index them): done gets them, the caller's to
// close.
func DBusCallOutFDs(conn Ptr, dest, path, iface, method string, params Ptr, timeoutMS int32, done func(reply Ptr, fds []int, err error)) {
	d, p, i, m := CString(dest), CString(path), CString(iface), CString(method)
	Async(func(a AsyncCall) {
		Call(FnConnectionCallWithFDs, conn, Addr(d), Addr(p), Addr(i), Addr(m), params, 0, 0, uintptr(timeoutMS), 0, 0, a.Callback, a.Data)
		runtime.KeepAlive(d)
		runtime.KeepAlive(p)
		runtime.KeepAlive(i)
		runtime.KeepAlive(m)
	}, func(_, res Ptr) {
		var gerr, list Ptr
		r := Sys.ConnectionCallFDFin(conn, &list, res, &gerr)
		var fds []int
		if list != 0 {
			var n int32
			arr := Dev.UnixFDListStealFDs(list, &n)
			for k := range int(n) {
				fds = append(fds, int(*At[int32](arr + uintptr(4*k))))
			}
			if arr != 0 {
				Free(arr)
			}
			ObjectUnref(list)
		}
		if r == 0 {
			done(0, fds, GError(gerr))
			return
		}
		done(r, fds, nil)
		Sys.VariantUnref(r)
	})
}

// GStreamer's appsink, samples and buffers.

// GstMapInfo is GStreamer's.
type GstMapInfo struct {
	Memory   Ptr
	Flags    uint32
	_        uint32
	Data     Ptr
	Size     uintptr
	MaxSize  uintptr
	User     [4]Ptr
	Reserved [4]Ptr
}

// Raw: called through Call, for each frame.
var (
	FnAppSinkTryPullSample, FnAppSinkTryPullPreroll uintptr
	FnSampleGetBuffer, FnSampleGetCaps              uintptr
	FnCapsGetStructure, FnStructureGetInt           uintptr
	FnBufferMap, FnBufferUnmap, FnMiniObjectUnref   uintptr
	FnStructureGetString                            uintptr
	FnBinGetByName, FnObjectUnrefGst                uintptr
)

var (
	appOnce sync.Once
	appErr  error
)

// LoadGstApp loads GStreamer (LoadGst) and its app library (appsink),
// once.
func LoadGstApp() error {
	appOnce.Do(func() {
		if appErr = LoadGst(); appErr != nil {
			return
		}
		core, err := modOpenLib("libgstreamer-1.0.so.0", "libgstreamer-1.0.so")
		if err != nil {
			appErr = err
			return
		}
		app, err := modOpenLib("libgstapp-1.0.so.0", "libgstapp-1.0.so")
		if err != nil {
			appErr = fmt.Errorf("GStreamer's app library isn't installed (%v)", err)
			return
		}
		for _, r := range []struct {
			lib  uintptr
			p    *uintptr
			name string
		}{
			{app, &FnAppSinkTryPullSample, "gst_app_sink_try_pull_sample"},
			{app, &FnAppSinkTryPullPreroll, "gst_app_sink_try_pull_preroll"},
			{core, &FnSampleGetBuffer, "gst_sample_get_buffer"},
			{core, &FnSampleGetCaps, "gst_sample_get_caps"},
			{core, &FnCapsGetStructure, "gst_caps_get_structure"},
			{core, &FnStructureGetInt, "gst_structure_get_int"},
			{core, &FnStructureGetString, "gst_structure_get_string"},
			{core, &FnBufferMap, "gst_buffer_map"},
			{core, &FnBufferUnmap, "gst_buffer_unmap"},
			{core, &FnMiniObjectUnref, "gst_mini_object_unref"},
			{core, &FnBinGetByName, "gst_bin_get_by_name"},
			{core, &FnObjectUnrefGst, "gst_object_unref"},
		} {
			p, err := purego.Dlsym(r.lib, r.name)
			if err != nil {
				appErr = fmt.Errorf("%s is missing", r.name)
				return
			}
			*r.p = p
		}
	})
	return appErr
}

var (
	cWidth  = CString("width")
	cHeight = CString("height")
)

// GstSampleFrame calls f with a sample's RGBA frame: its pixels (valid
// during f), its size and its stride; it reports whether the sample had
// one. The sample is the caller's.
func GstSampleFrame(sample Ptr, f func(data Ptr, w, h, stride int)) bool {
	buf := Call(FnSampleGetBuffer, sample)
	caps := Call(FnSampleGetCaps, sample)
	if buf == 0 || caps == 0 {
		return false
	}
	st := Call(FnCapsGetStructure, caps, 0)
	var w, h int32
	if st == 0 || Call(FnStructureGetInt, st, Addr(cWidth), Addr(&w)) == 0 || Call(FnStructureGetInt, st, Addr(cHeight), Addr(&h)) == 0 || w <= 0 || h <= 0 {
		return false
	}
	var info GstMapInfo
	if Call(FnBufferMap, buf, Addr(&info), 1) == 0 { // GST_MAP_READ
		return false
	}
	// RGBA rows are 4-byte aligned: GStreamer's stride is the width's.
	stride := int(w) * 4
	ok := int(info.Size) >= stride*int(h)
	if ok {
		f(info.Data, int(w), int(h), stride)
	}
	Call(FnBufferUnmap, buf, Addr(&info))
	return ok
}

// GstStructureString is a structure's string field ("" when it has none).
func GstStructureString(st Ptr, field string) string {
	c := CString(field)
	s := GoString(Call(FnStructureGetString, st, Addr(c)))
	runtime.KeepAlive(c)
	return s
}

// GstMessageStructurePtr is a message's structure (the message's).
func GstMessageStructurePtr(msg Ptr) Ptr { return Gst.MessageGetStruct(msg) }

// ZBar.

// ZBar is libzbar's, bound by LoadZBar.
var ZBar struct {
	ScannerCreate    func() Ptr
	ScannerDestroy   func(s Ptr)
	ScannerSetConfig func(s Ptr, sym, cfg, val int32) int32
	ImageCreate      func() Ptr
	ImageDestroy     func(i Ptr)
	ImageSetFormat   func(i Ptr, fourcc uint32)
	ImageSetSize     func(i Ptr, w, h uint32)
	ImageSetData     func(i Ptr, data *byte, n uintptr, cleanup Ptr)
	ScanImage        func(s, i Ptr) int32
	FirstSymbol      func(i Ptr) Ptr
	SymbolNext       func(s Ptr) Ptr
	SymbolType       func(s Ptr) int32
	SymbolName       func(t int32) Ptr
	SymbolData       func(s Ptr) Ptr
	SymbolDataLength func(s Ptr) uint32
	SymbolLocSize    func(s Ptr) uint32
	SymbolLocX       func(s Ptr, i uint32) int32
	SymbolLocY       func(s Ptr, i uint32) int32
}

var (
	zbarOnce sync.Once
	zbarErr  error
)

// LoadZBar opens libzbar, once.
func LoadZBar() error {
	zbarOnce.Do(func() {
		lib, err := modOpenLib("libzbar.so.0", "libzbar.so")
		if err != nil {
			zbarErr = fmt.Errorf("ZBar isn't installed (libzbar0): %v", err)
			return
		}
		z := &ZBar
		zbarErr = modBindLib(lib, []binding{
			{&z.ScannerCreate, "zbar_image_scanner_create"},
			{&z.ScannerDestroy, "zbar_image_scanner_destroy"},
			{&z.ScannerSetConfig, "zbar_image_scanner_set_config"},
			{&z.ImageCreate, "zbar_image_create"},
			{&z.ImageDestroy, "zbar_image_destroy"},
			{&z.ImageSetFormat, "zbar_image_set_format"},
			{&z.ImageSetSize, "zbar_image_set_size"},
			{&z.ImageSetData, "zbar_image_set_data"},
			{&z.ScanImage, "zbar_scan_image"},
			{&z.FirstSymbol, "zbar_image_first_symbol"},
			{&z.SymbolNext, "zbar_symbol_next"},
			{&z.SymbolType, "zbar_symbol_get_type"},
			{&z.SymbolName, "zbar_get_symbol_name"},
			{&z.SymbolData, "zbar_symbol_get_data"},
			{&z.SymbolDataLength, "zbar_symbol_get_data_length"},
			{&z.SymbolLocSize, "zbar_symbol_get_loc_size"},
			{&z.SymbolLocX, "zbar_symbol_get_loc_x"},
			{&z.SymbolLocY, "zbar_symbol_get_loc_y"},
		})
	})
	return zbarErr
}

// ZBarSymbol is a code ZBar read: its type's name ("QR-Code", "EAN-13"),
// its data and its outline's bounds, in pixels.
type ZBarSymbol struct {
	Type           string
	Data           string
	X0, Y0, X1, Y1 int
}

// ZBarScan reads the codes in a grayscale image (a byte a pixel, w×h).
// Safe off the main thread.
func ZBarScan(gray []byte, w, h int) []ZBarSymbol {
	z := &ZBar
	s := z.ScannerCreate()
	defer z.ScannerDestroy(s)
	const cfgEnable = 0
	z.ScannerSetConfig(s, 0, cfgEnable, 1)  // every symbology...
	z.ScannerSetConfig(s, 10, cfgEnable, 0) // ...but ISBN-10's
	z.ScannerSetConfig(s, 14, cfgEnable, 0) // and ISBN-13's: EAN-13s
	img := z.ImageCreate()
	defer z.ImageDestroy(img)
	z.ImageSetFormat(img, 'Y'|'8'<<8|'0'<<16|'0'<<24)
	z.ImageSetSize(img, uint32(w), uint32(h))
	z.ImageSetData(img, &gray[0], uintptr(len(gray)), 0)
	var out []ZBarSymbol
	if z.ScanImage(s, img) > 0 {
		for sym := z.FirstSymbol(img); sym != 0; sym = z.SymbolNext(sym) {
			r := ZBarSymbol{Type: GoString(z.SymbolName(z.SymbolType(sym))),
				Data: string(GoBytes(z.SymbolData(sym), int(z.SymbolDataLength(sym)))), X0: w, Y0: h}
			n := z.SymbolLocSize(sym)
			for i := range n {
				x, y := int(z.SymbolLocX(sym, i)), int(z.SymbolLocY(sym, i))
				r.X0, r.Y0, r.X1, r.Y1 = min(r.X0, x), min(r.Y0, y), max(r.X1, x), max(r.Y1, y)
			}
			if n == 0 {
				r.X0, r.Y0 = 0, 0
			}
			out = append(out, r)
		}
	}
	runtime.KeepAlive(gray)
	return out
}

// Tesseract.

// Tess is libtesseract's C API, bound by LoadTesseract.
var Tess struct {
	Create          func() Ptr
	Delete          func(a Ptr)
	End             func(a Ptr)
	Init3           func(a Ptr, datapath Ptr, lang string) int32
	SetImage        func(a Ptr, data *byte, w, h, bpp, bpl int32)
	Recognize       func(a, monitor Ptr) int32
	GetIterator     func(a Ptr) Ptr
	IterDelete      func(it Ptr)
	IterNext        func(it Ptr, level int32) bool
	IterText        func(it Ptr, level int32) Ptr
	IterPage        func(it Ptr) Ptr
	DeleteText      func(t Ptr)
	PageBox         func(pi Ptr, level int32, l, t, r, b *int32) bool
	PageAtBeginning func(pi Ptr, level int32) bool
}

var (
	tessOnce sync.Once
	tessErr  error
)

// LoadTesseract opens libtesseract, once.
func LoadTesseract() error {
	tessOnce.Do(func() {
		lib, err := modOpenLib("libtesseract.so.5", "libtesseract.so.4", "libtesseract.so")
		if err != nil {
			tessErr = fmt.Errorf("Tesseract isn't installed (libtesseract5): %v", err)
			return
		}
		t := &Tess
		tessErr = modBindLib(lib, []binding{
			{&t.Create, "TessBaseAPICreate"},
			{&t.Delete, "TessBaseAPIDelete"},
			{&t.End, "TessBaseAPIEnd"},
			{&t.Init3, "TessBaseAPIInit3"},
			{&t.SetImage, "TessBaseAPISetImage"},
			{&t.Recognize, "TessBaseAPIRecognize"},
			{&t.GetIterator, "TessBaseAPIGetIterator"},
			{&t.IterDelete, "TessResultIteratorDelete"},
			{&t.IterNext, "TessResultIteratorNext"},
			{&t.IterText, "TessResultIteratorGetUTF8Text"},
			{&t.IterPage, "TessResultIteratorGetPageIterator"},
			{&t.DeleteText, "TessDeleteText"},
			{&t.PageBox, "TessPageIteratorBoundingBox"},
			{&t.PageAtBeginning, "TessPageIteratorIsAtBeginningOf"},
		})
	})
	return tessErr
}

// TessWord is a word Tesseract read, with its line's (when it begins
// one) and their boxes in pixels (left, top, right, bottom).
type TessWord struct {
	Text    string
	Box     [4]int
	NewLine bool
	Line    string
	LineBox [4]int
}

// TessRead reads the text in an 8-bit grayscale image (w×h) in the
// languages ("eng+deu"). Safe off the main thread.
func TessRead(gray []byte, w, h int, langs string) ([]TessWord, error) {
	t := &Tess
	a := t.Create()
	defer t.Delete(a)
	if t.Init3(a, 0, langs) != 0 {
		return nil, fmt.Errorf("Tesseract has no data for %s", langs)
	}
	defer t.End(a)
	t.SetImage(a, &gray[0], int32(w), int32(h), 1, int32(w))
	if t.Recognize(a, 0) != 0 {
		runtime.KeepAlive(gray)
		return nil, errors.New("Tesseract failed")
	}
	runtime.KeepAlive(gray)
	it := t.GetIterator(a)
	if it == 0 {
		return nil, nil
	}
	defer t.IterDelete(it)
	const lineLevel, wordLevel = 2, 3
	pi := t.IterPage(it)
	text := func(level int32) string {
		p := t.IterText(it, level)
		s := GoString(p)
		if p != 0 {
			t.DeleteText(p)
		}
		return s
	}
	box := func(level int32) (b [4]int) {
		var l, tp, r, bt int32
		if t.PageBox(pi, level, &l, &tp, &r, &bt) {
			b = [4]int{int(l), int(tp), int(r), int(bt)}
		}
		return b
	}
	var out []TessWord
	for {
		wd := TessWord{Text: text(wordLevel), Box: box(wordLevel)}
		if len(out) == 0 || t.PageAtBeginning(pi, lineLevel) {
			wd.NewLine = true
			wd.Line = text(lineLevel)
			wd.LineBox = box(lineLevel)
		}
		if wd.Text != "" || wd.NewLine {
			out = append(out, wd)
		}
		if !t.IterNext(it, wordLevel) {
			break
		}
	}
	return out, nil
}

// libshumate.

// Shumate is libshumate's, bound by LoadShumate.
var Shumate struct {
	SimpleMapNew            func() Ptr
	SimpleMapSetMapSource   func(m, src Ptr)
	SimpleMapGetMap         func(m Ptr) Ptr
	SimpleMapGetViewport    func(m Ptr) Ptr
	SimpleMapAddOverlay     func(m, layer Ptr)
	SimpleMapSetShowZoom    func(m Ptr, show bool)
	MapCenterOn             func(m Ptr, lat, lon float64)
	ViewportSetZoom         func(v Ptr, z float64)
	ViewportGetZoom         func(v Ptr) float64
	ViewportWidgetToLoc     func(v, widget Ptr, x, y float64, lat, lon *float64)
	LocationGetLatitude     func(l Ptr) float64
	LocationGetLongitude    func(l Ptr) float64
	LocationSetLocation     func(l Ptr, lat, lon float64)
	RegistryNewWithDefaults func() Ptr
	RegistryGetByID         func(r Ptr, id string) Ptr
	MarkerLayerNew          func(v Ptr) Ptr
	MarkerLayerAdd          func(l, m Ptr)
	MarkerLayerRemoveAll    func(l Ptr)
	MarkerNew               func() Ptr
	MarkerSetChild          func(m, child Ptr)
	PointNew                func() Ptr
}

var (
	shumateOnce sync.Once
	shumateErr  error
)

// LoadShumate opens libshumate, once (main thread: it registers types).
func LoadShumate() error {
	shumateOnce.Do(func() {
		lib, err := modOpenLib("libshumate-1.0.so.1", "libshumate-1.0.so")
		if err != nil {
			shumateErr = fmt.Errorf("maps need libshumate (libshumate-1.0-1): %v", err)
			return
		}
		s := &Shumate
		shumateErr = modBindLib(lib, []binding{
			{&s.SimpleMapNew, "shumate_simple_map_new"},
			{&s.SimpleMapSetMapSource, "shumate_simple_map_set_map_source"},
			{&s.SimpleMapGetMap, "shumate_simple_map_get_map"},
			{&s.SimpleMapGetViewport, "shumate_simple_map_get_viewport"},
			{&s.SimpleMapAddOverlay, "shumate_simple_map_add_overlay_layer"},
			{&s.SimpleMapSetShowZoom, "shumate_simple_map_set_show_zoom_buttons"},
			{&s.MapCenterOn, "shumate_map_center_on"},
			{&s.ViewportSetZoom, "shumate_viewport_set_zoom_level"},
			{&s.ViewportGetZoom, "shumate_viewport_get_zoom_level"},
			{&s.ViewportWidgetToLoc, "shumate_viewport_widget_coords_to_location"},
			{&s.LocationGetLatitude, "shumate_location_get_latitude"},
			{&s.LocationGetLongitude, "shumate_location_get_longitude"},
			{&s.LocationSetLocation, "shumate_location_set_location"},
			{&s.RegistryNewWithDefaults, "shumate_map_source_registry_new_with_defaults"},
			{&s.RegistryGetByID, "shumate_map_source_registry_get_by_id"},
			{&s.MarkerLayerNew, "shumate_marker_layer_new"},
			{&s.MarkerLayerAdd, "shumate_marker_layer_add_marker"},
			{&s.MarkerLayerRemoveAll, "shumate_marker_layer_remove_all"},
			{&s.MarkerNew, "shumate_marker_new"},
			{&s.MarkerSetChild, "shumate_marker_set_child"},
			{&s.PointNew, "shumate_point_new"},
		})
	})
	return shumateErr
}

// WebKit's navigation decisions and sessions (after LoadWebKit).

var webAuthAPI struct {
	once                 sync.Once
	navigationAction     func(d Ptr) Ptr
	networkSessionNewEph func() Ptr
	webViewType          func() GType
}

// WebKitNavigationURI is a navigation (or new window) policy decision's
// request's URI.
func WebKitNavigationURI(decision Ptr) string {
	a := &webAuthAPI
	a.once.Do(webAuthBind)
	if a.navigationAction == nil {
		return ""
	}
	act := a.navigationAction(decision)
	if act == 0 {
		return ""
	}
	return GoString(WebKitURIRequestGetURI(WebKitNavigationActionGetRequest(act)))
}

func webAuthBind() {
	a := &webAuthAPI
	if p := sym(libWebKit, "webkit_navigation_policy_decision_get_navigation_action"); p != 0 {
		purego.RegisterFunc(&a.navigationAction, p)
	}
	if p := sym(libWebKit, "webkit_network_session_new_ephemeral"); p != 0 {
		purego.RegisterFunc(&a.networkSessionNewEph, p)
	}
	if p := sym(libWebKit, "webkit_web_view_get_type"); p != 0 {
		purego.RegisterFunc(&a.webViewType, p)
	}
}

// WebKitWebViewNewEphemeral is a web view (floating) whose cookies and
// storage are its own, gone when it is.
func WebKitWebViewNewEphemeral() Ptr {
	a := &webAuthAPI
	a.once.Do(webAuthBind)
	if a.networkSessionNewEph == nil || a.webViewType == nil {
		return WebKitWebViewNew()
	}
	s := a.networkSessionNewEph()
	var v Value
	ValueInit(&v, *At[GType](*At[Ptr](s)))
	Sys.ValueSetObject(&v, s)
	name := CString("network-session")
	names := [1]*byte{name}
	view := Call(FnObjectNew, uintptr(a.webViewType()), 1, uintptr(unsafe.Pointer(&names[0])), Addr(&v))
	runtime.KeepAlive(name)
	ValueUnset(&v)
	ObjectUnref(s)
	return view
}
