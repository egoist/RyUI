//go:build linux && !android

package gtk

// What host/linux's text calls, in Txt, a namespace of their own, bound
// by Load like the rest: Pango layouts' geometry (a rich text's links and inline
// components), text tags (a rich field's styles), the clipboard's content
// (a field's pastes of images and files).

var Txt struct {
	// Pango.
	LayoutXYToIndex                func(l Ptr, x, y int32, index, trailing *int32) bool
	LayoutGetIter                  func(l Ptr) Ptr
	LayoutIterNextChar             func(it Ptr) bool
	LayoutIterGetIndex             func(it Ptr) int32
	LayoutIterGetBaseline          func(it Ptr) int32
	LayoutIterGetCharExtents       func(it Ptr, r *PangoRectangle)
	LayoutIterFree                 func(it Ptr)
	LabelGetAttributes             func(l Ptr) Ptr
	WidgetSetCursor                func(w, c Ptr)
	WidgetSetMarginStart           func(w Ptr, m int32)
	WidgetSetMarginEnd             func(w Ptr, m int32)
	WidgetSetMarginTop             func(w Ptr, m int32)
	LabelGetLayoutOffsets          func(l Ptr, x, y *int32)
	FontDescriptionNew             func() Ptr
	FontDescriptionSetFamily       func(d Ptr, family string)
	FontDescriptionSetWeight       func(d Ptr, w int32)
	FontDescriptionSetStyle        func(d Ptr, s int32)
	FontDescriptionSetAbsoluteSize func(d Ptr, size float64)
	FontDescriptionGetType         func() GType
	UnderlineGetType               func() GType
	RGBAGetType                    func() GType
	ValueSetBoxed                  func(v *Value, p Ptr)
	ValueSetInt                    func(v *Value, i int32)

	// Text views, buffers and tags.
	TextTagNew                          func(name Ptr) Ptr
	TextBufferGetTagTable               func(b Ptr) Ptr
	TextTagTableAdd                     func(t, tag Ptr) bool
	TextTagSetPriority                  func(tag Ptr, p int32)
	TextBufferApplyTag                  func(b, tag Ptr, s, e *TextIter)
	TextBufferRemoveTag                 func(b, tag Ptr, s, e *TextIter)
	TextBufferInsert                    func(b Ptr, it *TextIter, text string, n int32)
	TextBufferInsertInteractiveAtCursor func(b Ptr, text string, n int32, editable bool) bool
	TextBufferBeginUserAction           func(b Ptr)
	TextBufferEndUserAction             func(b Ptr)
	TextBufferBeginIrreversibleAction   func(b Ptr)
	TextBufferEndIrreversibleAction     func(b Ptr)
	TextBufferGetSelectionBound         func(b Ptr) Ptr
	TextBufferGetInsert                 func(b Ptr) Ptr
	TextBufferGetIterAtMark             func(b Ptr, it *TextIter, mark Ptr)
	TextBufferGetCharCount              func(b Ptr) int32
	TextBufferGetSlice                  func(b Ptr, s, e *TextIter, hidden bool) Ptr
	TextIterGetTags                     func(it *TextIter) Ptr
	TextIterForwardToTagToggle          func(it *TextIter, tag Ptr) bool
	TextIterSetOffset                   func(it *TextIter, off int32)
	TextIterCompare                     func(a, b *TextIter) int32
	TextViewSetInputHints               func(v Ptr, h int32)
	TextViewSetInputPurpose             func(v Ptr, p int32)
	TextViewSetMonospace                func(v Ptr, m bool)
	SListFree                           func(l Ptr)

	// The clipboard's content.
	ClipboardGetFormats            func(c Ptr) Ptr
	ContentFormatsContainGType     func(f Ptr, t GType) bool
	ContentFormatsGetMimeTypes     func(f Ptr, n *uintptr) Ptr
	ClipboardReadTextureAsync      func(c, cancellable, cb, data Ptr)
	ClipboardReadTextureFinish     func(c, res Ptr, err *Ptr) Ptr
	ClipboardReadValueAsync        func(c Ptr, t GType, prio int32, cancellable, cb, data Ptr)
	ClipboardReadValueFinish       func(c, res Ptr, err *Ptr) Ptr
	ClipboardReadAsync             func(c, mimeTypes Ptr, prio int32, cancellable, cb, data Ptr)
	ClipboardReadFinish            func(c, res Ptr, mime *Ptr, err *Ptr) Ptr
	FileListGetType                func() GType
	FileListGetFiles               func(l Ptr) Ptr
	FileGetPath                    func(f Ptr) Ptr
	TextureGetType                 func() GType
	TextureSaveToPNGBytes          func(t Ptr) Ptr
	MemoryOutputStreamNewResizable func() Ptr
	OutputStreamSpliceAsync        func(out, in Ptr, flags uint32, prio int32, cancellable, cb, data Ptr)
	OutputStreamSpliceFinish       func(out, res Ptr, err *Ptr) int64
	MemoryOutputStreamStealAsBytes func(s Ptr) Ptr
}

func init() {
	bindings = append(bindings,
		binding{&Txt.LayoutXYToIndex, "pango_layout_xy_to_index"},
		binding{&Txt.LayoutGetIter, "pango_layout_get_iter"},
		binding{&Txt.LayoutIterNextChar, "pango_layout_iter_next_char"},
		binding{&Txt.LayoutIterGetIndex, "pango_layout_iter_get_index"},
		binding{&Txt.LayoutIterGetBaseline, "pango_layout_iter_get_baseline"},
		binding{&Txt.LayoutIterGetCharExtents, "pango_layout_iter_get_char_extents"},
		binding{&Txt.LayoutIterFree, "pango_layout_iter_free"},
		binding{&Txt.LabelGetLayoutOffsets, "gtk_label_get_layout_offsets"},
		binding{&Txt.LabelGetAttributes, "gtk_label_get_attributes"},
		binding{&Txt.WidgetSetCursor, "gtk_widget_set_cursor"},
		binding{&Txt.WidgetSetMarginStart, "gtk_widget_set_margin_start"},
		binding{&Txt.WidgetSetMarginEnd, "gtk_widget_set_margin_end"},
		binding{&Txt.WidgetSetMarginTop, "gtk_widget_set_margin_top"},
		binding{&Txt.FontDescriptionNew, "pango_font_description_new"},
		binding{&Txt.FontDescriptionSetFamily, "pango_font_description_set_family"},
		binding{&Txt.FontDescriptionSetWeight, "pango_font_description_set_weight"},
		binding{&Txt.FontDescriptionSetStyle, "pango_font_description_set_style"},
		binding{&Txt.FontDescriptionSetAbsoluteSize, "pango_font_description_set_absolute_size"},
		binding{&Txt.FontDescriptionGetType, "pango_font_description_get_type"},
		binding{&Txt.UnderlineGetType, "pango_underline_get_type"},
		binding{&Txt.RGBAGetType, "gdk_rgba_get_type"},
		binding{&Txt.ValueSetBoxed, "g_value_set_boxed"},
		binding{&Txt.ValueSetInt, "g_value_set_int"},

		binding{&Txt.TextTagNew, "gtk_text_tag_new"},
		binding{&Txt.TextBufferGetTagTable, "gtk_text_buffer_get_tag_table"},
		binding{&Txt.TextTagTableAdd, "gtk_text_tag_table_add"},
		binding{&Txt.TextTagSetPriority, "gtk_text_tag_set_priority"},
		binding{&Txt.TextBufferApplyTag, "gtk_text_buffer_apply_tag"},
		binding{&Txt.TextBufferRemoveTag, "gtk_text_buffer_remove_tag"},
		binding{&Txt.TextBufferInsert, "gtk_text_buffer_insert"},
		binding{&Txt.TextBufferInsertInteractiveAtCursor, "gtk_text_buffer_insert_interactive_at_cursor"},
		binding{&Txt.TextBufferBeginUserAction, "gtk_text_buffer_begin_user_action"},
		binding{&Txt.TextBufferEndUserAction, "gtk_text_buffer_end_user_action"},
		binding{&Txt.TextBufferBeginIrreversibleAction, "gtk_text_buffer_begin_irreversible_action"},
		binding{&Txt.TextBufferEndIrreversibleAction, "gtk_text_buffer_end_irreversible_action"},
		binding{&Txt.TextBufferGetInsert, "gtk_text_buffer_get_insert"},
		binding{&Txt.TextBufferGetSelectionBound, "gtk_text_buffer_get_selection_bound"},
		binding{&Txt.TextBufferGetIterAtMark, "gtk_text_buffer_get_iter_at_mark"},
		binding{&Txt.TextBufferGetCharCount, "gtk_text_buffer_get_char_count"},
		binding{&Txt.TextBufferGetSlice, "gtk_text_buffer_get_slice"},
		binding{&Txt.TextIterGetTags, "gtk_text_iter_get_tags"},
		binding{&Txt.TextIterForwardToTagToggle, "gtk_text_iter_forward_to_tag_toggle"},
		binding{&Txt.TextIterSetOffset, "gtk_text_iter_set_offset"},
		binding{&Txt.TextIterCompare, "gtk_text_iter_compare"},
		binding{&Txt.TextViewSetInputHints, "gtk_text_view_set_input_hints"},
		binding{&Txt.TextViewSetInputPurpose, "gtk_text_view_set_input_purpose"},
		binding{&Txt.TextViewSetMonospace, "gtk_text_view_set_monospace"},
		binding{&Txt.SListFree, "g_slist_free"},

		binding{&Txt.ClipboardGetFormats, "gdk_clipboard_get_formats"},
		binding{&Txt.ContentFormatsContainGType, "gdk_content_formats_contain_gtype"},
		binding{&Txt.ContentFormatsGetMimeTypes, "gdk_content_formats_get_mime_types"},
		binding{&Txt.ClipboardReadTextureAsync, "gdk_clipboard_read_texture_async"},
		binding{&Txt.ClipboardReadTextureFinish, "gdk_clipboard_read_texture_finish"},
		binding{&Txt.ClipboardReadValueAsync, "gdk_clipboard_read_value_async"},
		binding{&Txt.ClipboardReadValueFinish, "gdk_clipboard_read_value_finish"},
		binding{&Txt.ClipboardReadAsync, "gdk_clipboard_read_async"},
		binding{&Txt.ClipboardReadFinish, "gdk_clipboard_read_finish"},
		binding{&Txt.FileListGetType, "gdk_file_list_get_type"},
		binding{&Txt.FileListGetFiles, "gdk_file_list_get_files"},
		binding{&Txt.FileGetPath, "g_file_get_path"},
		binding{&Txt.TextureGetType, "gdk_texture_get_type"},
		binding{&Txt.TextureSaveToPNGBytes, "gdk_texture_save_to_png_bytes"},
		binding{&Txt.MemoryOutputStreamNewResizable, "g_memory_output_stream_new_resizable"},
		binding{&Txt.OutputStreamSpliceAsync, "g_output_stream_splice_async"},
		binding{&Txt.OutputStreamSpliceFinish, "g_output_stream_splice_finish"},
		binding{&Txt.MemoryOutputStreamStealAsBytes, "g_memory_output_stream_steal_as_bytes"},
	)
}
