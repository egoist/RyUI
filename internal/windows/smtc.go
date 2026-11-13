//go:build windows

package windows

import (
	"errors"
	"unsafe"
)

// The SystemMediaTransportControls of a window, as a desktop app gets
// them (ISystemMediaTransportControlsInterop::GetForWindow): the media
// controls of the volume flyout (Windows 10) or quick settings (Windows
// 11) and the lock screen, which the keyboard's media keys drive.

var (
	iidSMTCInterop       = NewGUID("DDB0472D-C911-4A1F-86D9-DC3D71A95F5A")
	iidSMTC              = NewGUID("99FA3FF4-1742-42A6-902E-087D41F965EC")
	iidSMTC2             = NewGUID("EA98D2F6-7F3C-4AF2-A586-72889808EFB1")
	iidMusicProps2       = NewGUID("00368462-97D3-44B9-B00F-008AFCEFAF18")
	iidTimelineProps     = NewGUID("5125316A-C3A2-475B-8507-93534DC88F15")
	iidRandomAccessStrm  = NewGUID("905A0FE1-BC53-11DF-8C49-001E4FC686DA")
	iidStreamRefStatics  = NewGUID("857309DC-3FBF-4E7D-986F-EF3B1A07A964")
	iidButtonPressedHdlr = NewGUID("0557E996-7B23-5BAE-AA81-EA0D671143A4") // TypedEventHandler<SystemMediaTransportControls, …ButtonPressedEventArgs>
	iidPositionHdlr      = NewGUID("44E34F15-BDC0-50A7-ACE4-39E91FB753F1") // TypedEventHandler<SystemMediaTransportControls, PlaybackPositionChangeRequestedEventArgs>

	procSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")
	procStreamOverStream  = systemDLL("shcore.dll").NewProc("CreateRandomAccessStreamOverStream")
	errNoMediaControls    = errors.New("not available: no SystemMediaTransportControls")
)

// Vtable indices (windows.media.h); IInspectable takes the first six.
const (
	smtcInteropGetForWindow = 6

	smtcPutPlaybackStatus       = 7
	smtcGetDisplayUpdater       = 8
	smtcPutIsEnabled            = 11
	smtcPutIsPlayEnabled        = 13
	smtcPutIsStopEnabled        = 15
	smtcPutIsPauseEnabled       = 17
	smtcPutIsFastForwardEnabled = 21
	smtcPutIsRewindEnabled      = 23
	smtcPutIsPreviousEnabled    = 25
	smtcPutIsNextEnabled        = 27
	smtcAddButtonPressed        = 32

	smtc2PutPlaybackRate                    = 11
	smtc2UpdateTimelineProperties           = 12
	smtc2AddPlaybackPositionChangeRequested = 13

	updaterPutType            = 7
	updaterPutThumbnail       = 11
	updaterGetMusicProperties = 12
	updaterClearAll           = 16
	updaterUpdate             = 17
	musicPutTitle             = 7
	musicPutArtist            = 11
	music2PutAlbumTitle       = 7
	timelinePutStartTime      = 7
	timelinePutEndTime        = 9
	timelinePutMinSeekTime    = 11
	timelinePutMaxSeekTime    = 13
	timelinePutPosition       = 15
	streamRefCreateFromStream = 8
	buttonArgsGetButton       = 6
	positionArgsGetRequested  = 6
)

// MediaStatus is a MediaPlaybackStatus.
type MediaStatus uintptr

const (
	MediaClosed  MediaStatus = 0
	MediaStopped MediaStatus = 2
	MediaPlaying MediaStatus = 3
	MediaPaused  MediaStatus = 4
)

// MediaButton is a SystemMediaTransportControlsButton.
type MediaButton int32

const (
	MediaButtonPlay        MediaButton = 0
	MediaButtonPause       MediaButton = 1
	MediaButtonStop        MediaButton = 2
	MediaButtonFastForward MediaButton = 4
	MediaButtonRewind      MediaButton = 5
	MediaButtonNext        MediaButton = 6
	MediaButtonPrevious    MediaButton = 7
)

// MediaControls are a window's controls.
type MediaControls struct {
	c, c2 uintptr // ISystemMediaTransportControls, and its second version (0 if none)
}

// MediaInfo is what the controls show.
type MediaInfo struct {
	Title, Artist, Album string
	// Thumbnail is the artwork's bytes (PNG, JPEG...), nil: none.
	Thumbnail []byte
	Status    MediaStatus
	// Duration and Position are in seconds; Rate is the playback rate.
	Duration, Position, Rate float64
	// The buttons enabled.
	Play, Pause, Stop, Next, Previous, FastForward, Rewind, Seek bool
}

// GetMediaControls gets hwnd's controls (a top-level window's): button
// gets the buttons pressed, seek the positions asked for (in seconds),
// on threads of the system's own: they must be safe on any thread.
func GetMediaControls(hwnd uintptr, button func(MediaButton), seek func(float64)) (*MediaControls, error) {
	if !WinRTAvailable() {
		return nil, errNoMediaControls
	}
	interop, err := ActivationFactory("Windows.Media.SystemMediaTransportControls", iidSMTCInterop)
	if err != nil {
		return nil, err
	}
	defer Release(interop)
	var c uintptr
	if hr := ComCall(interop, smtcInteropGetForWindow, hwnd, uintptr(unsafe.Pointer(&iidSMTC)), uintptr(unsafe.Pointer(&c))); Failed(hr) || c == 0 {
		return nil, errNoMediaControls
	}
	m := &MediaControls{c: c}
	var token int64
	h := NewDelegate(iidButtonPressedHdlr, func(_, args uintptr) {
		var b int32
		if !Failed(ComCall(args, buttonArgsGetButton, uintptr(unsafe.Pointer(&b)))) {
			button(MediaButton(b))
		}
	})
	ComCall(c, smtcAddButtonPressed, h, uintptr(unsafe.Pointer(&token)))
	Release(h)
	if c2 := QueryInterface(c, &iidSMTC2); c2 != 0 {
		h := NewDelegate(iidPositionHdlr, func(_, args uintptr) {
			var t int64 // a TimeSpan, in 100 ns
			if !Failed(ComCall(args, positionArgsGetRequested, uintptr(unsafe.Pointer(&t)))) {
				seek(float64(t) / 1e7)
			}
		})
		ComCall(c2, smtc2AddPlaybackPositionChangeRequested, h, uintptr(unsafe.Pointer(&token)))
		Release(h)
		m.c2 = c2
	}
	return m, nil
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// withHString passes a temporary HSTRING of s to f.
func withHString(s string, f func(h uintptr)) {
	h := NewHString(s)
	defer h.Delete()
	f(uintptr(h))
}

// Set shows info.
func (m *MediaControls) Set(info MediaInfo) {
	c := m.c
	ComCall(c, smtcPutIsEnabled, 1)
	ComCall(c, smtcPutPlaybackStatus, uintptr(info.Status))
	ComCall(c, smtcPutIsPlayEnabled, boolArg(info.Play))
	ComCall(c, smtcPutIsPauseEnabled, boolArg(info.Pause))
	ComCall(c, smtcPutIsStopEnabled, boolArg(info.Stop))
	ComCall(c, smtcPutIsNextEnabled, boolArg(info.Next))
	ComCall(c, smtcPutIsPreviousEnabled, boolArg(info.Previous))
	ComCall(c, smtcPutIsFastForwardEnabled, boolArg(info.FastForward))
	ComCall(c, smtcPutIsRewindEnabled, boolArg(info.Rewind))
	var updater uintptr
	if !Failed(ComCall(c, smtcGetDisplayUpdater, uintptr(unsafe.Pointer(&updater)))) && updater != 0 {
		const mediaPlaybackTypeMusic = 1
		ComCall(updater, updaterPutType, mediaPlaybackTypeMusic)
		var music uintptr
		if !Failed(ComCall(updater, updaterGetMusicProperties, uintptr(unsafe.Pointer(&music)))) && music != 0 {
			withHString(info.Title, func(h uintptr) { ComCall(music, musicPutTitle, h) })
			withHString(info.Artist, func(h uintptr) { ComCall(music, musicPutArtist, h) })
			if music2 := QueryInterface(music, &iidMusicProps2); music2 != 0 {
				withHString(info.Album, func(h uintptr) { ComCall(music2, music2PutAlbumTitle, h) })
				Release(music2)
			}
			Release(music)
		}
		thumb := streamReference(info.Thumbnail)
		ComCall(updater, updaterPutThumbnail, thumb)
		Release(thumb)
		ComCall(updater, updaterUpdate)
		Release(updater)
	}
	if m.c2 == 0 {
		return
	}
	if tl, err := ActivateInstance("Windows.Media.SystemMediaTransportControlsTimelineProperties"); err == nil {
		if t := QueryInterface(tl, &iidTimelineProps); t != 0 {
			// TimeSpans, in 100 ns, from a start of 0.
			span := func(s float64) uintptr { return uintptr(int64(s * 1e7)) }
			ComCall(t, timelinePutStartTime, 0)
			ComCall(t, timelinePutEndTime, span(info.Duration))
			ComCall(t, timelinePutMinSeekTime, 0)
			if info.Seek {
				ComCall(t, timelinePutMaxSeekTime, span(info.Duration))
			} else {
				ComCall(t, timelinePutMaxSeekTime, 0)
			}
			ComCall(t, timelinePutPosition, span(info.Position))
			ComCall(m.c2, smtc2UpdateTimelineProperties, t)
			Release(t)
		}
		Release(tl)
	}
	rate := info.Rate
	if rate <= 0 {
		rate = 1
	}
	ComCallDouble(m.c2, smtc2PutPlaybackRate, rate)
}

// MediaShown is what the controls have, as they report it (for tests).
type MediaShown struct {
	Status                         MediaStatus
	Enabled                        bool
	Play, Pause, Next, FastForward bool
	Title                          string
}

// Shown reads back what the controls have.
func (m *MediaControls) Shown() MediaShown {
	var s MediaShown
	get := func(i int) bool {
		var b uint8
		ComCall(m.c, i, uintptr(unsafe.Pointer(&b)))
		return b != 0
	}
	var status int32
	ComCall(m.c, smtcPutPlaybackStatus-1, uintptr(unsafe.Pointer(&status)))
	s.Status = MediaStatus(status)
	s.Enabled = get(smtcPutIsEnabled - 1)
	s.Play = get(smtcPutIsPlayEnabled - 1)
	s.Pause = get(smtcPutIsPauseEnabled - 1)
	s.Next = get(smtcPutIsNextEnabled - 1)
	s.FastForward = get(smtcPutIsFastForwardEnabled - 1)
	var updater uintptr
	if !Failed(ComCall(m.c, smtcGetDisplayUpdater, uintptr(unsafe.Pointer(&updater)))) && updater != 0 {
		var music uintptr
		if !Failed(ComCall(updater, updaterGetMusicProperties, uintptr(unsafe.Pointer(&music)))) && music != 0 {
			var h uintptr
			ComCall(music, musicPutTitle-1, uintptr(unsafe.Pointer(&h)))
			s.Title = HString(h).String()
			HString(h).Delete()
			Release(music)
		}
		Release(updater)
	}
	return s
}

// Clear takes what shows away: the controls close.
func (m *MediaControls) Clear() {
	var updater uintptr
	if !Failed(ComCall(m.c, smtcGetDisplayUpdater, uintptr(unsafe.Pointer(&updater)))) && updater != 0 {
		ComCall(updater, updaterClearAll)
		ComCall(updater, updaterUpdate)
		Release(updater)
	}
	ComCall(m.c, smtcPutPlaybackStatus, uintptr(MediaClosed))
	ComCall(m.c, smtcPutIsEnabled, 0)
}

// streamReference is a RandomAccessStreamReference of an image's bytes
// (a reference the caller releases), or 0.
func streamReference(img []byte) uintptr {
	if len(img) == 0 || !Has(procSHCreateMemStream) || !Has(procStreamOverStream) {
		return 0
	}
	stream, _, _ := procSHCreateMemStream.Call(uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)))
	if stream == 0 {
		return 0
	}
	defer Release(stream)
	var ras uintptr
	const bsosDefault = 0
	if hr, _, _ := procStreamOverStream.Call(stream, bsosDefault, uintptr(unsafe.Pointer(&iidRandomAccessStrm)), uintptr(unsafe.Pointer(&ras))); Failed(hr) || ras == 0 {
		return 0
	}
	defer Release(ras)
	statics, err := ActivationFactory("Windows.Storage.Streams.RandomAccessStreamReference", iidStreamRefStatics)
	if err != nil {
		return 0
	}
	defer Release(statics)
	var ref uintptr
	if Failed(ComCall(statics, streamRefCreateFromStream, ras, uintptr(unsafe.Pointer(&ref)))) {
		return 0
	}
	return ref
}
