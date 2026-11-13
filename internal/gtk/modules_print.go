//go:build linux && !android

package gtk

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// Printing: GTK's print settings, page setups and dialog, and WebKit's
// print operation (asuka/sdk/print's markup and web views).
var Print struct {
	SettingsNew        func() Ptr
	SettingsSet        func(s Ptr, key, value string)
	SettingsSetPrinter func(s Ptr, printer string)
	PageSetupNew       func() Ptr
	SetOrientation     func(p Ptr, o int32)
	SetPaperSize       func(p, size Ptr)
	SetTopMargin       func(p Ptr, m float64, unit int32)
	SetBottomMargin    func(p Ptr, m float64, unit int32)
	SetLeftMargin      func(p Ptr, m float64, unit int32)
	SetRightMargin     func(p Ptr, m float64, unit int32)
	PaperSizeNewCustom func(name, display string, w, h float64, unit int32) Ptr
	PaperSizeFree      func(p Ptr)
	UnixDialogNew      func(title string, parent Ptr) Ptr
	UnixDialogPrinter  func(d Ptr) Ptr
	UnixDialogSettings func(d Ptr) Ptr
	UnixDialogSetup    func(d Ptr) Ptr
	UnixDialogSetCaps  func(d Ptr, caps int32)
	JobNew             func(title string, printer, settings, setup Ptr) Ptr
	JobSetSourceFile   func(j Ptr, path string, err *Ptr) bool
	JobSend            func(j, cb, data, destroy Ptr)

	// WebKit's, bound by LoadWebKitPrint.
	OperationNew         func(view Ptr) Ptr
	OperationRunDialog   func(op, parent Ptr) int32
	OperationPrint       func(op Ptr)
	OperationSetSettings func(op, s Ptr)
	OperationSetSetup    func(op, p Ptr)
}

const (
	UnitPoints = 1 // GtkUnit

	ResponseOK     = -5 // GtkResponseType
	ResponseApply  = -10
	ResponseCancel = -6

	PrintPreviewCaps = 1 << 6 // GTK_PRINT_CAPABILITY_PREVIEW
)

func init() {
	p := &Print
	bindings = append(bindings,
		binding{&p.SettingsNew, "gtk_print_settings_new"},
		binding{&p.SettingsSet, "gtk_print_settings_set"},
		binding{&p.SettingsSetPrinter, "gtk_print_settings_set_printer"},
		binding{&p.PageSetupNew, "gtk_page_setup_new"},
		binding{&p.SetOrientation, "gtk_page_setup_set_orientation"},
		binding{&p.SetPaperSize, "gtk_page_setup_set_paper_size"},
		binding{&p.SetTopMargin, "gtk_page_setup_set_top_margin"},
		binding{&p.SetBottomMargin, "gtk_page_setup_set_bottom_margin"},
		binding{&p.SetLeftMargin, "gtk_page_setup_set_left_margin"},
		binding{&p.SetRightMargin, "gtk_page_setup_set_right_margin"},
		binding{&p.PaperSizeNewCustom, "gtk_paper_size_new_custom"},
		binding{&p.PaperSizeFree, "gtk_paper_size_free"},
		binding{&p.UnixDialogNew, "gtk_print_unix_dialog_new"},
		binding{&p.UnixDialogPrinter, "gtk_print_unix_dialog_get_selected_printer"},
		binding{&p.UnixDialogSettings, "gtk_print_unix_dialog_get_settings"},
		binding{&p.UnixDialogSetup, "gtk_print_unix_dialog_get_page_setup"},
		binding{&p.UnixDialogSetCaps, "gtk_print_unix_dialog_set_manual_capabilities"},
		binding{&p.JobNew, "gtk_print_job_new"},
		binding{&p.JobSetSourceFile, "gtk_print_job_set_source_file"},
		binding{&p.JobSend, "gtk_print_job_send"},
	)
}

var webKitPrintErr error
var webKitPrintBound bool

// LoadWebKitPrint binds WebKit's printing, once WebKit is loaded.
func LoadWebKitPrint() error {
	if err := LoadWebKit(); err != nil {
		return err
	}
	if webKitPrintBound {
		return webKitPrintErr
	}
	webKitPrintBound = true
	p := &Print
	for _, b := range []binding{
		{&p.OperationNew, "webkit_print_operation_new"},
		{&p.OperationRunDialog, "webkit_print_operation_run_dialog"},
		{&p.OperationPrint, "webkit_print_operation_print"},
		{&p.OperationSetSettings, "webkit_print_operation_set_print_settings"},
		{&p.OperationSetSetup, "webkit_print_operation_set_page_setup"},
	} {
		s, err := purego.Dlsym(libWebKit, b.name)
		if err != nil {
			webKitPrintErr = fmt.Errorf("WebKitGTK has no %s", b.name)
			return webKitPrintErr
		}
		purego.RegisterFunc(b.p, s)
	}
	return nil
}

var cbPrintJob uintptr

// PrintJobSend sends a job to its printer; done gets its end (a
// GtkPrintJobCompleteFunc), on the main thread.
func PrintJobSend(job Ptr, done func(err error)) {
	if cbPrintJob == 0 {
		cbPrintJob = purego.NewCallback(func(job, data, gerr uintptr) uintptr {
			f, _ := handlers[data].(func(error))
			delete(handlers, data)
			if f != nil {
				var err error
				if gerr != 0 {
					err = fmt.Errorf("%s", GoString(*At[Ptr](gerr + 8)))
				}
				f(err)
			}
			return 0
		})
	}
	id := register(done)
	Print.JobSend(job, cbPrintJob, id, 0)
}
