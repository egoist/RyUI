//go:build linux && !android

package gtk

// GTK's media: a GtkMediaFile (GStreamer's backend) played in a GtkVideo
// (with its controls) or a GtkPicture (without).
var (
	VideoNew                    func() Ptr
	VideoSetMediaStream         func(v, stream Ptr)
	MediaFileNewForFile         func(file Ptr) Ptr
	MediaStreamPlay             func(s Ptr)
	MediaStreamPause            func(s Ptr)
	MediaStreamGetPlaying       func(s Ptr) bool
	MediaStreamSeek             func(s Ptr, usec int64)
	MediaStreamSetMuted         func(s Ptr, muted bool)
	MediaStreamIsPrepared       func(s Ptr) bool
	MediaStreamGetEnded         func(s Ptr) bool
	MediaStreamGetError         func(s Ptr) Ptr
	PaintableGetIntrinsicWidth  func(p Ptr) int32
	PaintableGetIntrinsicHeight func(p Ptr) int32
)

func init() {
	bindings = append(bindings,
		binding{&VideoNew, "gtk_video_new"},
		binding{&VideoSetMediaStream, "gtk_video_set_media_stream"},
		binding{&MediaFileNewForFile, "gtk_media_file_new_for_file"},
		binding{&MediaStreamPlay, "gtk_media_stream_play"},
		binding{&MediaStreamPause, "gtk_media_stream_pause"},
		binding{&MediaStreamGetPlaying, "gtk_media_stream_get_playing"},
		binding{&MediaStreamSeek, "gtk_media_stream_seek"},
		binding{&MediaStreamSetMuted, "gtk_media_stream_set_muted"},
		binding{&MediaStreamIsPrepared, "gtk_media_stream_is_prepared"},
		binding{&MediaStreamGetEnded, "gtk_media_stream_get_ended"},
		binding{&MediaStreamGetError, "gtk_media_stream_get_error"},
		binding{&PaintableGetIntrinsicWidth, "gdk_paintable_get_intrinsic_width"},
		binding{&PaintableGetIntrinsicHeight, "gdk_paintable_get_intrinsic_height"},
	)
}
