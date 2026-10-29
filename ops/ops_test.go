package ops

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// goldenBatch uses every op and value type. The Swift host's tests decode
// testdata/golden.bin and must print testdata/golden.txt.
func goldenBatch() *Batch {
	var b Batch
	b.Create(1, KindView)
	b.Create(2, KindText)
	b.SetString(2, PropTextText, "héllo, 世界")
	b.SetBool(2, PropHidden, true)
	b.SetInt(2, PropLineLimit, -3)
	b.SetFloat(2, PropFontSize, 17.5)
	b.SetColor(1, PropBackground, RGBA(0x11, 0x22, 0x33, 0xff))
	b.SetColor(2, PropColor, SemanticColorValue(SemanticColorSecondaryLabel))
	b.SetEnum(2, PropTextStyle, int32(TextStyleHeadline))
	b.SetEdges(1, PropBorderWidth, 1, 2, 3.25, 4)
	b.SetBytes(1, PropA11yLabel, []byte{0, 1, 0xfe})
	b.Insert(1, 2, 0)
	b.SetFrame(2, 0.5, 1, 320, 44.25)
	b.Listen(1, EventPress)
	b.Unlisten(1, EventPress)
	b.Command(1, CmdListUpdate, []byte{1, 2, 3})
	b.Remove(1, 2)
	b.Destroy(2)
	b.SetRoot(1)
	return &b
}

// dump prints ops in the language-neutral form the Swift tests print.
func dump(b *Batch) string {
	var s strings.Builder
	f32 := func(f float32) string { return fmt.Sprintf("%08x", math.Float32bits(f)) }
	for _, op := range b.Ops {
		switch op.Code {
		case OpCreate:
			fmt.Fprintf(&s, "create %d %d\n", op.ID, op.Kind)
		case OpDestroy:
			fmt.Fprintf(&s, "destroy %d\n", op.ID)
		case OpInsert:
			fmt.Fprintf(&s, "insert %d %d %d\n", op.ID, op.Child, op.Index)
		case OpRemove:
			fmt.Fprintf(&s, "remove %d %d\n", op.ID, op.Child)
		case OpSetProp:
			v := op.Value
			var val string
			switch v.Type {
			case TBool:
				val = fmt.Sprint(v.Int != 0)
			case TInt, TEnum:
				val = fmt.Sprint(v.Int)
			case TFloat:
				val = fmt.Sprintf("%016x", math.Float64bits(v.Float))
			case TString:
				val = fmt.Sprintf("%x", v.Str)
			case TColor:
				val = fmt.Sprintf("%016x", uint64(v.Int))
			case TEdges:
				val = f32(v.Edges[0]) + " " + f32(v.Edges[1]) + " " + f32(v.Edges[2]) + " " + f32(v.Edges[3])
			case TBytes:
				val = fmt.Sprintf("%x", v.Bytes)
			}
			fmt.Fprintf(&s, "set %d %d %d %s\n", op.ID, op.Prop, v.Type, val)
		case OpSetFrame:
			fmt.Fprintf(&s, "frame %d %s %s %s %s\n", op.ID, f32(op.Frame[0]), f32(op.Frame[1]), f32(op.Frame[2]), f32(op.Frame[3]))
		case OpListen:
			fmt.Fprintf(&s, "listen %d %d\n", op.ID, op.Event)
		case OpUnlisten:
			fmt.Fprintf(&s, "unlisten %d %d\n", op.ID, op.Event)
		case OpCommand:
			fmt.Fprintf(&s, "command %d %d %x\n", op.ID, op.Cmd, op.Value.Bytes)
		case OpSetRoot:
			fmt.Fprintf(&s, "root %d\n", op.ID)
		}
	}
	return s.String()
}

// goldenEvents uses every payload. The Swift tests encode the same events
// and must produce testdata/events.bin (each event prefixed by its u32
// length).
func goldenEvents() []Event {
	return []Event{
		{ID: 7, Kind: EventPress},
		{ID: 7, Kind: EventTextChanged, Str: "hé"},
		{ID: 8, Kind: EventToggled, Bool: true},
		{ID: 9, Kind: EventValueChanged, Float: 0.25},
		{ID: 10, Kind: EventScrolled, X: 1.5, Y: -2},
		{ID: 1, Kind: EventLifecycle, Int: int64(LifecycleBackground)},
		{ID: 1, Kind: EventEnvironment, Env: Environment{
			Width: 402, Height: 874, SafeTop: 62, SafeBottom: 34, Keyboard: 336,
			Scale: 3, FontScale: 1.25, Dark: true, Locale: "en_US", TimeZone: "Asia/Shanghai",
		}},
		{ID: 11, Kind: EventPan, G: Gesture{State: GestureStateChanged, X: 10, Y: 20, TranslationX: 5, TranslationY: -3, VelocityX: 100, VelocityY: -50, Scale: 1}},
		{ID: 12, Kind: EventNative, Bytes: []byte{1, 2, 3}},
	}
}

func encodeEvents(evs []Event) []byte {
	var out []byte
	for i := range evs {
		e := AppendEvent(nil, &evs[i])
		out = binary.LittleEndian.AppendUint32(out, uint32(len(e)))
		out = append(out, e...)
	}
	return out
}

func TestGolden(t *testing.T) {
	b := goldenBatch()
	bin := b.Encode()
	txt := dump(b)
	evs := encodeEvents(goldenEvents())
	files := map[string][]byte{"golden.bin": bin, "golden.txt": []byte(txt), "events.bin": evs}
	for name, want := range files {
		path := filepath.Join("testdata", name)
		if *update {
			os.MkdirAll("testdata", 0o755)
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./ops -update)", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale: run go test ./ops -update", path)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	b := goldenBatch()
	var d Batch
	if err := d.Decode(b.Encode()); err != nil {
		t.Fatal(err)
	}
	if got, want := dump(&d), dump(b); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if err := d.Decode(b.Encode()[:20]); err == nil {
		t.Fatal("truncated batch decoded")
	}
}

func TestEventRoundTrip(t *testing.T) {
	for _, e := range goldenEvents() {
		got, err := DecodeEvent(AppendEvent(nil, &e))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, e) {
			t.Fatalf("got %+v, want %+v", got, e)
		}
	}
}

func TestEncodeAllocatesNothing(t *testing.T) {
	var b Batch
	fill := func() {
		b.Reset()
		for i := 0; i < 50; i++ {
			b.SetString(NodeID(i), PropTextText, "some text")
			b.SetFrame(NodeID(i), 1, 2, 3, 4)
			b.Command(NodeID(i), CmdFocus, []byte{1, 2})
		}
		b.Encode()
	}
	fill()
	if a := testing.AllocsPerRun(100, fill); a != 0 {
		t.Fatalf("%v allocations per batch", a)
	}
}

func TestColor(t *testing.T) {
	c := RGBA(1, 2, 3, 4)
	if r, g, b, a, ok := c.RGBA(); !ok || r != 1 || g != 2 || b != 3 || a != 4 {
		t.Fatal("RGBA")
	}
	s := SemanticColorValue(SemanticColorAccent)
	if v, ok := s.Semantic(); !ok || v != SemanticColorAccent {
		t.Fatal("Semantic")
	}
	if _, ok := Color(0).Semantic(); ok {
		t.Fatal("zero is semantic")
	}
}

func TestSchemaNames(t *testing.T) {
	if KindPropName(KindButton, PropButtonTitle) != "Title" || PropName(PropBackground) != "Background" {
		t.Fatal("prop names")
	}
	if typ, ok := PropType(KindTextField, PropTextFieldPlaceholder); !ok || typ != TString {
		t.Fatal("prop type")
	}
	if KindList.String() != "List" || EventToggled.String() != "Toggled" || CmdListUpdate.String() != "ListUpdate" {
		t.Fatal("names")
	}
}
