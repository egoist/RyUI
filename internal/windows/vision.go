//go:build windows

package windows

import (
	"errors"
	"image"
	"unsafe"
)

// What's in an image, with WinRT: its text (Windows.Media.Ocr, in the
// languages whose OCR is installed) and faces
// (Windows.Media.FaceAnalysis), from pixels as a SoftwareBitmap. These
// wait for the operations: call them off the main thread, from a thread
// in COM's multithreaded apartment (WithCOM).

var (
	iidOcrEngineStatics       = NewGUID("5BFFA85A-3384-3540-9940-699120D428A8")
	iidOcrResult              = NewGUID("9BD235B2-175B-3D6A-92E2-388C206E2F63")
	iidLanguageFactory        = NewGUID("9B0252AC-0C27-44F8-B792-9793FB66C63E")
	iidSoftwareBitmapStatics  = NewGUID("DF0385DB-672F-4A9D-806E-C2442F343E86")
	iidFaceDetectorStatics    = NewGUID("BC042D67-9047-33F6-881B-6746C1B218B8")
	iidFaceDetector           = NewGUID("16B672DC-FE6F-3117-8D95-C3F04D51630C")
	iidDetectedFace           = NewGUID("8200D454-66BC-34DF-9410-E89400195414")
	iidBufferFactory          = NewGUID("71AF914D-C10F-484B-BC50-14BC623B3A27")
	iidBufferByteAccess       = NewGUID("905A0FEF-BC53-11DF-8C49-001E4FC686DA")
	iidOcrResultCompleted     = ParamIID("pinterface(" + asyncHandler + ";" + RuntimeClassSig("Windows.Media.Ocr.OcrResult", iidOcrResult) + ")")
	iidFaceDetectorCompleted  = ParamIID("pinterface(" + asyncHandler + ";" + RuntimeClassSig("Windows.Media.FaceAnalysis.FaceDetector", iidFaceDetector) + ")")
	iidDetectedFacesCompleted = ParamIID("pinterface(" + asyncHandler + ";pinterface({913337e9-11a1-4345-a3a2-4e7f956e222d};" +
		RuntimeClassSig("Windows.Media.FaceAnalysis.DetectedFace", iidDetectedFace) + "))")
)

// awaitOp waits for an IAsyncOperation (whose completed handler is of
// iid) and answers its result (a reference the caller releases), or 0.
func awaitOp(op uintptr, iid GUID) uintptr {
	done := make(chan uintptr, 1)
	h := NewDelegate(iid, func(op, status uintptr) {
		var r uintptr
		if status == 1 { // Completed
			ComCall(op, 8, uintptr(unsafe.Pointer(&r))) // GetResults
		}
		done <- r
	})
	if Failed(ComCall(op, 6, h)) { // put_Completed
		Release(h)
		return 0
	}
	Release(h)
	return <-done
}

// vectorItems are an IVectorView's (or IVector's) items (references the
// caller releases).
func vectorItems(v uintptr) []uintptr {
	var n uint32
	ComCall(v, 7, uintptr(unsafe.Pointer(&n))) // get_Size
	out := make([]uintptr, 0, n)
	for i := uint32(0); i < n; i++ {
		var x uintptr
		if !Failed(ComCall(v, 6, uintptr(i), uintptr(unsafe.Pointer(&x)))) && x != 0 { // GetAt
			out = append(out, x)
		}
	}
	return out
}

func releaseAll(xs []uintptr) {
	for _, x := range xs {
		Release(x)
	}
}

// hstringOf calls a getter of an HSTRING.
func hstringOf(obj uintptr, i int) string {
	var hs HString
	if Failed(ComCall(obj, i, uintptr(unsafe.Pointer(&hs)))) {
		return ""
	}
	defer hs.Delete()
	return hs.String()
}

// softwareBitmap makes a SoftwareBitmap of pixels (format: 87 BGRA8, 62
// Gray8).
func softwareBitmap(pix []byte, format, w, h int) (uintptr, error) {
	bf, err := ActivationFactory("Windows.Storage.Streams.Buffer", iidBufferFactory)
	if err != nil {
		return 0, err
	}
	defer Release(bf)
	var buf uintptr
	if Failed(ComCall(bf, 6, uintptr(len(pix)), uintptr(unsafe.Pointer(&buf)))) || buf == 0 { // Create
		return 0, errors.New("no buffer")
	}
	defer Release(buf)
	acc := QueryInterface(buf, &iidBufferByteAccess)
	if acc == 0 {
		return 0, errors.New("no buffer")
	}
	var p uintptr
	ComCall(acc, 3, uintptr(unsafe.Pointer(&p))) // Buffer
	Release(acc)
	copy(unsafe.Slice((*byte)(native(p)), len(pix)), pix)
	ComCall(buf, 8, uintptr(len(pix))) // put_Length
	st, err := ActivationFactory("Windows.Graphics.Imaging.SoftwareBitmap", iidSoftwareBitmapStatics)
	if err != nil {
		return 0, err
	}
	defer Release(st)
	var bmp uintptr
	if hr := ComCall(st, 9, buf, uintptr(format), uintptr(w), uintptr(h), uintptr(unsafe.Pointer(&bmp))); Failed(hr) || bmp == 0 { // CreateCopyFromBuffer
		return 0, errors.New("no bitmap")
	}
	return bmp, nil
}

// OCRWord is a word read, its box in the image's pixels.
type OCRWord struct {
	Text       string
	X, Y, W, H float64
}

// OCRLine is a line of text read.
type OCRLine struct {
	Text  string
	Words []OCRWord
}

// ErrNoOCR is a Windows without the OCR of any language asked for (or
// of the user's).
var ErrNoOCR = errors.New("no OCR language installed")

// OCRMaxSide is the longest side Windows's OCR reads.
func OCRMaxSide() int {
	st, err := ActivationFactory("Windows.Media.Ocr.OcrEngine", iidOcrEngineStatics)
	if err != nil {
		return 0
	}
	defer Release(st)
	var n uint32
	ComCall(st, 6, uintptr(unsafe.Pointer(&n))) // MaxImageDimension
	return int(n)
}

// OCR reads the text in img (no larger than OCRMaxSide) in the first of
// languages (BCP 47) that's installed, else the user's.
func OCR(img *image.NRGBA, languages []string) ([]OCRLine, error) {
	st, err := ActivationFactory("Windows.Media.Ocr.OcrEngine", iidOcrEngineStatics)
	if err != nil {
		return nil, err
	}
	defer Release(st)
	var engine uintptr
	if len(languages) > 0 {
		if lf, err := ActivationFactory("Windows.Globalization.Language", iidLanguageFactory); err == nil {
			for _, tag := range languages {
				hs := NewHString(tag)
				var lang uintptr
				ComCall(lf, 6, uintptr(hs), uintptr(unsafe.Pointer(&lang))) // CreateLanguage
				hs.Delete()
				if lang == 0 {
					continue
				}
				ComCall(st, 9, lang, uintptr(unsafe.Pointer(&engine))) // TryCreateFromLanguage
				Release(lang)
				if engine != 0 {
					break
				}
			}
			Release(lf)
		}
	}
	if engine == 0 {
		ComCall(st, 10, uintptr(unsafe.Pointer(&engine))) // TryCreateFromUserProfileLanguages
	}
	if engine == 0 {
		return nil, ErrNoOCR
	}
	defer Release(engine)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			s, d := row[x*4:], pix[(y*w+x)*4:]
			d[0], d[1], d[2], d[3] = s[2], s[1], s[0], 255
		}
	}
	bmp, err := softwareBitmap(pix, 87, w, h)
	if err != nil {
		return nil, err
	}
	defer Release(bmp)
	var op uintptr
	if Failed(ComCall(engine, 6, bmp, uintptr(unsafe.Pointer(&op)))) || op == 0 { // RecognizeAsync
		return nil, errors.New("OCR failed")
	}
	res := awaitOp(op, iidOcrResultCompleted)
	Release(op)
	if res == 0 {
		return nil, errors.New("OCR failed")
	}
	defer Release(res)
	var lines uintptr
	ComCall(res, 6, uintptr(unsafe.Pointer(&lines))) // get_Lines
	if lines == 0 {
		return nil, nil
	}
	defer Release(lines)
	var out []OCRLine
	ls := vectorItems(lines)
	defer releaseAll(ls)
	for _, l := range ls {
		line := OCRLine{Text: hstringOf(l, 7)}
		var words uintptr
		ComCall(l, 6, uintptr(unsafe.Pointer(&words)))
		if words != 0 {
			ws := vectorItems(words)
			for _, wd := range ws {
				var r [4]float32 // Rect: X, Y, Width, Height
				ComCall(wd, 6, uintptr(unsafe.Pointer(&r[0])))
				line.Words = append(line.Words, OCRWord{Text: hstringOf(wd, 7), X: float64(r[0]), Y: float64(r[1]), W: float64(r[2]), H: float64(r[3])})
			}
			releaseAll(ws)
			Release(words)
		}
		out = append(out, line)
	}
	return out, nil
}

// DetectFaces finds the faces in img: their boxes in its pixels. A
// Windows without the face detector finds none.
func DetectFaces(img *image.NRGBA) ([]image.Rectangle, error) {
	st, err := ActivationFactory("Windows.Media.FaceAnalysis.FaceDetector", iidFaceDetectorStatics)
	if err != nil {
		return nil, err
	}
	defer Release(st)
	var supported uint8
	if ComCall(st, 9, uintptr(unsafe.Pointer(&supported))); supported == 0 { // IsSupported
		return nil, nil
	}
	var op uintptr
	if Failed(ComCall(st, 6, uintptr(unsafe.Pointer(&op)))) || op == 0 { // CreateAsync
		return nil, errors.New("no face detector")
	}
	det := awaitOp(op, iidFaceDetectorCompleted)
	Release(op)
	if det == 0 {
		return nil, errors.New("no face detector")
	}
	defer Release(det)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	gray := make([]byte, w*h)
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			p := row[x*4:]
			gray[y*w+x] = uint8((299*int(p[0]) + 587*int(p[1]) + 114*int(p[2])) / 1000)
		}
	}
	bmp, err := softwareBitmap(gray, 62, w, h)
	if err != nil {
		return nil, err
	}
	defer Release(bmp)
	if Failed(ComCall(det, 6, bmp, uintptr(unsafe.Pointer(&op)))) || op == 0 { // DetectFacesAsync
		return nil, errors.New("face detection failed")
	}
	faces := awaitOp(op, iidDetectedFacesCompleted)
	Release(op)
	if faces == 0 {
		return nil, errors.New("face detection failed")
	}
	defer Release(faces)
	fs := vectorItems(faces)
	defer releaseAll(fs)
	var out []image.Rectangle
	for _, f := range fs {
		var b [4]uint32 // BitmapBounds: X, Y, Width, Height
		ComCall(f, 6, uintptr(unsafe.Pointer(&b[0])))
		out = append(out, image.Rect(int(b[0]), int(b[1]), int(b[0]+b[2]), int(b[1]+b[3])))
	}
	return out, nil
}
