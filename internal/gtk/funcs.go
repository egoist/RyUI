//go:build linux && !android

package gtk

// Raw function pointers, called through Call (no reflection): the hot
// paths and the calls with only integer and pointer arguments that the
// package wraps itself.
var (
	FnSignalConnectData, FnSignalHandlerDisconnect            uintptr
	FnIdleAddFull, FnTimeoutAddFull, FnSourceRemove           uintptr
	FnTypeQuery, FnTypeClassPeekParent                        uintptr
	FnTypeRegisterStaticSimple, FnObjectNew                   uintptr
	FnWidgetAddTickCallback                                   uintptr
	FnWidgetAllocate, FnWidgetFirstChild, FnWidgetNextSibling uintptr
	FnWidgetSnapshotChild, FnWidgetQueueAllocate              uintptr
	FnWidgetQueueDraw, FnWidgetGetWidth, FnWidgetGetHeight    uintptr
	FnWidgetMeasure                                           uintptr
	FnSnapshotAppendColor, FnSnapshotPushRoundedClip          uintptr
	FnSnapshotPushClip, FnSnapshotPop, FnSnapshotAppendBorder uintptr
	FnSnapshotAppendLinearGradient, FnSnapshotSave            uintptr
	FnSnapshotRestore, FnSnapshotTranslate                    uintptr
	FnSnapshotAppendFill, FnSnapshotAppendStroke              uintptr
	FnSnapshotAppendLayout, FnSnapshotPushFill                uintptr
	FnTransformTranslate                                      uintptr
	FnEditableGetText                                         uintptr
)

var raws = []struct {
	p    *uintptr
	name string
}{
	{&FnSignalConnectData, "g_signal_connect_data"},
	{&FnSignalHandlerDisconnect, "g_signal_handler_disconnect"},
	{&FnIdleAddFull, "g_idle_add_full"},
	{&FnTimeoutAddFull, "g_timeout_add_full"},
	{&FnSourceRemove, "g_source_remove"},
	{&FnTypeQuery, "g_type_query"},
	{&FnTypeClassPeekParent, "g_type_class_peek_parent"},
	{&FnTypeRegisterStaticSimple, "g_type_register_static_simple"},
	{&FnObjectNew, "g_object_new_with_properties"},
	{&FnWidgetAddTickCallback, "gtk_widget_add_tick_callback"},
	{&FnWidgetAllocate, "gtk_widget_allocate"},
	{&FnWidgetFirstChild, "gtk_widget_get_first_child"},
	{&FnWidgetNextSibling, "gtk_widget_get_next_sibling"},
	{&FnWidgetSnapshotChild, "gtk_widget_snapshot_child"},
	{&FnWidgetQueueAllocate, "gtk_widget_queue_allocate"},
	{&FnWidgetQueueDraw, "gtk_widget_queue_draw"},
	{&FnWidgetGetWidth, "gtk_widget_get_width"},
	{&FnWidgetGetHeight, "gtk_widget_get_height"},
	{&FnWidgetMeasure, "gtk_widget_measure"},
	{&FnSnapshotAppendColor, "gtk_snapshot_append_color"},
	{&FnSnapshotPushRoundedClip, "gtk_snapshot_push_rounded_clip"},
	{&FnSnapshotPushClip, "gtk_snapshot_push_clip"},
	{&FnSnapshotPop, "gtk_snapshot_pop"},
	{&FnSnapshotAppendBorder, "gtk_snapshot_append_border"},
	{&FnSnapshotAppendLinearGradient, "gtk_snapshot_append_linear_gradient"},
	{&FnSnapshotSave, "gtk_snapshot_save"},
	{&FnSnapshotRestore, "gtk_snapshot_restore"},
	{&FnSnapshotTranslate, "gtk_snapshot_translate"},
	{&FnSnapshotAppendFill, "gtk_snapshot_append_fill"},
	{&FnSnapshotAppendStroke, "gtk_snapshot_append_stroke"},
	{&FnSnapshotAppendLayout, "gtk_snapshot_append_layout"},
	{&FnSnapshotPushFill, "gtk_snapshot_push_fill"},
	{&FnTransformTranslate, "gsk_transform_translate"},
	{&FnEditableGetText, "gtk_editable_get_text"},
	{&FnTypeClassRef, "g_type_class_ref"},
	{&FnEnumGetValueByNick, "g_enum_get_value_by_nick"},
}

// Typed functions, bound by Load. Strings are passed as Go strings
// (purego copies them for the call).
var (
	// GLib, GObject.
	Free                     func(p Ptr)
	MainContextDefault       func() Ptr
	MainContextIsOwner       func(ctx Ptr) bool
	MainContextIteration     func(ctx Ptr, mayBlock bool) bool
	ObjectRef                func(o Ptr) Ptr
	ObjectRefSink            func(o Ptr) Ptr
	ObjectUnref              func(o Ptr)
	ObjectGetProperty        func(o Ptr, name string, v *Value)
	ObjectSetProperty        func(o Ptr, name string, v *Value)
	ValueInit                func(v *Value, t GType) Ptr
	ValueUnset               func(v *Value)
	ValueGetBoolean          func(v *Value) bool
	ValueSetBoolean          func(v *Value, b bool)
	ValueGetInt              func(v *Value) int32
	ValueGetDouble           func(v *Value) float64
	ValueGetString           func(v *Value) Ptr
	ValueSetString           func(v *Value, s string)
	ValueGetObject           func(v *Value) Ptr
	ValueGetBoxed            func(v *Value) Ptr
	TypeFromName             func(name string) GType
	TypeCheckInstanceIsA     func(o Ptr, t GType) bool
	SignalStopEmissionByName func(o Ptr, signal string)
	BytesNew                 func(data *byte, size uintptr) Ptr
	BytesUnref               func(b Ptr)
	BytesGetData             func(b Ptr, size *uintptr) Ptr
	ErrorFree                func(e Ptr)
	GetUserConfigDir         func() Ptr
	GetUserDataDir           func() Ptr

	// GSettings.
	SettingsSchemaSourceGetDefault func() Ptr
	SettingsSchemaSourceLookup     func(src Ptr, id string, recursive bool) Ptr
	SettingsSchemaHasKey           func(schema Ptr, key string) bool
	SettingsSchemaUnref            func(schema Ptr)
	SettingsNew                    func(id string) Ptr
	SettingsGetDouble              func(s Ptr, key string) float64
	SettingsGetString              func(s Ptr, key string) Ptr
	SettingsGetBoolean             func(s Ptr, key string) bool
	SettingsGetInt                 func(s Ptr, key string) int32
	SettingsGetEnum                func(s Ptr, key string) int32

	// GApplication.
	AdwApplicationNew    func(id string, flags uint32) Ptr
	ApplicationRun       func(app Ptr, argc int32, argv Ptr) int32
	ApplicationQuit      func(app Ptr)
	ApplicationHold      func(app Ptr)
	ApplicationRelease   func(app Ptr)
	ApplicationIDIsValid func(id string) bool
	ApplicationRegister  func(app Ptr, cancellable Ptr, err *Ptr) bool
	ApplicationActivate  func(app Ptr)

	// GDK.
	DisplayGetDefault      func() Ptr
	DisplayGetClipboard    func(d Ptr) Ptr
	ClipboardSetText       func(c Ptr, text string)
	SurfaceGetScale        func(s Ptr) float64
	SurfaceGetScaleFactor  func(s Ptr) int32
	TextureNewFromBytes    func(b Ptr, err *Ptr) Ptr
	TextureNewFromFilename func(path string, err *Ptr) Ptr
	TextureGetWidth        func(t Ptr) int32
	TextureGetHeight       func(t Ptr) int32
	TextureSaveToPNG       func(t Ptr, path string) bool
	MemoryTextureNew       func(w, h int32, format int32, bytes Ptr, stride uintptr) Ptr
	CursorNewFromName      func(name string, fallback Ptr) Ptr

	// GSK.
	TransformRotate           func(t Ptr, degrees float32) Ptr
	TransformScale            func(t Ptr, x, y float32) Ptr
	TransformUnref            func(t Ptr)
	PathParse                 func(s string) Ptr
	PathUnref                 func(p Ptr)
	PathBuilderNew            func() Ptr
	PathBuilderMoveTo         func(b Ptr, x, y float32)
	PathBuilderLineTo         func(b Ptr, x, y float32)
	PathBuilderQuadTo         func(b Ptr, x1, y1, x2, y2 float32)
	PathBuilderCubicTo        func(b Ptr, x1, y1, x2, y2, x3, y3 float32)
	PathBuilderClose          func(b Ptr)
	PathBuilderAddRect        func(b Ptr, r *Rect)
	PathBuilderAddRoundedRect func(b Ptr, r *RoundedRect)
	PathBuilderAddCircle      func(b Ptr, center *Point, radius float32)
	PathBuilderArcTo          func(b Ptr, x1, y1, x2, y2 float32)
	PathBuilderToPath         func(b Ptr) Ptr
	PathBuilderFreeToPath     func(b Ptr) Ptr
	PathBuilderAddPath        func(b Ptr, p Ptr)
	PathBuilderAddSegment     func(b Ptr, p Ptr, start, end Ptr)
	PathMeasureNew            func(p Ptr) Ptr
	PathMeasureGetLength      func(m Ptr) float32
	PathMeasureGetPoint       func(m Ptr, distance float32, result Ptr) bool
	PathMeasureUnref          func(m Ptr)
	StrokeNew                 func(width float32) Ptr
	StrokeFree                func(s Ptr)
	StrokeSetLineCap          func(s Ptr, c int32)
	StrokeSetLineJoin         func(s Ptr, j int32)
	StrokeSetDash             func(s Ptr, dash *float32, n uintptr)
	StrokeSetDashOffset       func(s Ptr, off float32)
	RendererRenderTexture     func(r Ptr, node Ptr, viewport *Rect) Ptr
	RenderNodeUnref           func(n Ptr)

	// Snapshots (with floats).
	SnapshotNew                 func() Ptr
	SnapshotFreeToNode          func(s Ptr) Ptr
	SnapshotPushOpacity         func(s Ptr, opacity float64)
	SnapshotRotate              func(s Ptr, degrees float32)
	SnapshotScale               func(s Ptr, x, y float32)
	SnapshotAppendOutsetShadow  func(s Ptr, outline *RoundedRect, color *RGBA, dx, dy, spread, blur float32)
	SnapshotAppendInsetShadow   func(s Ptr, outline *RoundedRect, color *RGBA, dx, dy, spread, blur float32)
	SnapshotPushBlur            func(s Ptr, radius float64)
	SnapshotPushMask            func(s Ptr, mode int32)
	SnapshotAppendTexture       func(s Ptr, texture Ptr, bounds *Rect)
	SnapshotAppendScaledTexture func(s Ptr, texture Ptr, filter int32, bounds *Rect)
	PaintableSnapshot           func(p Ptr, s Ptr, w, h float64)

	// Widgets.
	WidgetSetParent                      func(w, parent Ptr)
	WidgetUnparent                       func(w Ptr)
	WidgetInsertBefore                   func(w, parent, next Ptr)
	WidgetInsertAfter                    func(w, parent, prev Ptr)
	WidgetGetParent                      func(w Ptr) Ptr
	WidgetGetLastChild                   func(w Ptr) Ptr
	WidgetGetPrevSibling                 func(w Ptr) Ptr
	WidgetSetVisible                     func(w Ptr, v bool)
	WidgetGetVisible                     func(w Ptr) bool
	WidgetGetMapped                      func(w Ptr) bool
	WidgetSetOpacity                     func(w Ptr, o float64)
	WidgetSetSensitive                   func(w Ptr, s bool)
	WidgetSetTooltipText                 func(w Ptr, s string)
	WidgetAddCSSClass                    func(w Ptr, c string)
	WidgetRemoveCSSClass                 func(w Ptr, c string)
	WidgetHasCSSClass                    func(w Ptr, c string) bool
	WidgetGetSensitive                   func(w Ptr) bool
	WidgetAddController                  func(w, c Ptr)
	WidgetRemoveController               func(w, c Ptr)
	WidgetGrabFocus                      func(w Ptr) bool
	WidgetHasFocus                       func(w Ptr) bool
	WidgetIsFocus                        func(w Ptr) bool
	WidgetHasVisibleFocus                func(w Ptr) bool
	WidgetSetFocusable                   func(w Ptr, f bool)
	WidgetSetFocusOnClick                func(w Ptr, f bool)
	WidgetSetCanTarget                   func(w Ptr, f bool)
	WidgetSetCanFocus                    func(w Ptr, f bool)
	WidgetSetOverflow                    func(w Ptr, o int32)
	WidgetGetScaleFactor                 func(w Ptr) int32
	WidgetGetNative                      func(w Ptr) Ptr
	WidgetGetRoot                        func(w Ptr) Ptr
	WidgetSetDirection                   func(w Ptr, d int32)
	WidgetQueueResize                    func(w Ptr)
	WidgetSetHExpand                     func(w Ptr, e bool)
	WidgetSetVExpand                     func(w Ptr, e bool)
	WidgetSetHAlign                      func(w Ptr, a int32)
	WidgetSetVAlign                      func(w Ptr, a int32)
	WidgetSetSizeRequest                 func(w Ptr, width, height int32)
	WidgetSetCursorFromName              func(w Ptr, name string)
	WidgetComputeBounds                  func(w, target Ptr, out *Rect) bool
	WidgetComputePoint                   func(w, target Ptr, p, out *Point) bool
	WidgetGetDisplay                     func(w Ptr) Ptr
	WidgetGetColor                       func(w Ptr, out *RGBA)
	WidgetGetStyleContext                func(w Ptr) Ptr
	StyleContextLookupColor              func(ctx Ptr, name string, out *RGBA) bool
	WidgetSetName                        func(w Ptr, name string)
	WidgetActivateAction                 func(w Ptr, name string, format Ptr) bool
	WidgetPick                           func(w Ptr, x, y float64, flags int32) Ptr
	WidgetContains                       func(w Ptr, x, y float64) bool
	WidgetGetFirstChild                  func(w Ptr) Ptr
	WidgetGetNextSibling                 func(w Ptr) Ptr
	WidgetInsertActionGroup              func(w Ptr, name string, group Ptr)
	WidgetTriggerTooltipQuery            func(w Ptr)
	WidgetMnemonicActivate               func(w Ptr, groupCycling bool) bool
	WidgetActivate                       func(w Ptr) bool
	WidgetGetAllocatedBaseline           func(w Ptr) int32
	WidgetPaintableNew                   func(w Ptr) Ptr
	NativeGetSurface                     func(n Ptr) Ptr
	NativeGetRenderer                    func(n Ptr) Ptr
	StyleContextAddProviderForDisplay    func(d, provider Ptr, priority uint32)
	StyleContextRemoveProviderForDisplay func(d, provider Ptr)
	CSSProviderNew                       func() Ptr
	CSSProviderLoadFromString            func(p Ptr, css string)
	SettingsGetDefault                   func() Ptr
	AccessibleUpdateProperty             func(a Ptr, n int32, props *int32, values *Value)
	AccessibleUpdateState                func(a Ptr, n int32, states *int32, values *Value)
	AccessibleResetProperty              func(a Ptr, p int32)
	AccessibleAnnounce                   func(a Ptr, message string, priority int32)

	// Windows.
	AdwApplicationWindowNew        func(app Ptr) Ptr
	AdwApplicationWindowSetContent func(w, content Ptr)
	AdwWindowNew                   func() Ptr
	AdwWindowSetContent            func(w, content Ptr)
	WindowSetTitle                 func(w Ptr, title string)
	WindowSetDefaultSize           func(w Ptr, width, height int32)
	WindowGetDefaultSize           func(w Ptr, width, height *int32)
	WindowSetResizable             func(w Ptr, r bool)
	WindowSetModal                 func(w Ptr, m bool)
	WindowSetTransientFor          func(w, parent Ptr)
	WindowSetHideOnClose           func(w Ptr, h bool)
	WindowSetTitlebar              func(w, titlebar Ptr)
	WindowPresent                  func(w Ptr)
	WindowClose                    func(w Ptr)
	WindowDestroy                  func(w Ptr)
	WindowIsActive                 func(w Ptr) bool
	WindowIsMaximized              func(w Ptr) bool
	WindowMaximize                 func(w Ptr)
	WindowGetFocus                 func(w Ptr) Ptr
	WindowSetFocus                 func(w, focus Ptr)
	WindowSetApplication           func(w, app Ptr)
	WindowSetDecorated             func(w Ptr, d bool)
	WindowListToplevels            func() Ptr
	ApplicationGetActiveWindow     func(app Ptr) Ptr
	WindowSetDeletable             func(w Ptr, d bool)
	WindowSetDestroyWithParent     func(w Ptr, d bool)
	WindowMinimize                 func(w Ptr)
	WindowUnminimize               func(w Ptr)
	WindowUnmaximize               func(w Ptr)
	WindowFullscreen               func(w Ptr)
	WindowUnfullscreen             func(w Ptr)
	WindowIsFullscreen             func(w Ptr) bool

	// Labels, Pango.
	LabelNew                  func(text string) Ptr
	LabelSetText              func(l Ptr, text string)
	LabelSetAttributes        func(l, attrs Ptr)
	LabelSetXAlign            func(l Ptr, x float32)
	LabelSetYAlign            func(l Ptr, y float32)
	LabelSetWrap              func(l Ptr, wrap bool)
	LabelSetWrapMode          func(l Ptr, mode int32)
	LabelSetNaturalWrapMode   func(l Ptr, mode int32)
	LabelSetLines             func(l Ptr, lines int32)
	LabelSetEllipsize         func(l Ptr, mode int32)
	LabelSetJustify           func(l Ptr, j int32)
	LabelSetSelectable        func(l Ptr, s bool)
	LabelSetUseMarkup         func(l Ptr, m bool)
	LabelSetMarkup            func(l Ptr, m string)
	LabelGetLayout            func(l Ptr) Ptr
	LabelSetMaxWidthChars     func(l Ptr, n int32)
	AttrListNew               func() Ptr
	AttrListInsert            func(list, attr Ptr)
	AttrListUnref             func(list Ptr)
	AttrSizeNewAbsolute       func(size int32) Ptr
	AttrWeightNew             func(weight int32) Ptr
	AttrStyleNew              func(style int32) Ptr
	AttrForegroundNew         func(r, g, b uint16) Ptr
	AttrForegroundAlphaNew    func(a uint16) Ptr
	AttrBackgroundNew         func(r, g, b uint16) Ptr
	AttrBackgroundAlphaNew    func(a uint16) Ptr
	AttrFamilyNew             func(family string) Ptr
	AttrUnderlineNew          func(u int32) Ptr
	AttrStrikethroughNew      func(s bool) Ptr
	AttrFontFeaturesNew       func(features string) Ptr
	AttrFallbackNew           func(f bool) Ptr
	AttrShapeNew              func(ink, logical *PangoRectangle) Ptr
	AttrLineHeightNewAbsolute func(h int32) Ptr
	FontMapGetDefault         func() Ptr // pango_cairo_font_map_get_default
	FontDescriptionFromString func(s string) Ptr
	FontDescriptionFree       func(d Ptr)
	LayoutNew                 func(ctx Ptr) Ptr
	LayoutSetText             func(l Ptr, text string, n int32)
	LayoutSetFontDescription  func(l, d Ptr)
	LayoutSetAttributes       func(l, attrs Ptr)
	LayoutSetWidth            func(l Ptr, w int32)
	LayoutSetAlignment        func(l Ptr, a int32)
	LayoutGetPixelSize        func(l Ptr, w, h *int32)
	LayoutGetBaseline         func(l Ptr) int32
	WidgetCreatePangoLayout   func(w Ptr, text string) Ptr
	WidgetGetPangoContext     func(w Ptr) Ptr
	FontMapLoadFont           func(fm, ctx, desc Ptr) Ptr

	// Buttons.
	ButtonNew                func() Ptr
	ButtonSetLabel           func(b Ptr, label string)
	ButtonSetChild           func(b, child Ptr)
	ButtonSetIconName        func(b Ptr, name string)
	ButtonSetHasFrame        func(b Ptr, f bool)
	ButtonSetUseUnderline    func(b Ptr, u bool)
	ButtonContentNew         func() Ptr
	ButtonContentSetLabel    func(c Ptr, label string)
	ButtonContentSetIconName func(c Ptr, name string)
	MenuButtonNew            func() Ptr
	MenuButtonSetPopover     func(b, p Ptr)
	MenuButtonSetMenuModel   func(b, m Ptr)
	MenuButtonSetLabel       func(b Ptr, l string)
	MenuButtonSetIconName    func(b Ptr, n string)
	MenuButtonPopup          func(b Ptr)
	ToggleButtonNew          func() Ptr
	ToggleButtonSetActive    func(b Ptr, a bool)
	ToggleButtonGetActive    func(b Ptr) bool
	ToggleButtonSetGroup     func(b, group Ptr)

	// Text input.
	EntryNew                      func() Ptr
	EntrySetPlaceholderText       func(e Ptr, s string)
	EntrySetInputPurpose          func(e Ptr, p int32)
	EntrySetInputHints            func(e Ptr, h int32)
	EntrySetHasFrame              func(e Ptr, f bool)
	EntrySetVisibility            func(e Ptr, v bool)
	EntrySetIconFromIconName      func(e Ptr, pos int32, name string)
	EntrySetAlignment             func(e Ptr, x float32)
	EntrySetAttributes            func(e, attrs Ptr)
	PasswordEntryNew              func() Ptr
	PasswordEntrySetShowPeekIcon  func(e Ptr, s bool)
	SearchEntryNew                func() Ptr
	SearchEntrySetPlaceholderText func(e Ptr, s string)
	SearchBarNew                  func() Ptr
	SearchBarSetChild             func(b, child Ptr)
	SearchBarConnectEntry         func(b, entry Ptr)
	SearchBarSetKeyCaptureWidget  func(b, w Ptr)
	SearchBarSetSearchMode        func(b Ptr, on bool)
	SearchBarGetSearchMode        func(b Ptr) bool
	EditableSetText               func(e Ptr, s string)
	EditableGetPosition           func(e Ptr) int32
	EditableSetPosition           func(e Ptr, pos int32)
	EditableSelectRegion          func(e Ptr, start, end int32)
	EditableGetSelectionBounds    func(e Ptr, start, end *int32) bool
	EditableSetEditable           func(e Ptr, ed bool)
	EditableInsertText            func(e Ptr, text string, n int32, pos *int32)
	EditableDeleteSelection       func(e Ptr)
	EditableGetDelegate           func(e Ptr) Ptr
	EditableSetAlignment          func(e Ptr, x float32)
	TextViewNew                   func() Ptr
	TextViewGetBuffer             func(v Ptr) Ptr
	TextViewSetWrapMode           func(v Ptr, m int32)
	TextViewSetAcceptsTab         func(v Ptr, a bool)
	TextViewSetEditable           func(v Ptr, e bool)
	TextViewSetCursorVisible      func(v Ptr, c bool)
	TextViewSetTopMargin          func(v Ptr, m int32)
	TextViewSetBottomMargin       func(v Ptr, m int32)
	TextViewSetLeftMargin         func(v Ptr, m int32)
	TextViewSetRightMargin        func(v Ptr, m int32)
	TextViewSetJustification      func(v Ptr, j int32)
	TextBufferSetText             func(b Ptr, text string, n int32)
	TextBufferGetStartIter        func(b Ptr, it *TextIter)
	TextBufferGetEndIter          func(b Ptr, it *TextIter)
	TextBufferGetText             func(b Ptr, start, end *TextIter, hidden bool) Ptr
	TextBufferGetSelectionBounds  func(b Ptr, start, end *TextIter) bool
	TextBufferSelectRange         func(b Ptr, ins, bound *TextIter)
	TextBufferGetIterAtOffset     func(b Ptr, it *TextIter, off int32)
	TextBufferInsertAtCursor      func(b Ptr, text string, n int32)
	TextBufferDeleteSelection     func(b Ptr, interactive, editable bool) bool
	TextBufferGetHasSelection     func(b Ptr) bool
	TextIterGetOffset             func(it *TextIter) int32

	// Controls.
	SwitchNew                   func() Ptr
	SwitchSetActive             func(s Ptr, a bool)
	SwitchGetActive             func(s Ptr) bool
	CheckButtonNew              func() Ptr
	CheckButtonSetActive        func(c Ptr, a bool)
	CheckButtonGetActive        func(c Ptr) bool
	ScaleNewWithRange           func(orientation int32, min, max, step float64) Ptr
	ScaleSetDrawValue           func(s Ptr, d bool)
	RangeSetValue               func(r Ptr, v float64)
	RangeGetValue               func(r Ptr) float64
	RangeSetRange               func(r Ptr, min, max float64)
	RangeSetIncrements          func(r Ptr, step, page float64)
	RangeSetInverted            func(r Ptr, i bool)
	SpinnerNew                  func() Ptr
	SpinnerSetSpinning          func(s Ptr, on bool)
	AdwSpinnerNew               func() Ptr // libadwaita 1.6
	ProgressBarNew              func() Ptr
	ProgressBarSetFraction      func(p Ptr, f float64)
	ProgressBarPulse            func(p Ptr)
	ProgressBarSetPulseStep     func(p Ptr, s float64)
	SpinButtonNewWithRange      func(min, max, step float64) Ptr
	SpinButtonSetValue          func(s Ptr, v float64)
	SpinButtonGetValue          func(s Ptr) float64
	SpinButtonSetRange          func(s Ptr, min, max float64)
	SpinButtonSetIncrements     func(s Ptr, step, page float64)
	SpinButtonSetDigits         func(s Ptr, d uint32)
	SpinButtonSetNumeric        func(s Ptr, n bool)
	DropDownNewFromStrings      func(strs *Ptr) Ptr
	DropDownSetModel            func(d, model Ptr)
	DropDownSetSelected         func(d Ptr, i uint32)
	DropDownGetSelected         func(d Ptr) uint32
	StringListNew               func(strs *Ptr) Ptr
	StringListAppend            func(l Ptr, s string)
	StringListSplice            func(l Ptr, pos, nRemove uint32, add *Ptr)
	ColorDialogNew              func() Ptr
	ColorDialogSetWithAlpha     func(d Ptr, a bool)
	ColorDialogSetTitle         func(d Ptr, t string)
	ColorDialogButtonNew        func(dialog Ptr) Ptr
	ColorDialogButtonSetRGBA    func(b Ptr, c *RGBA)
	ColorDialogButtonGetRGBA    func(b Ptr) Ptr
	CalendarNew                 func() Ptr
	CalendarSelectDay           func(c Ptr, dt Ptr)
	CalendarGetDate             func(c Ptr) Ptr
	DateTimeNewLocal            func(y, m, d, h, min int32, s float64) Ptr
	DateTimeNewFromUnixLocal    func(t int64) Ptr
	DateTimeToUnix              func(dt Ptr) int64
	DateTimeUnref               func(dt Ptr)
	DateTimeGetYear             func(dt Ptr) int32
	DateTimeGetMonth            func(dt Ptr) int32
	DateTimeGetDayOfMonth       func(dt Ptr) int32
	DateTimeGetHour             func(dt Ptr) int32
	DateTimeGetMinute           func(dt Ptr) int32
	DateTimeFormat              func(dt Ptr, format string) Ptr
	PopoverNew                  func() Ptr
	PopoverSetChild             func(p, child Ptr)
	PopoverPopup                func(p Ptr)
	PopoverPopdown              func(p Ptr)
	PopoverSetPointingTo        func(p Ptr, r *IntRect)
	PopoverSetHasArrow          func(p Ptr, a bool)
	PopoverSetAutohide          func(p Ptr, a bool)
	PopoverSetPosition          func(p Ptr, pos int32)
	PopoverPresent              func(p Ptr)
	PopoverMenuNewFromModel     func(model Ptr) Ptr
	PopoverMenuNewFromModelFull func(model Ptr, flags int32) Ptr

	// Containers.
	ScrolledWindowNew                      func() Ptr
	ScrolledWindowSetChild                 func(s, child Ptr)
	ScrolledWindowSetPolicy                func(s Ptr, h, v int32)
	ScrolledWindowGetHAdjustment           func(s Ptr) Ptr
	ScrolledWindowGetVAdjustment           func(s Ptr) Ptr
	ScrolledWindowSetKineticScrolling      func(s Ptr, k bool)
	ScrolledWindowSetOverlayScrolling      func(s Ptr, o bool)
	ScrolledWindowSetPropagateNaturalWidth func(s Ptr, p bool)
	ViewportNew                            func(h, v Ptr) Ptr
	ViewportSetChild                       func(v, child Ptr)
	ViewportSetScrollToFocus               func(v Ptr, s bool)
	AdjustmentGetValue                     func(a Ptr) float64
	AdjustmentSetValue                     func(a Ptr, v float64)
	AdjustmentGetUpper                     func(a Ptr) float64
	AdjustmentGetLower                     func(a Ptr) float64
	AdjustmentGetPageSize                  func(a Ptr) float64
	AdjustmentConfigure                    func(a Ptr, value, lower, upper, step, page, pageSize float64)
	BoxNew                                 func(orientation int32, spacing int32) Ptr
	BoxAppend                              func(b, child Ptr)
	BoxRemove                              func(b, child Ptr)
	BoxPrepend                             func(b, child Ptr)
	OverlayNew                             func() Ptr
	OverlaySetChild                        func(o, child Ptr)
	OverlayAddOverlay                      func(o, child Ptr)
	OverlaySetMeasureOverlay               func(o, child Ptr, measure bool)
	StackNew                               func() Ptr

	// Images.
	PictureNew                        func() Ptr
	PictureSetPaintable               func(p, paintable Ptr)
	PictureSetContentFit              func(p Ptr, fit int32)
	PictureSetCanShrink               func(p Ptr, s bool)
	ImageNew                          func() Ptr
	ImageNewFromIconName              func(name string) Ptr
	ImageSetFromIconName              func(i Ptr, name string)
	ImageSetPixelSize                 func(i Ptr, size int32)
	ImageSetFromPaintable             func(i, p Ptr)
	IconThemeGetForDisplay            func(d Ptr) Ptr
	IconThemeHasIcon                  func(t Ptr, name string) bool
	IconThemeAddSearchPath            func(t Ptr, path string)
	IconThemeAddResourcePath          func(t Ptr, path string)
	IconThemeLookupIcon               func(t Ptr, name string, fallbacks *Ptr, size, scale, dir int32, flags uint32) Ptr
	SymbolicPaintableSnapshotSymbolic func(p, snap Ptr, w, h float64, colors *RGBA, n uintptr)

	// Event controllers.
	GestureClickNew                     func() Ptr
	GestureLongPressNew                 func() Ptr
	GestureLongPressSetDelayFactor      func(g Ptr, f float64)
	GestureDragNew                      func() Ptr
	GestureDragGetOffset                func(g Ptr, x, y *float64) bool
	GestureDragGetStartPoint            func(g Ptr, x, y *float64) bool
	GestureZoomNew                      func() Ptr
	GestureZoomGetScaleDelta            func(g Ptr) float64
	GestureRotateNew                    func() Ptr
	GestureSwipeNew                     func() Ptr
	GestureSwipeGetVelocity             func(g Ptr, x, y *float64) bool
	GestureSingleSetButton              func(g Ptr, b uint32)
	GestureSingleGetCurrentButton       func(g Ptr) uint32
	GestureSingleSetTouchOnly           func(g Ptr, t bool)
	GestureSetState                     func(g Ptr, s int32) bool
	GestureGetBoundingBoxCenter         func(g Ptr, x, y *float64) bool
	GestureGetPoint                     func(g Ptr, seq Ptr, x, y *float64) bool
	GestureIsRecognized                 func(g Ptr) bool
	EventControllerMotionNew            func() Ptr
	EventControllerScrollNew            func(flags uint32) Ptr
	EventControllerScrollGetUnit        func(c Ptr) int32
	EventControllerKeyNew               func() Ptr
	EventControllerFocusNew             func() Ptr
	EventControllerSetPropagationPhase  func(c Ptr, phase int32)
	EventControllerSetPropagationLimit  func(c Ptr, limit int32)
	EventControllerGetCurrentEventState func(c Ptr) uint32
	EventControllerGetWidget            func(c Ptr) Ptr
	EventControllerKeyForward           func(c, widget Ptr) bool
	ShortcutControllerNew               func() Ptr
	ShortcutControllerSetScope          func(c Ptr, scope int32)
	ShortcutControllerAddShortcut       func(c, s Ptr)
	ShortcutNew                         func(trigger, action Ptr) Ptr
	ShortcutTriggerParseString          func(s string) Ptr
	CallbackActionNew                   func(cb, data, destroy Ptr) Ptr
	NamedActionNew                      func(name string) Ptr
	KeyvalToUnicode                     func(k uint32) uint32
	KeyvalName                          func(k uint32) Ptr
	KeyvalFromName                      func(name string) uint32
	AcceleratorParse                    func(accel string, key *uint32, mods *uint32) bool

	// Actions and menus.
	SimpleActionGroupNew            func() Ptr
	SimpleActionNew                 func(name string, paramType Ptr) Ptr
	SimpleActionSetEnabled          func(a Ptr, e bool)
	SimpleActionNewStateful         func(name string, paramType, state Ptr) Ptr
	SimpleActionSetState            func(a Ptr, state Ptr)
	ActionMapAddAction              func(m, a Ptr)
	ActionMapRemoveAction           func(m Ptr, name string)
	ActionMapLookupAction           func(m Ptr, name string) Ptr
	MenuNew                         func() Ptr
	MenuAppend                      func(m Ptr, label string, action string)
	MenuAppendSection               func(m Ptr, label Ptr, section Ptr)
	MenuAppendSubmenu               func(m Ptr, label string, sub Ptr)
	MenuAppendItem                  func(m, item Ptr)
	MenuItemNew                     func(label string, action string) Ptr
	MenuItemSetAttributeValue       func(i Ptr, attr string, v Ptr)
	MenuItemSetIcon                 func(i, icon Ptr)
	MenuItemNewSubmenu              func(label string, sub Ptr) Ptr
	VariantNewString                func(s string) Ptr
	VariantNewBoolean               func(b bool) Ptr
	VariantNewInt32                 func(i int32) Ptr
	VariantGetInt32                 func(v Ptr) int32
	VariantTypeNew                  func(t string) Ptr
	ThemedIconNew                   func(name string) Ptr
	ApplicationSetAccelsForAction   func(app Ptr, action string, accels *Ptr)
	ApplicationSetMenubar           func(app, menu Ptr)
	ApplicationWindowSetShowMenubar func(w Ptr, s bool)
	PopoverMenuBarNewFromModel      func(model Ptr) Ptr

	// libadwaita.
	AdwInit                                  func()
	AdwStyleManagerGetDefault                func() Ptr
	AdwStyleManagerGetDark                   func(m Ptr) bool
	AdwStyleManagerSetColorScheme            func(m Ptr, scheme int32)
	AdwStyleManagerGetHighContrast           func(m Ptr) bool
	AdwStyleManagerGetAccentColorRGBA        func(m Ptr) Ptr // 1.6
	AdwToolbarViewNew                        func() Ptr
	AdwToolbarViewSetContent                 func(t, content Ptr)
	AdwToolbarViewAddTopBar                  func(t, bar Ptr)
	AdwToolbarViewAddBottomBar               func(t, bar Ptr)
	AdwToolbarViewRemove                     func(t, bar Ptr)
	AdwToolbarViewSetRevealTopBars           func(t Ptr, r bool)
	AdwToolbarViewSetTopBarStyle             func(t Ptr, s int32)
	AdwToolbarViewGetTopBarHeight            func(t Ptr) int32
	AdwToolbarViewGetBottomBarHeight         func(t Ptr) int32
	AdwToolbarViewSetExtendContentToTopEdge  func(t Ptr, e bool)
	ActionBarNew                             func() Ptr
	ActionBarPackStart                       func(b, w Ptr)
	ActionBarPackEnd                         func(b, w Ptr)
	ActionBarRemove                          func(b, w Ptr)
	ActionBarSetRevealed                     func(b Ptr, r bool)
	AdwHeaderBarNew                          func() Ptr
	AdwHeaderBarPackStart                    func(h, w Ptr)
	AdwHeaderBarPackEnd                      func(h, w Ptr)
	AdwHeaderBarRemove                       func(h, w Ptr)
	AdwHeaderBarSetTitleWidget               func(h, w Ptr)
	AdwHeaderBarSetShowStartTitleButtons     func(h Ptr, s bool)
	AdwHeaderBarSetShowEndTitleButtons       func(h Ptr, s bool)
	AdwHeaderBarSetShowBackButton            func(h Ptr, s bool)
	AdwHeaderBarSetShowTitle                 func(h Ptr, s bool)
	AdwHeaderBarSetDecorationLayout          func(h Ptr, layout string)
	AdwWindowTitleNew                        func(title, subtitle string) Ptr
	AdwWindowTitleSetTitle                   func(t Ptr, title string)
	AdwNavigationViewNew                     func() Ptr
	AdwNavigationViewPush                    func(v, page Ptr)
	AdwNavigationViewPop                     func(v Ptr) bool
	AdwNavigationViewPopToPage               func(v, page Ptr) bool
	AdwNavigationViewReplace                 func(v Ptr, pages *Ptr, n int32)
	AdwNavigationViewAdd                     func(v, page Ptr)
	AdwNavigationViewRemove                  func(v, page Ptr)
	AdwNavigationViewGetVisiblePage          func(v Ptr) Ptr
	AdwNavigationViewGetPreviousPage         func(v, page Ptr) Ptr
	AdwNavigationViewSetAnimateTransitions   func(v Ptr, a bool)
	AdwNavigationViewSetPopOnEscape          func(v Ptr, p bool)
	AdwNavigationViewGetNavigationStack      func(v Ptr) Ptr
	AdwNavigationPageNew                     func(child Ptr, title string) Ptr
	AdwNavigationPageSetTitle                func(p Ptr, title string)
	AdwNavigationPageSetCanPop               func(p Ptr, c bool)
	AdwNavigationPageSetChild                func(p, child Ptr)
	AdwNavigationPageSetTag                  func(p Ptr, tag string)
	AdwViewStackNew                          func() Ptr
	AdwViewStackAddTitledWithIcon            func(s, child Ptr, name, title, icon string) Ptr
	AdwViewStackRemove                       func(s, child Ptr)
	AdwViewStackSetVisibleChild              func(s, child Ptr)
	AdwViewStackGetVisibleChild              func(s Ptr) Ptr
	AdwViewStackSetVisibleChildName          func(s Ptr, name string)
	AdwViewStackPageSetTitle                 func(p Ptr, t string)
	AdwViewStackPageSetIconName              func(p Ptr, n string)
	AdwViewStackPageSetBadgeNumber           func(p Ptr, n uint32)
	AdwViewStackPageSetNeedsAttention        func(p Ptr, a bool)
	AdwViewStackGetPage                      func(s, child Ptr) Ptr
	AdwViewSwitcherNew                       func() Ptr
	AdwViewSwitcherSetStack                  func(sw, stack Ptr)
	AdwViewSwitcherSetPolicy                 func(sw Ptr, p int32)
	AdwViewSwitcherBarNew                    func() Ptr
	AdwViewSwitcherBarSetStack               func(sw, stack Ptr)
	AdwViewSwitcherBarSetReveal              func(sw Ptr, r bool)
	AdwDialogNew                             func() Ptr
	AdwDialogSetChild                        func(d, child Ptr)
	AdwDialogSetTitle                        func(d Ptr, t string)
	AdwDialogSetContentWidth                 func(d Ptr, w int32)
	AdwDialogSetContentHeight                func(d Ptr, h int32)
	AdwDialogSetFollowsContentSize           func(d Ptr, f bool)
	AdwDialogSetPresentationMode             func(d Ptr, m int32)
	AdwDialogSetCanClose                     func(d Ptr, c bool)
	AdwDialogGetCanClose                     func(d Ptr) bool
	AdwDialogGetPresentationMode             func(d Ptr) int32
	AdwDialogPresent                         func(d, parent Ptr)
	AdwDialogClose                           func(d Ptr) bool
	AdwDialogForceClose                      func(d Ptr)
	AdwAlertDialogNew                        func(heading, body string) Ptr
	AdwAlertDialogAddResponse                func(d Ptr, id, label string)
	AdwAlertDialogSetResponseAppearance      func(d Ptr, id string, a int32)
	AdwAlertDialogSetDefaultResponse         func(d Ptr, id string)
	AdwAlertDialogSetCloseResponse           func(d Ptr, id string)
	AdwAlertDialogSetPreferWideLayout        func(d Ptr, w bool)
	AdwAlertDialogSetResponseEnabled         func(d Ptr, id string, enabled bool)
	AdwAlertDialogSetResponseLabel           func(d Ptr, id, label string)
	AdwToastOverlayNew                       func() Ptr
	AdwBinNew                                func() Ptr
	AdwBinSetChild                           func(b, child Ptr)
	AdwOverlaySplitViewNew                   func() Ptr
	AdwNavigationSplitViewNew                func() Ptr
	AdwNavigationSplitViewSetSidebar         func(v, page Ptr)
	AdwNavigationSplitViewSetContent         func(v, page Ptr)
	AdwNavigationSplitViewSetCollapsed       func(v Ptr, c bool)
	AdwNavigationSplitViewSetShowContent     func(v Ptr, s bool)
	AdwNavigationSplitViewSetMinSidebarWidth func(v Ptr, w float64)
	AdwNavigationSplitViewSetMaxSidebarWidth func(v Ptr, w float64)
	AdwBreakpointNew                         func(cond Ptr) Ptr
	AdwBreakpointConditionParse              func(s string) Ptr
	AdwBreakpointAddSetter                   func(b, object Ptr, property string, value *Value)
	AdwApplicationWindowAddBreakpoint        func(w, b Ptr)
	AdwSpinRowNew                            func(adj Ptr, climb float64, digits uint32) Ptr

	// Accessibility of GTK's own.
	AccessibleGetATContext func(a Ptr) Ptr
)

type binding struct {
	p    any
	name string
}

var bindings = []binding{
	{&Free, "g_free"},
	{&MainContextDefault, "g_main_context_default"},
	{&MainContextIsOwner, "g_main_context_is_owner"},
	{&MainContextIteration, "g_main_context_iteration"},
	{&ObjectRef, "g_object_ref"},
	{&ObjectRefSink, "g_object_ref_sink"},
	{&ObjectUnref, "g_object_unref"},
	{&ObjectGetProperty, "g_object_get_property"},
	{&ObjectSetProperty, "g_object_set_property"},
	{&ValueInit, "g_value_init"},
	{&ValueUnset, "g_value_unset"},
	{&ValueGetBoolean, "g_value_get_boolean"},
	{&ValueSetBoolean, "g_value_set_boolean"},
	{&ValueGetInt, "g_value_get_int"},
	{&ValueGetDouble, "g_value_get_double"},
	{&ValueGetString, "g_value_get_string"},
	{&ValueSetString, "g_value_set_string"},
	{&ValueGetObject, "g_value_get_object"},
	{&ValueGetBoxed, "g_value_get_boxed"},
	{&ValueSetEnum, "g_value_set_enum"},
	{&FrameClockGetFrameTime, "gdk_frame_clock_get_frame_time"},
	{&WidgetGetOpacity, "gtk_widget_get_opacity"},
	{&WidgetSetDefaultDirection, "gtk_widget_set_default_direction"},
	{&TypeFromName, "g_type_from_name"},
	{&TypeCheckInstanceIsA, "g_type_check_instance_is_a"},
	{&SignalStopEmissionByName, "g_signal_stop_emission_by_name"},
	{&BytesNew, "g_bytes_new"},
	{&BytesUnref, "g_bytes_unref"},
	{&BytesGetData, "g_bytes_get_data"},
	{&ErrorFree, "g_error_free"},
	{&GetUserConfigDir, "g_get_user_config_dir"},
	{&GetUserDataDir, "g_get_user_data_dir"},

	{&SettingsSchemaSourceGetDefault, "g_settings_schema_source_get_default"},
	{&SettingsSchemaSourceLookup, "g_settings_schema_source_lookup"},
	{&SettingsSchemaHasKey, "g_settings_schema_has_key"},
	{&SettingsSchemaUnref, "g_settings_schema_unref"},
	{&SettingsNew, "g_settings_new"},
	{&SettingsGetDouble, "g_settings_get_double"},
	{&SettingsGetString, "g_settings_get_string"},
	{&SettingsGetBoolean, "g_settings_get_boolean"},
	{&SettingsGetInt, "g_settings_get_int"},
	{&SettingsGetEnum, "g_settings_get_enum"},

	{&AdwApplicationNew, "adw_application_new"},
	{&ApplicationRun, "g_application_run"},
	{&ApplicationQuit, "g_application_quit"},
	{&ApplicationHold, "g_application_hold"},
	{&ApplicationRelease, "g_application_release"},
	{&ApplicationIDIsValid, "g_application_id_is_valid"},
	{&ApplicationRegister, "g_application_register"},
	{&ApplicationActivate, "g_application_activate"},

	{&DisplayGetDefault, "gdk_display_get_default"},
	{&DisplayGetClipboard, "gdk_display_get_clipboard"},
	{&ClipboardSetText, "gdk_clipboard_set_text"},
	{&ClipboardReadTextAsync, "gdk_clipboard_read_text_async"},
	{&ClipboardReadTextFinish, "gdk_clipboard_read_text_finish"},
	{&UriLauncherNew, "gtk_uri_launcher_new"},
	{&UriLauncherLaunch, "gtk_uri_launcher_launch"},
	{&ApplicationInhibit, "gtk_application_inhibit"},
	{&ApplicationUninhibit, "gtk_application_uninhibit"},
	{&SurfaceGetScale, "gdk_surface_get_scale"},
	{&SurfaceGetScaleFactor, "gdk_surface_get_scale_factor"},
	{&TextureNewFromBytes, "gdk_texture_new_from_bytes"},
	{&TextureNewFromFilename, "gdk_texture_new_from_filename"},
	{&TextureGetWidth, "gdk_texture_get_width"},
	{&TextureGetHeight, "gdk_texture_get_height"},
	{&TextureSaveToPNG, "gdk_texture_save_to_png"},
	{&MemoryTextureNew, "gdk_memory_texture_new"},
	{&CursorNewFromName, "gdk_cursor_new_from_name"},

	{&TransformRotate, "gsk_transform_rotate"},
	{&TransformScale, "gsk_transform_scale"},
	{&TransformUnref, "gsk_transform_unref"},
	{&PathParse, "gsk_path_parse"},
	{&PathUnref, "gsk_path_unref"},
	{&PathBuilderNew, "gsk_path_builder_new"},
	{&PathBuilderMoveTo, "gsk_path_builder_move_to"},
	{&PathBuilderLineTo, "gsk_path_builder_line_to"},
	{&PathBuilderQuadTo, "gsk_path_builder_quad_to"},
	{&PathBuilderCubicTo, "gsk_path_builder_cubic_to"},
	{&PathBuilderClose, "gsk_path_builder_close"},
	{&PathBuilderAddRect, "gsk_path_builder_add_rect"},
	{&PathBuilderAddRoundedRect, "gsk_path_builder_add_rounded_rect"},
	{&PathBuilderAddCircle, "gsk_path_builder_add_circle"},
	{&PathBuilderArcTo, "gsk_path_builder_arc_to"},
	{&PathBuilderToPath, "gsk_path_builder_to_path"},
	{&PathBuilderFreeToPath, "gsk_path_builder_free_to_path"},
	{&PathBuilderAddPath, "gsk_path_builder_add_path"},
	{&PathBuilderAddSegment, "gsk_path_builder_add_segment"},
	{&PathMeasureNew, "gsk_path_measure_new"},
	{&PathMeasureGetLength, "gsk_path_measure_get_length"},
	{&PathMeasureGetPoint, "gsk_path_measure_get_point"},
	{&PathMeasureUnref, "gsk_path_measure_unref"},
	{&StrokeNew, "gsk_stroke_new"},
	{&StrokeFree, "gsk_stroke_free"},
	{&StrokeSetLineCap, "gsk_stroke_set_line_cap"},
	{&StrokeSetLineJoin, "gsk_stroke_set_line_join"},
	{&StrokeSetDash, "gsk_stroke_set_dash"},
	{&StrokeSetDashOffset, "gsk_stroke_set_dash_offset"},
	{&RendererRenderTexture, "gsk_renderer_render_texture"},
	{&RenderNodeUnref, "gsk_render_node_unref"},

	{&SnapshotNew, "gtk_snapshot_new"},
	{&SnapshotFreeToNode, "gtk_snapshot_free_to_node"},
	{&SnapshotPushOpacity, "gtk_snapshot_push_opacity"},
	{&SnapshotRotate, "gtk_snapshot_rotate"},
	{&SnapshotScale, "gtk_snapshot_scale"},
	{&SnapshotAppendOutsetShadow, "gtk_snapshot_append_outset_shadow"},
	{&SnapshotAppendInsetShadow, "gtk_snapshot_append_inset_shadow"},
	{&SnapshotPushBlur, "gtk_snapshot_push_blur"},
	{&SnapshotPushMask, "gtk_snapshot_push_mask"},
	{&SnapshotAppendTexture, "gtk_snapshot_append_texture"},
	{&SnapshotAppendScaledTexture, "gtk_snapshot_append_scaled_texture"},
	{&PaintableSnapshot, "gdk_paintable_snapshot"},

	{&WidgetSetParent, "gtk_widget_set_parent"},
	{&WidgetUnparent, "gtk_widget_unparent"},
	{&WidgetInsertBefore, "gtk_widget_insert_before"},
	{&WidgetInsertAfter, "gtk_widget_insert_after"},
	{&WidgetGetParent, "gtk_widget_get_parent"},
	{&WidgetGetLastChild, "gtk_widget_get_last_child"},
	{&WidgetGetPrevSibling, "gtk_widget_get_prev_sibling"},
	{&WidgetSetVisible, "gtk_widget_set_visible"},
	{&WidgetGetVisible, "gtk_widget_get_visible"},
	{&WidgetGetMapped, "gtk_widget_get_mapped"},
	{&WidgetSetOpacity, "gtk_widget_set_opacity"},
	{&WidgetSetSensitive, "gtk_widget_set_sensitive"},
	{&WidgetSetTooltipText, "gtk_widget_set_tooltip_text"},
	{&WidgetAddCSSClass, "gtk_widget_add_css_class"},
	{&WidgetRemoveCSSClass, "gtk_widget_remove_css_class"},
	{&WidgetHasCSSClass, "gtk_widget_has_css_class"},
	{&WidgetGetSensitive, "gtk_widget_get_sensitive"},
	{&WidgetAddController, "gtk_widget_add_controller"},
	{&WidgetRemoveController, "gtk_widget_remove_controller"},
	{&WidgetGrabFocus, "gtk_widget_grab_focus"},
	{&WidgetHasFocus, "gtk_widget_has_focus"},
	{&WidgetIsFocus, "gtk_widget_is_focus"},
	{&WidgetHasVisibleFocus, "gtk_widget_has_visible_focus"},
	{&WidgetSetFocusable, "gtk_widget_set_focusable"},
	{&WidgetSetFocusOnClick, "gtk_widget_set_focus_on_click"},
	{&WidgetSetCanTarget, "gtk_widget_set_can_target"},
	{&WidgetSetCanFocus, "gtk_widget_set_can_focus"},
	{&WidgetSetOverflow, "gtk_widget_set_overflow"},
	{&WidgetGetScaleFactor, "gtk_widget_get_scale_factor"},
	{&WidgetGetNative, "gtk_widget_get_native"},
	{&WidgetGetRoot, "gtk_widget_get_root"},
	{&WidgetSetDirection, "gtk_widget_set_direction"},
	{&WidgetQueueResize, "gtk_widget_queue_resize"},
	{&WidgetSetHExpand, "gtk_widget_set_hexpand"},
	{&WidgetSetVExpand, "gtk_widget_set_vexpand"},
	{&WidgetSetHAlign, "gtk_widget_set_halign"},
	{&WidgetSetVAlign, "gtk_widget_set_valign"},
	{&WidgetSetSizeRequest, "gtk_widget_set_size_request"},
	{&WidgetSetCursorFromName, "gtk_widget_set_cursor_from_name"},
	{&WidgetComputeBounds, "gtk_widget_compute_bounds"},
	{&WidgetComputePoint, "gtk_widget_compute_point"},
	{&WidgetGetDisplay, "gtk_widget_get_display"},
	{&WidgetGetColor, "gtk_widget_get_color"},
	{&WidgetGetStyleContext, "gtk_widget_get_style_context"},
	{&StyleContextLookupColor, "gtk_style_context_lookup_color"},
	{&WidgetSetName, "gtk_widget_set_name"},
	{&WidgetActivateAction, "gtk_widget_activate_action_variant"},
	{&WidgetPick, "gtk_widget_pick"},
	{&WidgetContains, "gtk_widget_contains"},
	{&WidgetGetFirstChild, "gtk_widget_get_first_child"},
	{&WidgetGetNextSibling, "gtk_widget_get_next_sibling"},
	{&WidgetInsertActionGroup, "gtk_widget_insert_action_group"},
	{&WidgetTriggerTooltipQuery, "gtk_widget_trigger_tooltip_query"},
	{&WidgetMnemonicActivate, "gtk_widget_mnemonic_activate"},
	{&WidgetActivate, "gtk_widget_activate"},
	{&WidgetGetAllocatedBaseline, "gtk_widget_get_baseline"},
	{&WidgetPaintableNew, "gtk_widget_paintable_new"},
	{&NativeGetSurface, "gtk_native_get_surface"},
	{&NativeGetRenderer, "gtk_native_get_renderer"},
	{&StyleContextAddProviderForDisplay, "gtk_style_context_add_provider_for_display"},
	{&StyleContextRemoveProviderForDisplay, "gtk_style_context_remove_provider_for_display"},
	{&CSSProviderNew, "gtk_css_provider_new"},
	{&CSSProviderLoadFromString, "gtk_css_provider_load_from_string"},
	{&SettingsGetDefault, "gtk_settings_get_default"},
	{&AccessibleUpdateProperty, "gtk_accessible_update_property_value"},
	{&AccessibleUpdateState, "gtk_accessible_update_state_value"},
	{&AccessibleResetProperty, "gtk_accessible_reset_property"},
	{&AccessibleAnnounce, "gtk_accessible_announce"},

	{&AdwApplicationWindowNew, "adw_application_window_new"},
	{&AdwApplicationWindowSetContent, "adw_application_window_set_content"},
	{&AdwWindowNew, "adw_window_new"},
	{&AdwWindowSetContent, "adw_window_set_content"},
	{&WindowSetTitle, "gtk_window_set_title"},
	{&WindowSetDefaultSize, "gtk_window_set_default_size"},
	{&WindowGetDefaultSize, "gtk_window_get_default_size"},
	{&WindowSetResizable, "gtk_window_set_resizable"},
	{&WindowSetModal, "gtk_window_set_modal"},
	{&WindowSetTransientFor, "gtk_window_set_transient_for"},
	{&WindowSetHideOnClose, "gtk_window_set_hide_on_close"},
	{&WindowSetTitlebar, "gtk_window_set_titlebar"},
	{&WindowPresent, "gtk_window_present"},
	{&WindowClose, "gtk_window_close"},
	{&WindowDestroy, "gtk_window_destroy"},
	{&WindowIsActive, "gtk_window_is_active"},
	{&WindowIsMaximized, "gtk_window_is_maximized"},
	{&WindowMaximize, "gtk_window_maximize"},
	{&WindowGetFocus, "gtk_window_get_focus"},
	{&WindowSetFocus, "gtk_window_set_focus"},
	{&WindowSetApplication, "gtk_window_set_application"},
	{&WindowSetDecorated, "gtk_window_set_decorated"},
	{&WindowListToplevels, "gtk_window_list_toplevels"},
	{&ApplicationGetActiveWindow, "gtk_application_get_active_window"},
	{&WindowSetDeletable, "gtk_window_set_deletable"},
	{&WindowSetDestroyWithParent, "gtk_window_set_destroy_with_parent"},
	{&WindowMinimize, "gtk_window_minimize"},
	{&WindowUnminimize, "gtk_window_unminimize"},
	{&WindowUnmaximize, "gtk_window_unmaximize"},
	{&WindowFullscreen, "gtk_window_fullscreen"},
	{&WindowUnfullscreen, "gtk_window_unfullscreen"},
	{&WindowIsFullscreen, "gtk_window_is_fullscreen"},

	{&LabelNew, "gtk_label_new"},
	{&LabelSetText, "gtk_label_set_text"},
	{&LabelSetAttributes, "gtk_label_set_attributes"},
	{&LabelSetXAlign, "gtk_label_set_xalign"},
	{&LabelSetYAlign, "gtk_label_set_yalign"},
	{&LabelSetWrap, "gtk_label_set_wrap"},
	{&LabelSetWrapMode, "gtk_label_set_wrap_mode"},
	{&LabelSetNaturalWrapMode, "gtk_label_set_natural_wrap_mode"},
	{&LabelSetLines, "gtk_label_set_lines"},
	{&LabelSetEllipsize, "gtk_label_set_ellipsize"},
	{&LabelSetJustify, "gtk_label_set_justify"},
	{&LabelSetSelectable, "gtk_label_set_selectable"},
	{&LabelSetUseMarkup, "gtk_label_set_use_markup"},
	{&LabelSetMarkup, "gtk_label_set_markup"},
	{&LabelGetLayout, "gtk_label_get_layout"},
	{&LabelSetMaxWidthChars, "gtk_label_set_max_width_chars"},
	{&AttrListNew, "pango_attr_list_new"},
	{&AttrListInsert, "pango_attr_list_insert"},
	{&AttrListUnref, "pango_attr_list_unref"},
	{&AttrSizeNewAbsolute, "pango_attr_size_new_absolute"},
	{&AttrWeightNew, "pango_attr_weight_new"},
	{&AttrStyleNew, "pango_attr_style_new"},
	{&AttrForegroundNew, "pango_attr_foreground_new"},
	{&AttrForegroundAlphaNew, "pango_attr_foreground_alpha_new"},
	{&AttrBackgroundNew, "pango_attr_background_new"},
	{&AttrBackgroundAlphaNew, "pango_attr_background_alpha_new"},
	{&AttrFamilyNew, "pango_attr_family_new"},
	{&AttrUnderlineNew, "pango_attr_underline_new"},
	{&AttrStrikethroughNew, "pango_attr_strikethrough_new"},
	{&AttrFontFeaturesNew, "pango_attr_font_features_new"},
	{&AttrFallbackNew, "pango_attr_fallback_new"},
	{&AttrShapeNew, "pango_attr_shape_new"},
	{&AttrLineHeightNewAbsolute, "pango_attr_line_height_new_absolute"},
	{&FontMapGetDefault, "pango_cairo_font_map_get_default"},
	{&FontDescriptionFromString, "pango_font_description_from_string"},
	{&FontDescriptionFree, "pango_font_description_free"},
	{&LayoutNew, "pango_layout_new"},
	{&LayoutSetText, "pango_layout_set_text"},
	{&LayoutSetFontDescription, "pango_layout_set_font_description"},
	{&LayoutSetAttributes, "pango_layout_set_attributes"},
	{&LayoutSetWidth, "pango_layout_set_width"},
	{&LayoutSetAlignment, "pango_layout_set_alignment"},
	{&LayoutGetPixelSize, "pango_layout_get_pixel_size"},
	{&LayoutGetBaseline, "pango_layout_get_baseline"},
	{&LayoutSetWrap, "pango_layout_set_wrap"},
	{&LayoutSetHeight, "pango_layout_set_height"},
	{&LayoutSetEllipsize, "pango_layout_set_ellipsize"},
	{&WidgetCreatePangoLayout, "gtk_widget_create_pango_layout"},
	{&WidgetGetPangoContext, "gtk_widget_get_pango_context"},
	{&FontMapLoadFont, "pango_font_map_load_font"},

	{&ButtonNew, "gtk_button_new"},
	{&ButtonSetLabel, "gtk_button_set_label"},
	{&ButtonSetChild, "gtk_button_set_child"},
	{&ButtonSetIconName, "gtk_button_set_icon_name"},
	{&ButtonSetHasFrame, "gtk_button_set_has_frame"},
	{&ButtonSetUseUnderline, "gtk_button_set_use_underline"},
	{&ButtonContentNew, "adw_button_content_new"},
	{&ButtonContentSetLabel, "adw_button_content_set_label"},
	{&ButtonContentSetIconName, "adw_button_content_set_icon_name"},
	{&MenuButtonNew, "gtk_menu_button_new"},
	{&MenuButtonSetPopover, "gtk_menu_button_set_popover"},
	{&MenuButtonSetMenuModel, "gtk_menu_button_set_menu_model"},
	{&MenuButtonSetLabel, "gtk_menu_button_set_label"},
	{&MenuButtonSetIconName, "gtk_menu_button_set_icon_name"},
	{&MenuButtonPopup, "gtk_menu_button_popup"},
	{&ToggleButtonNew, "gtk_toggle_button_new"},
	{&ToggleButtonSetActive, "gtk_toggle_button_set_active"},
	{&ToggleButtonGetActive, "gtk_toggle_button_get_active"},
	{&ToggleButtonSetGroup, "gtk_toggle_button_set_group"},

	{&EntryNew, "gtk_entry_new"},
	{&EntrySetPlaceholderText, "gtk_entry_set_placeholder_text"},
	{&EntrySetInputPurpose, "gtk_entry_set_input_purpose"},
	{&EntrySetInputHints, "gtk_entry_set_input_hints"},
	{&EntrySetHasFrame, "gtk_entry_set_has_frame"},
	{&EntrySetVisibility, "gtk_entry_set_visibility"},
	{&EntrySetIconFromIconName, "gtk_entry_set_icon_from_icon_name"},
	{&EntrySetAlignment, "gtk_entry_set_alignment"},
	{&EntrySetAttributes, "gtk_entry_set_attributes"},
	{&PasswordEntryNew, "gtk_password_entry_new"},
	{&PasswordEntrySetShowPeekIcon, "gtk_password_entry_set_show_peek_icon"},
	{&SearchEntryNew, "gtk_search_entry_new"},
	{&SearchEntrySetPlaceholderText, "gtk_search_entry_set_placeholder_text"},
	{&SearchBarNew, "gtk_search_bar_new"},
	{&SearchBarSetChild, "gtk_search_bar_set_child"},
	{&SearchBarConnectEntry, "gtk_search_bar_connect_entry"},
	{&SearchBarSetKeyCaptureWidget, "gtk_search_bar_set_key_capture_widget"},
	{&SearchBarSetSearchMode, "gtk_search_bar_set_search_mode"},
	{&SearchBarGetSearchMode, "gtk_search_bar_get_search_mode"},
	{&EditableSetText, "gtk_editable_set_text"},
	{&EditableGetPosition, "gtk_editable_get_position"},
	{&EditableSetPosition, "gtk_editable_set_position"},
	{&EditableSelectRegion, "gtk_editable_select_region"},
	{&EditableGetSelectionBounds, "gtk_editable_get_selection_bounds"},
	{&EditableSetEditable, "gtk_editable_set_editable"},
	{&EditableInsertText, "gtk_editable_insert_text"},
	{&EditableDeleteSelection, "gtk_editable_delete_selection"},
	{&EditableGetDelegate, "gtk_editable_get_delegate"},
	{&EditableSetAlignment, "gtk_editable_set_alignment"},
	{&TextViewNew, "gtk_text_view_new"},
	{&TextViewGetBuffer, "gtk_text_view_get_buffer"},
	{&TextViewSetWrapMode, "gtk_text_view_set_wrap_mode"},
	{&TextViewSetAcceptsTab, "gtk_text_view_set_accepts_tab"},
	{&TextViewSetEditable, "gtk_text_view_set_editable"},
	{&TextViewSetCursorVisible, "gtk_text_view_set_cursor_visible"},
	{&TextViewSetTopMargin, "gtk_text_view_set_top_margin"},
	{&TextViewSetBottomMargin, "gtk_text_view_set_bottom_margin"},
	{&TextViewSetLeftMargin, "gtk_text_view_set_left_margin"},
	{&TextViewSetRightMargin, "gtk_text_view_set_right_margin"},
	{&TextViewSetJustification, "gtk_text_view_set_justification"},
	{&TextBufferSetText, "gtk_text_buffer_set_text"},
	{&TextBufferGetStartIter, "gtk_text_buffer_get_start_iter"},
	{&TextBufferGetEndIter, "gtk_text_buffer_get_end_iter"},
	{&TextBufferGetText, "gtk_text_buffer_get_text"},
	{&TextBufferGetSelectionBounds, "gtk_text_buffer_get_selection_bounds"},
	{&TextBufferSelectRange, "gtk_text_buffer_select_range"},
	{&TextBufferGetIterAtOffset, "gtk_text_buffer_get_iter_at_offset"},
	{&TextBufferInsertAtCursor, "gtk_text_buffer_insert_at_cursor"},
	{&TextBufferDeleteSelection, "gtk_text_buffer_delete_selection"},
	{&TextBufferGetHasSelection, "gtk_text_buffer_get_has_selection"},
	{&TextIterGetOffset, "gtk_text_iter_get_offset"},

	{&SwitchNew, "gtk_switch_new"},
	{&SwitchSetActive, "gtk_switch_set_active"},
	{&SwitchGetActive, "gtk_switch_get_active"},
	{&CheckButtonNew, "gtk_check_button_new"},
	{&CheckButtonSetActive, "gtk_check_button_set_active"},
	{&CheckButtonGetActive, "gtk_check_button_get_active"},
	{&ScaleNewWithRange, "gtk_scale_new_with_range"},
	{&ScaleSetDrawValue, "gtk_scale_set_draw_value"},
	{&RangeSetValue, "gtk_range_set_value"},
	{&RangeGetValue, "gtk_range_get_value"},
	{&RangeSetRange, "gtk_range_set_range"},
	{&RangeSetIncrements, "gtk_range_set_increments"},
	{&RangeSetInverted, "gtk_range_set_inverted"},
	{&SpinnerNew, "gtk_spinner_new"},
	{&SpinnerSetSpinning, "gtk_spinner_set_spinning"},
	{&AdwSpinnerNew, "adw_spinner_new"},
	{&ProgressBarNew, "gtk_progress_bar_new"},
	{&ProgressBarSetFraction, "gtk_progress_bar_set_fraction"},
	{&ProgressBarPulse, "gtk_progress_bar_pulse"},
	{&ProgressBarSetPulseStep, "gtk_progress_bar_set_pulse_step"},
	{&SpinButtonNewWithRange, "gtk_spin_button_new_with_range"},
	{&SpinButtonSetValue, "gtk_spin_button_set_value"},
	{&SpinButtonGetValue, "gtk_spin_button_get_value"},
	{&SpinButtonSetRange, "gtk_spin_button_set_range"},
	{&SpinButtonSetIncrements, "gtk_spin_button_set_increments"},
	{&SpinButtonSetDigits, "gtk_spin_button_set_digits"},
	{&SpinButtonSetNumeric, "gtk_spin_button_set_numeric"},
	{&DropDownNewFromStrings, "gtk_drop_down_new_from_strings"},
	{&DropDownSetModel, "gtk_drop_down_set_model"},
	{&DropDownSetSelected, "gtk_drop_down_set_selected"},
	{&DropDownGetSelected, "gtk_drop_down_get_selected"},
	{&StringListNew, "gtk_string_list_new"},
	{&StringListAppend, "gtk_string_list_append"},
	{&StringListSplice, "gtk_string_list_splice"},
	{&ColorDialogNew, "gtk_color_dialog_new"},
	{&ColorDialogSetWithAlpha, "gtk_color_dialog_set_with_alpha"},
	{&ColorDialogSetTitle, "gtk_color_dialog_set_title"},
	{&ColorDialogButtonNew, "gtk_color_dialog_button_new"},
	{&ColorDialogButtonSetRGBA, "gtk_color_dialog_button_set_rgba"},
	{&ColorDialogButtonGetRGBA, "gtk_color_dialog_button_get_rgba"},
	{&CalendarNew, "gtk_calendar_new"},
	{&CalendarSelectDay, "gtk_calendar_select_day"},
	{&CalendarGetDate, "gtk_calendar_get_date"},
	{&DateTimeNewLocal, "g_date_time_new_local"},
	{&DateTimeNewFromUnixLocal, "g_date_time_new_from_unix_local"},
	{&DateTimeToUnix, "g_date_time_to_unix"},
	{&DateTimeUnref, "g_date_time_unref"},
	{&DateTimeGetYear, "g_date_time_get_year"},
	{&DateTimeGetMonth, "g_date_time_get_month"},
	{&DateTimeGetDayOfMonth, "g_date_time_get_day_of_month"},
	{&DateTimeGetHour, "g_date_time_get_hour"},
	{&DateTimeGetMinute, "g_date_time_get_minute"},
	{&DateTimeFormat, "g_date_time_format"},
	{&PopoverNew, "gtk_popover_new"},
	{&PopoverSetChild, "gtk_popover_set_child"},
	{&PopoverPopup, "gtk_popover_popup"},
	{&PopoverPopdown, "gtk_popover_popdown"},
	{&PopoverSetPointingTo, "gtk_popover_set_pointing_to"},
	{&PopoverSetHasArrow, "gtk_popover_set_has_arrow"},
	{&PopoverSetAutohide, "gtk_popover_set_autohide"},
	{&PopoverSetPosition, "gtk_popover_set_position"},
	{&PopoverPresent, "gtk_popover_present"},
	{&PopoverMenuNewFromModel, "gtk_popover_menu_new_from_model"},
	{&PopoverMenuNewFromModelFull, "gtk_popover_menu_new_from_model_full"},

	{&ScrolledWindowNew, "gtk_scrolled_window_new"},
	{&ScrolledWindowSetChild, "gtk_scrolled_window_set_child"},
	{&ScrolledWindowSetPolicy, "gtk_scrolled_window_set_policy"},
	{&ScrolledWindowGetHAdjustment, "gtk_scrolled_window_get_hadjustment"},
	{&ScrolledWindowGetVAdjustment, "gtk_scrolled_window_get_vadjustment"},
	{&ScrolledWindowSetKineticScrolling, "gtk_scrolled_window_set_kinetic_scrolling"},
	{&ScrolledWindowSetOverlayScrolling, "gtk_scrolled_window_set_overlay_scrolling"},
	{&ScrolledWindowSetPropagateNaturalWidth, "gtk_scrolled_window_set_propagate_natural_width"},
	{&ViewportNew, "gtk_viewport_new"},
	{&ViewportSetChild, "gtk_viewport_set_child"},
	{&ViewportSetScrollToFocus, "gtk_viewport_set_scroll_to_focus"},
	{&AdjustmentGetValue, "gtk_adjustment_get_value"},
	{&AdjustmentSetValue, "gtk_adjustment_set_value"},
	{&AdjustmentGetUpper, "gtk_adjustment_get_upper"},
	{&AdjustmentGetLower, "gtk_adjustment_get_lower"},
	{&AdjustmentGetPageSize, "gtk_adjustment_get_page_size"},
	{&AdjustmentConfigure, "gtk_adjustment_configure"},
	{&BoxNew, "gtk_box_new"},
	{&BoxAppend, "gtk_box_append"},
	{&BoxRemove, "gtk_box_remove"},
	{&BoxPrepend, "gtk_box_prepend"},
	{&OverlayNew, "gtk_overlay_new"},
	{&OverlaySetChild, "gtk_overlay_set_child"},
	{&OverlayAddOverlay, "gtk_overlay_add_overlay"},
	{&OverlaySetMeasureOverlay, "gtk_overlay_set_measure_overlay"},
	{&StackNew, "gtk_stack_new"},

	{&PictureNew, "gtk_picture_new"},
	{&PictureSetPaintable, "gtk_picture_set_paintable"},
	{&PictureSetContentFit, "gtk_picture_set_content_fit"},
	{&PictureSetCanShrink, "gtk_picture_set_can_shrink"},
	{&ImageNew, "gtk_image_new"},
	{&ImageNewFromIconName, "gtk_image_new_from_icon_name"},
	{&ImageSetFromIconName, "gtk_image_set_from_icon_name"},
	{&ImageSetPixelSize, "gtk_image_set_pixel_size"},
	{&ImageSetFromPaintable, "gtk_image_set_from_paintable"},
	{&IconThemeGetForDisplay, "gtk_icon_theme_get_for_display"},
	{&IconThemeHasIcon, "gtk_icon_theme_has_icon"},
	{&IconThemeAddSearchPath, "gtk_icon_theme_add_search_path"},
	{&IconThemeAddResourcePath, "gtk_icon_theme_add_resource_path"},
	{&IconThemeLookupIcon, "gtk_icon_theme_lookup_icon"},
	{&SymbolicPaintableSnapshotSymbolic, "gtk_symbolic_paintable_snapshot_symbolic"},

	{&GestureClickNew, "gtk_gesture_click_new"},
	{&GestureLongPressNew, "gtk_gesture_long_press_new"},
	{&GestureLongPressSetDelayFactor, "gtk_gesture_long_press_set_delay_factor"},
	{&GestureDragNew, "gtk_gesture_drag_new"},
	{&GestureDragGetOffset, "gtk_gesture_drag_get_offset"},
	{&GestureDragGetStartPoint, "gtk_gesture_drag_get_start_point"},
	{&GestureZoomNew, "gtk_gesture_zoom_new"},
	{&GestureZoomGetScaleDelta, "gtk_gesture_zoom_get_scale_delta"},
	{&GestureRotateNew, "gtk_gesture_rotate_new"},
	{&GestureSwipeNew, "gtk_gesture_swipe_new"},
	{&GestureSwipeGetVelocity, "gtk_gesture_swipe_get_velocity"},
	{&GestureSingleSetButton, "gtk_gesture_single_set_button"},
	{&GestureSingleGetCurrentButton, "gtk_gesture_single_get_current_button"},
	{&GestureSingleSetTouchOnly, "gtk_gesture_single_set_touch_only"},
	{&GestureSetState, "gtk_gesture_set_state"},
	{&GestureGetBoundingBoxCenter, "gtk_gesture_get_bounding_box_center"},
	{&GestureGetPoint, "gtk_gesture_get_point"},
	{&GestureIsRecognized, "gtk_gesture_is_recognized"},
	{&EventControllerMotionNew, "gtk_event_controller_motion_new"},
	{&EventControllerScrollNew, "gtk_event_controller_scroll_new"},
	{&EventControllerScrollGetUnit, "gtk_event_controller_scroll_get_unit"},
	{&EventControllerKeyNew, "gtk_event_controller_key_new"},
	{&EventControllerFocusNew, "gtk_event_controller_focus_new"},
	{&EventControllerSetPropagationPhase, "gtk_event_controller_set_propagation_phase"},
	{&EventControllerSetPropagationLimit, "gtk_event_controller_set_propagation_limit"},
	{&EventControllerGetCurrentEventState, "gtk_event_controller_get_current_event_state"},
	{&EventControllerGetWidget, "gtk_event_controller_get_widget"},
	{&EventControllerKeyForward, "gtk_event_controller_key_forward"},
	{&ShortcutControllerNew, "gtk_shortcut_controller_new"},
	{&ShortcutControllerSetScope, "gtk_shortcut_controller_set_scope"},
	{&ShortcutControllerAddShortcut, "gtk_shortcut_controller_add_shortcut"},
	{&ShortcutNew, "gtk_shortcut_new"},
	{&ShortcutTriggerParseString, "gtk_shortcut_trigger_parse_string"},
	{&CallbackActionNew, "gtk_callback_action_new"},
	{&NamedActionNew, "gtk_named_action_new"},
	{&KeyvalToUnicode, "gdk_keyval_to_unicode"},
	{&KeyvalName, "gdk_keyval_name"},
	{&KeyvalFromName, "gdk_keyval_from_name"},
	{&AcceleratorParse, "gtk_accelerator_parse"},

	{&SimpleActionGroupNew, "g_simple_action_group_new"},
	{&SimpleActionNew, "g_simple_action_new"},
	{&SimpleActionSetEnabled, "g_simple_action_set_enabled"},
	{&SimpleActionNewStateful, "g_simple_action_new_stateful"},
	{&SimpleActionSetState, "g_simple_action_set_state"},
	{&ActionMapAddAction, "g_action_map_add_action"},
	{&ActionMapRemoveAction, "g_action_map_remove_action"},
	{&ActionMapLookupAction, "g_action_map_lookup_action"},
	{&MenuNew, "g_menu_new"},
	{&MenuAppend, "g_menu_append"},
	{&MenuAppendSection, "g_menu_append_section"},
	{&MenuAppendSubmenu, "g_menu_append_submenu"},
	{&MenuAppendItem, "g_menu_append_item"},
	{&MenuItemNew, "g_menu_item_new"},
	{&MenuItemSetAttributeValue, "g_menu_item_set_attribute_value"},
	{&MenuItemSetIcon, "g_menu_item_set_icon"},
	{&MenuItemNewSubmenu, "g_menu_item_new_submenu"},
	{&VariantNewString, "g_variant_new_string"},
	{&VariantNewBoolean, "g_variant_new_boolean"},
	{&VariantNewInt32, "g_variant_new_int32"},
	{&VariantGetInt32, "g_variant_get_int32"},
	{&VariantTypeNew, "g_variant_type_new"},
	{&ThemedIconNew, "g_themed_icon_new"},
	{&ApplicationSetAccelsForAction, "gtk_application_set_accels_for_action"},
	{&ApplicationSetMenubar, "gtk_application_set_menubar"},
	{&ApplicationWindowSetShowMenubar, "gtk_application_window_set_show_menubar"},
	{&PopoverMenuBarNewFromModel, "gtk_popover_menu_bar_new_from_model"},

	{&AdwInit, "adw_init"},
	{&AdwStyleManagerGetDefault, "adw_style_manager_get_default"},
	{&AdwStyleManagerGetDark, "adw_style_manager_get_dark"},
	{&AdwStyleManagerSetColorScheme, "adw_style_manager_set_color_scheme"},
	{&AdwStyleManagerGetHighContrast, "adw_style_manager_get_high_contrast"},
	{&AdwStyleManagerGetAccentColorRGBA, "adw_style_manager_get_accent_color_rgba"},
	{&AdwToolbarViewNew, "adw_toolbar_view_new"},
	{&AdwToolbarViewSetContent, "adw_toolbar_view_set_content"},
	{&AdwToolbarViewAddTopBar, "adw_toolbar_view_add_top_bar"},
	{&AdwToolbarViewAddBottomBar, "adw_toolbar_view_add_bottom_bar"},
	{&AdwToolbarViewRemove, "adw_toolbar_view_remove"},
	{&AdwToolbarViewSetRevealTopBars, "adw_toolbar_view_set_reveal_top_bars"},
	{&AdwToolbarViewSetTopBarStyle, "adw_toolbar_view_set_top_bar_style"},
	{&AdwToolbarViewGetTopBarHeight, "adw_toolbar_view_get_top_bar_height"},
	{&AdwToolbarViewGetBottomBarHeight, "adw_toolbar_view_get_bottom_bar_height"},
	{&AdwToolbarViewSetExtendContentToTopEdge, "adw_toolbar_view_set_extend_content_to_top_edge"},
	{&ActionBarNew, "gtk_action_bar_new"},
	{&ActionBarPackStart, "gtk_action_bar_pack_start"},
	{&ActionBarPackEnd, "gtk_action_bar_pack_end"},
	{&ActionBarRemove, "gtk_action_bar_remove"},
	{&ActionBarSetRevealed, "gtk_action_bar_set_revealed"},
	{&AdwHeaderBarNew, "adw_header_bar_new"},
	{&AdwHeaderBarPackStart, "adw_header_bar_pack_start"},
	{&AdwHeaderBarPackEnd, "adw_header_bar_pack_end"},
	{&AdwHeaderBarRemove, "adw_header_bar_remove"},
	{&AdwHeaderBarSetTitleWidget, "adw_header_bar_set_title_widget"},
	{&AdwHeaderBarSetShowStartTitleButtons, "adw_header_bar_set_show_start_title_buttons"},
	{&AdwHeaderBarSetShowEndTitleButtons, "adw_header_bar_set_show_end_title_buttons"},
	{&AdwHeaderBarSetShowBackButton, "adw_header_bar_set_show_back_button"},
	{&AdwHeaderBarSetShowTitle, "adw_header_bar_set_show_title"},
	{&AdwHeaderBarSetDecorationLayout, "adw_header_bar_set_decoration_layout"},
	{&AdwWindowTitleNew, "adw_window_title_new"},
	{&AdwWindowTitleSetTitle, "adw_window_title_set_title"},
	{&AdwNavigationViewNew, "adw_navigation_view_new"},
	{&AdwNavigationViewPush, "adw_navigation_view_push"},
	{&AdwNavigationViewPop, "adw_navigation_view_pop"},
	{&AdwNavigationViewPopToPage, "adw_navigation_view_pop_to_page"},
	{&AdwNavigationViewReplace, "adw_navigation_view_replace"},
	{&AdwNavigationViewAdd, "adw_navigation_view_add"},
	{&AdwNavigationViewRemove, "adw_navigation_view_remove"},
	{&AdwNavigationViewGetVisiblePage, "adw_navigation_view_get_visible_page"},
	{&AdwNavigationViewGetPreviousPage, "adw_navigation_view_get_previous_page"},
	{&AdwNavigationViewSetAnimateTransitions, "adw_navigation_view_set_animate_transitions"},
	{&AdwNavigationViewSetPopOnEscape, "adw_navigation_view_set_pop_on_escape"},
	{&AdwNavigationViewGetNavigationStack, "adw_navigation_view_get_navigation_stack"},
	{&AdwNavigationPageNew, "adw_navigation_page_new"},
	{&AdwNavigationPageSetTitle, "adw_navigation_page_set_title"},
	{&AdwNavigationPageSetCanPop, "adw_navigation_page_set_can_pop"},
	{&AdwNavigationPageSetChild, "adw_navigation_page_set_child"},
	{&AdwNavigationPageSetTag, "adw_navigation_page_set_tag"},
	{&AdwViewStackNew, "adw_view_stack_new"},
	{&AdwViewStackAddTitledWithIcon, "adw_view_stack_add_titled_with_icon"},
	{&AdwViewStackRemove, "adw_view_stack_remove"},
	{&AdwViewStackSetVisibleChild, "adw_view_stack_set_visible_child"},
	{&AdwViewStackGetVisibleChild, "adw_view_stack_get_visible_child"},
	{&AdwViewStackSetVisibleChildName, "adw_view_stack_set_visible_child_name"},
	{&AdwViewStackPageSetTitle, "adw_view_stack_page_set_title"},
	{&AdwViewStackPageSetIconName, "adw_view_stack_page_set_icon_name"},
	{&AdwViewStackPageSetBadgeNumber, "adw_view_stack_page_set_badge_number"},
	{&AdwViewStackPageSetNeedsAttention, "adw_view_stack_page_set_needs_attention"},
	{&AdwViewStackGetPage, "adw_view_stack_get_page"},
	{&AdwViewSwitcherNew, "adw_view_switcher_new"},
	{&AdwViewSwitcherSetStack, "adw_view_switcher_set_stack"},
	{&AdwViewSwitcherSetPolicy, "adw_view_switcher_set_policy"},
	{&AdwViewSwitcherBarNew, "adw_view_switcher_bar_new"},
	{&AdwViewSwitcherBarSetStack, "adw_view_switcher_bar_set_stack"},
	{&AdwViewSwitcherBarSetReveal, "adw_view_switcher_bar_set_reveal"},
	{&AdwDialogNew, "adw_dialog_new"},
	{&AdwDialogSetChild, "adw_dialog_set_child"},
	{&AdwDialogSetTitle, "adw_dialog_set_title"},
	{&AdwDialogSetContentWidth, "adw_dialog_set_content_width"},
	{&AdwDialogSetContentHeight, "adw_dialog_set_content_height"},
	{&AdwDialogSetFollowsContentSize, "adw_dialog_set_follows_content_size"},
	{&AdwDialogSetPresentationMode, "adw_dialog_set_presentation_mode"},
	{&AdwDialogSetCanClose, "adw_dialog_set_can_close"},
	{&AdwDialogGetCanClose, "adw_dialog_get_can_close"},
	{&AdwDialogGetPresentationMode, "adw_dialog_get_presentation_mode"},
	{&AdwDialogPresent, "adw_dialog_present"},
	{&AdwDialogClose, "adw_dialog_close"},
	{&AdwDialogForceClose, "adw_dialog_force_close"},
	{&AdwAlertDialogNew, "adw_alert_dialog_new"},
	{&AdwAlertDialogAddResponse, "adw_alert_dialog_add_response"},
	{&AdwAlertDialogSetResponseAppearance, "adw_alert_dialog_set_response_appearance"},
	{&AdwAlertDialogSetDefaultResponse, "adw_alert_dialog_set_default_response"},
	{&AdwAlertDialogSetCloseResponse, "adw_alert_dialog_set_close_response"},
	{&AdwAlertDialogSetPreferWideLayout, "adw_alert_dialog_set_prefer_wide_layout"},
	{&AdwAlertDialogSetResponseEnabled, "adw_alert_dialog_set_response_enabled"},
	{&AdwAlertDialogSetResponseLabel, "adw_alert_dialog_set_response_label"},
	{&AdwToastOverlayNew, "adw_toast_overlay_new"},
	{&AdwToastOverlaySetChild, "adw_toast_overlay_set_child"},
	{&AdwToastOverlayAddToast, "adw_toast_overlay_add_toast"},
	{&AdwToastNew, "adw_toast_new"},
	{&AdwBinNew, "adw_bin_new"},
	{&AdwBinSetChild, "adw_bin_set_child"},
	{&AdwOverlaySplitViewNew, "adw_overlay_split_view_new"},
	{&AdwNavigationSplitViewNew, "adw_navigation_split_view_new"},
	{&AdwNavigationSplitViewSetSidebar, "adw_navigation_split_view_set_sidebar"},
	{&AdwNavigationSplitViewSetContent, "adw_navigation_split_view_set_content"},
	{&AdwNavigationSplitViewSetCollapsed, "adw_navigation_split_view_set_collapsed"},
	{&AdwNavigationSplitViewSetShowContent, "adw_navigation_split_view_set_show_content"},
	{&AdwNavigationSplitViewSetMinSidebarWidth, "adw_navigation_split_view_set_min_sidebar_width"},
	{&AdwNavigationSplitViewSetMaxSidebarWidth, "adw_navigation_split_view_set_max_sidebar_width"},
	{&AdwBreakpointNew, "adw_breakpoint_new"},
	{&AdwBreakpointConditionParse, "adw_breakpoint_condition_parse"},
	{&AdwBreakpointAddSetter, "adw_breakpoint_add_setter"},
	{&AdwApplicationWindowAddBreakpoint, "adw_application_window_add_breakpoint"},
	{&AdwSpinRowNew, "adw_spin_row_new"},

	{&AccessibleGetATContext, "gtk_accessible_get_at_context"},
}

func bind() {
	for _, r := range raws {
		*r.p = Sym(r.name)
	}
	for _, b := range bindings {
		fn(b.p, b.name)
	}
	initSignals()
	initSources()
}

var FnTypeClassRef, FnEnumGetValueByNick uintptr

// Bound by Load, as above.
var (
	ValueSetEnum              func(v *Value, e int32)
	FrameClockGetFrameTime    func(c Ptr) int64
	WidgetGetOpacity          func(w Ptr) float64
	WidgetSetDefaultDirection func(d int32)
	LayoutSetWrap             func(l Ptr, w int32)
	LayoutSetHeight           func(l Ptr, h int32)
	LayoutSetEllipsize        func(l Ptr, e int32)
	ClipboardReadTextAsync    func(c, cancellable, cb, data Ptr)
	ClipboardReadTextFinish   func(c, res Ptr, err *Ptr) Ptr
	UriLauncherNew            func(uri string) Ptr
	UriLauncherLaunch         func(l, parent, cancellable, cb, data Ptr)
	ApplicationInhibit        func(app, win Ptr, flags uint32, reason string) uint32
	ApplicationUninhibit      func(app Ptr, cookie uint32)
	AdwToastOverlaySetChild   func(o, child Ptr)
	AdwToastOverlayAddToast   func(o, toast Ptr)
	AdwToastNew               func(title string) Ptr
)

// Bound by Load, as above.
var (
	ListModelGetNItems            func(m Ptr) uint32
	ListModelGetItem              func(m Ptr, i uint32) Ptr
	ApplicationGetActionsForAccel func(app Ptr, accel string) Ptr
	StrfreeV                      func(v Ptr)
	ActionGroupActivateAction     func(g Ptr, name string, param Ptr)
	FileGetURI                    func(f Ptr) Ptr
	WindowGetToplevels            func() Ptr
)

func init() {
	bindings = append(bindings,
		binding{&ListModelGetNItems, "g_list_model_get_n_items"},
		binding{&ListModelGetItem, "g_list_model_get_item"},
		binding{&ApplicationGetActionsForAccel, "gtk_application_get_actions_for_accel"},
		binding{&StrfreeV, "g_strfreev"},
		binding{&ActionGroupActivateAction, "g_action_group_activate_action"},
		binding{&FileGetURI, "g_file_get_uri"},
		binding{&WindowGetToplevels, "gtk_window_get_toplevels"},
	)
}
