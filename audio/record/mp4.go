package record

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

var adtsRates = [...]uint32{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

// ErrEmptyStream says a stream holds no whole frame: nothing was recorded.
var ErrEmptyStream = errors.New("record: nothing was recorded")

type adtsInfo struct {
	objectType, rateIndex, channels byte
	sizes                           []uint32
}

// scanADTS walks a stream's frames. A frame cut short (what a crash leaves) ends
// it; bytes that aren't a frame are skipped to the next sync word.
func scanADTS(r io.Reader, payload func(frame []byte) error) (adtsInfo, error) {
	var info adtsInfo
	br := bufio.NewReaderSize(r, 1<<16)
	head := make([]byte, 7)
	buf := make([]byte, 0, 8192)
	for {
		if _, err := io.ReadFull(br, head[:2]); err != nil {
			return info, nil
		}
		if head[0] != 0xFF || head[1]&0xF6 != 0xF0 {
			// Not a sync word: go on from the next byte.
			second := head[1]
			if second != 0xFF {
				continue
			}
			if err := br.UnreadByte(); err != nil {
				return info, err
			}
			continue
		}
		if _, err := io.ReadFull(br, head[2:7]); err != nil {
			return info, nil
		}
		protectionAbsent := head[1]&1 == 1
		object := head[2]>>6 + 1
		rate := head[2] >> 2 & 0xF
		channels := head[2]&1<<2 | head[3]>>6
		length := int(head[3]&3)<<11 | int(head[4])<<3 | int(head[5])>>5
		headLen := 7
		if !protectionAbsent {
			headLen = 9
		}
		if length < headLen || int(rate) >= len(adtsRates) {
			continue
		}
		if !protectionAbsent {
			if _, err := br.Discard(2); err != nil {
				return info, nil
			}
		}
		size := length - headLen
		if cap(buf) < size {
			buf = make([]byte, size)
		}
		buf = buf[:size]
		if _, err := io.ReadFull(br, buf); err != nil {
			return info, nil // cut short: the frames before it stand
		}
		if len(info.sizes) == 0 {
			info.objectType, info.rateIndex, info.channels = object, rate, channels
		}
		info.sizes = append(info.sizes, uint32(size))
		if payload != nil {
			if err := payload(buf); err != nil {
				return info, err
			}
		}
	}
}

func box(name string, parts ...[]byte) []byte {
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 8, n)
	binary.BigEndian.PutUint32(out, uint32(n))
	copy(out[4:], name)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u32s(vs ...uint32) []byte {
	out := make([]byte, 0, len(vs)*4)
	for _, v := range vs {
		out = binary.BigEndian.AppendUint32(out, v)
	}
	return out
}

const samplesPerFrame = 1024

// moovBox is the MP4 index for frames starting at mdatStart in the file.
func moovBox(info adtsInfo, mdatStart uint32) []byte {
	rate := adtsRates[info.rateIndex]
	frames := uint32(len(info.sizes))
	duration := frames * samplesPerFrame
	identity := u32s(0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000)

	mvhd := box("mvhd", u32s(0, 0, 0, rate, duration, 0x00010000), []byte{0x01, 0x00, 0, 0}, make([]byte, 8), identity, make([]byte, 24), u32s(2))
	tkhd := box("tkhd", u32s(3, 0, 0, 1, 0, duration), make([]byte, 8), []byte{0, 0, 0, 0, 0x01, 0x00, 0, 0}, identity, u32s(0, 0))
	mdhd := box("mdhd", u32s(0, 0, 0, rate, duration), []byte{0x55, 0xC4, 0, 0})
	hdlr := box("hdlr", u32s(0, 0), []byte("soun"), make([]byte, 12), []byte("SoundHandler\x00"))
	smhd := box("smhd", u32s(0, 0))
	dinf := box("dinf", box("dref", u32s(0, 1), box("url ", u32s(1))))

	// The decoder's config: 5 bits object type, 4 bits rate index, 4 bits channels.
	asc := []byte{info.objectType<<3 | info.rateIndex>>1, info.rateIndex<<7 | info.channels<<3}
	decoder := append([]byte{0x05, byte(len(asc))}, asc...)
	config := append([]byte{0x40, 0x15, 0, 0, 0}, u32s(0, 0)...)
	config = append(config, decoder...)
	dcd := append([]byte{0x04, byte(len(config))}, config...)
	es := append([]byte{0, 1, 0}, dcd...)
	es = append(es, 0x06, 1, 2)
	esds := box("esds", u32s(0), append([]byte{0x03, byte(len(es))}, es...))
	channels := uint16(info.channels)
	if channels == 0 {
		channels = 1
	}
	entry := make([]byte, 0, 28)
	entry = append(entry, make([]byte, 6)...)
	entry = binary.BigEndian.AppendUint16(entry, 1) // the data reference
	entry = append(entry, make([]byte, 8)...)
	entry = binary.BigEndian.AppendUint16(entry, channels)
	entry = binary.BigEndian.AppendUint16(entry, 16)
	entry = append(entry, make([]byte, 4)...)
	entry = binary.BigEndian.AppendUint32(entry, min(rate, 0xFFFF)<<16)
	stsd := box("stsd", u32s(0, 1), box("mp4a", entry, esds))

	stts := box("stts", u32s(0, 1, frames, samplesPerFrame))
	stsc := box("stsc", u32s(0, 1, 1, frames, 1))
	stsz := box("stsz", u32s(0, 0, frames), u32s(info.sizes...))
	stco := box("stco", u32s(0, 1, mdatStart))
	stbl := box("stbl", stsd, stts, stsc, stsz, stco)
	minf := box("minf", smhd, dinf, stbl)
	mdia := box("mdia", mdhd, hdlr, minf)
	return box("moov", mvhd, box("trak", tkhd, mdia))
}

// Finalize makes a stream that Options.Stream recorded into an .m4a at dst that
// seeks properly, atomically, and says how long it is. What a crash cut short
// is kept up to its last whole frame; a stream with no frames at all is
// ErrEmptyStream. It takes a moment for a long recording, and leaves the stream
// for the caller to remove: not on the main thread.
func Finalize(stream, dst string) (time.Duration, error) {
	in, err := os.Open(stream)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	info, err := scanADTS(in, nil)
	if err != nil {
		return 0, err
	}
	if len(info.sizes) == 0 {
		return 0, ErrEmptyStream
	}
	var total uint64
	for _, s := range info.sizes {
		total += uint64(s)
	}
	ftyp := box("ftyp", []byte("M4A "), u32s(0), []byte("M4A mp42isom"))
	// moov's size doesn't depend on where mdat starts: measure it, then write it.
	moovSize := len(moovBox(info, 0))
	mdatStart := uint64(len(ftyp)) + uint64(moovSize) + 8
	if mdatStart+total > 1<<32-1 {
		return 0, errors.New("record: too long for an MPEG-4 file with 32-bit offsets")
	}
	moov := moovBox(info, uint32(mdatStart))

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp)
	w := bufio.NewWriterSize(out, 1<<16)
	mdat := make([]byte, 8)
	binary.BigEndian.PutUint32(mdat, uint32(total+8))
	copy(mdat[4:], "mdat")
	for _, b := range [][]byte{ftyp, moov, mdat} {
		if _, err := w.Write(b); err != nil {
			out.Close()
			return 0, err
		}
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		out.Close()
		return 0, err
	}
	if _, err := scanADTS(in, func(f []byte) error { _, err := w.Write(f); return err }); err != nil {
		out.Close()
		return 0, err
	}
	if err := w.Flush(); err != nil {
		out.Close()
		return 0, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return 0, err
	}
	frames := int64(len(info.sizes))
	return time.Duration(frames*samplesPerFrame) * time.Second / time.Duration(adtsRates[info.rateIndex]), nil
}
