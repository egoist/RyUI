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
			Scale: 3, FontScale: 1.25, Dark: true, ReduceMotion: true, ScreenReader: true, Platform: PlatformIOS,
			WidthClass: SizeClassCompact, HeightClass: SizeClassExpanded, Locale: "en_US", TimeZone: "Asia/Shanghai",
			Languages: "en-US,zh-Hans", FirstWeekday: 2, HourCycle: 24,
		}},
		{ID: 11, Kind: EventPan, G: Gesture{State: GestureStateChanged, X: 10, Y: 20, TranslationX: 5, TranslationY: -3, VelocityX: 100, VelocityY: -50, Scale: 1, Rotation: 0.5}},
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

// goldenActions is a menu with every kind of item. The Swift, Kotlin and
// JavaScript hosts' tests decode testdata/actions.bin and must print
// testdata/actions.txt: per item, depth first, its number, depth, role,
// flags, title and icon, tab separated.
func goldenActions() []ActionItem {
	return []ActionItem{
		{Title: "Copy", Icon: "doc.on.doc"},
		{Title: "Sort by", Flags: MenuInline, Items: []ActionItem{
			{Title: "Name", Flags: MenuChecked},
			{Title: "Date", Flags: MenuDisabled | MenuChecked},
		}},
		{Flags: MenuInline, Items: []ActionItem{{Title: "Untitled section"}}},
		{Title: "Move to", Icon: "folder", Items: []ActionItem{
			{Title: "Inbox"},
			{Title: "Nested", Items: []ActionItem{{Title: "Archive"}}},
		}},
		{Title: "Löschen…", Role: AlertButtonRoleDestructive, Flags: MenuDisabled},
		{Title: "Cancel", Role: AlertButtonRoleCancel},
	}
}

func dumpActions(items []ActionItem) string {
	var b strings.Builder
	var walk func(items []ActionItem, depth int)
	walk = func(items []ActionItem, depth int) {
		for _, it := range items {
			fmt.Fprintf(&b, "%d\t%d\t%d\t%d\t%s\t%s\n", it.Index, depth, it.Role, it.Flags, it.Title, it.Icon)
			walk(it.Items, depth+1)
		}
	}
	walk(items, 0)
	return b.String()
}

// goldenSidebar is a sidebar with an untitled and a titled section, and
// items with and without icons and badges. The Swift, Kotlin and
// JavaScript hosts' tests decode testdata/sidebar.bin and must print
// testdata/sidebar.txt: per section a line "#", its title; per item its
// section, title, icon and badge, tab separated.
func goldenSidebar() []SidebarSection {
	return []SidebarSection{
		{Items: []SidebarItem{
			{Title: "Inbox", Icon: "tray", Badge: "3"},
			{Title: "Drafts", Icon: "doc"},
		}},
		{Title: "Mailboxes", Items: []SidebarItem{
			{Title: "Archiv", Badge: "Neu"},
			{Title: "Gelöscht", Icon: "trash", Badge: "1234"},
		}},
		{Title: "Empty"},
	}
}

func dumpSidebar(sections []SidebarSection) string {
	var b strings.Builder
	for i, s := range sections {
		fmt.Fprintf(&b, "#\t%s\n", s.Title)
		for _, it := range s.Items {
			fmt.Fprintf(&b, "%d\t%s\t%s\t%s\n", i, it.Title, it.Icon, it.Badge)
		}
	}
	return b.String()
}

// goldenAlertFields are an alert's fields with every option. The Swift,
// Kotlin and JavaScript hosts' tests decode testdata/alertfields.bin and
// must print testdata/alertfields.txt: per field its placeholder, text,
// flags, keyboard and content type, tab separated; and encode
// FieldChanged's payload for field 2, "Grüße", as
// testdata/fieldchanged.bin.
func goldenAlertFields() []AlertField {
	return []AlertField{
		{Placeholder: "Name", Text: "Untitled", ContentType: "name"},
		{Placeholder: "Passwort", Flags: AlertFieldSecure, ContentType: "password"},
		{Placeholder: "Email", Text: "a@b.c", Keyboard: KeyboardEmail},
		{},
	}
}

func dumpAlertFields(fs []AlertField) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%s\n", f.Placeholder, f.Text, f.Flags, f.Keyboard, f.ContentType)
	}
	return b.String()
}

func TestSidebar(t *testing.T) {
	want := goldenSidebar()
	b := AppendSidebar(nil, want)
	if got := dumpSidebar(DecodeSidebar(b)); got != dumpSidebar(want) {
		t.Fatalf("got\n%s", got)
	}
	// Cut short, it ends where it's cut, with whole items.
	cut := DecodeSidebar(b[:len(b)-18])
	if len(cut) != 2 || len(cut[1].Items) != 1 || cut[1].Items[0].Badge != "Neu" {
		t.Errorf("cut: %+v", cut)
	}
	if DecodeSidebar(nil) != nil {
		t.Error("nothing decoded to something")
	}
	same := goldenSidebar()
	same[0].Items[0].Badge = "4"
	if !SameSidebarShape(want, same) {
		t.Error("a badge changed the shape")
	}
	same[2].Items = append(same[2].Items, SidebarItem{Title: "Spam"})
	if SameSidebarShape(want, same) {
		t.Error("an item more kept the shape")
	}
	for s, want := range map[string]int{"0": 0, "3": 3, "1234": 1234, "999999999": 999999999} {
		if n, ok := BadgeNumber(s); !ok || n != want {
			t.Errorf("BadgeNumber(%q) = %d, %v", s, n, ok)
		}
	}
	for _, s := range []string{"", "New", "99+", "-1", "1.5", "٣", "1234567890"} {
		if _, ok := BadgeNumber(s); ok {
			t.Errorf("BadgeNumber(%q) is a number", s)
		}
	}
}

func TestAlertFields(t *testing.T) {
	fs := goldenAlertFields()
	b := AppendAlertFields(nil, fs)
	if got, want := dumpAlertFields(DecodeAlertFields(b)), dumpAlertFields(fs); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := DecodeAlertFields(b[:len(b)-3]); len(got) != 3 {
		t.Fatalf("a cut list decoded %d fields, want 3", len(got))
	}
	if DecodeAlertFields(nil) != nil {
		t.Fatal("no bytes decoded fields")
	}
	i, s, ok := DecodeFieldChanged(AppendFieldChanged(nil, 2, "Grüße"))
	if !ok || i != 2 || s != "Grüße" {
		t.Fatalf("FieldChanged decoded %d %q %v", i, s, ok)
	}
	if _, _, ok := DecodeFieldChanged([]byte{2, 0, 0, 0, 9, 0, 0, 0, 'x'}); ok {
		t.Fatal("a cut FieldChanged decoded")
	}
	items := []ActionItem{{Index: 0, Role: AlertButtonRoleCancel}, {Index: 1, Role: AlertButtonRoleDestructive}, {Index: 2}, {Index: 3}}
	if got := DefaultAlertAction(items); got != 2 {
		t.Fatalf("default action %d, want 2", got)
	}
	if got := DefaultAlertAction(items[:2]); got != -1 {
		t.Fatalf("default action %d, want -1", got)
	}
}

func TestGolden(t *testing.T) {
	b := goldenBatch()
	bin := b.Encode()
	txt := dump(b)
	evs := encodeEvents(goldenEvents())
	acts := goldenActions()
	files := map[string][]byte{"golden.bin": bin, "golden.txt": []byte(txt), "events.bin": evs,
		"actions.bin": AppendActions(nil, acts), "actions.txt": []byte(dumpActions(DecodeActions(AppendActions(nil, acts)))),
		"sidebar.bin": AppendSidebar(nil, goldenSidebar()), "sidebar.txt": []byte(dumpSidebar(goldenSidebar())),
		"alertfields.bin": AppendAlertFields(nil, goldenAlertFields()), "alertfields.txt": []byte(dumpAlertFields(goldenAlertFields())),
		"fieldchanged.bin": AppendFieldChanged(nil, 2, "Grüße")}
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
	// Opacity keeps a semantic color one, and multiplies.
	h := s.Opacity(0.5).Opacity(0.5)
	if v, ok := h.Semantic(); !ok || v != SemanticColorAccent || h.Alpha() != 64 {
		t.Fatalf("semantic at a quarter: %v %v %d", v, ok, h.Alpha())
	}
	if s.Opacity(1) != s || s.Alpha() != 255 {
		t.Fatal("opaque changed the encoding")
	}
	if _, _, _, a, _ := c.Opacity(0.5).RGBA(); a != 2 {
		t.Fatalf("RGBA at half: %d", a)
	}
	if Color(0).Opacity(0.5) != 0 {
		t.Fatal("the default took an opacity")
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

func TestSizeClassBreakpoints(t *testing.T) {
	for _, c := range []struct {
		dp   float32
		w, h SizeClass
	}{
		{0, SizeClassCompact, SizeClassCompact},
		{479, SizeClassCompact, SizeClassCompact},
		{480, SizeClassCompact, SizeClassMedium},
		{599, SizeClassCompact, SizeClassMedium},
		{600, SizeClassMedium, SizeClassMedium},
		{839, SizeClassMedium, SizeClassMedium},
		{840, SizeClassExpanded, SizeClassMedium},
		{900, SizeClassExpanded, SizeClassExpanded},
	} {
		if w, h := WidthClassOf(c.dp), HeightClassOf(c.dp); w != c.w || h != c.h {
			t.Errorf("%v dp: width %v height %v, want %v %v", c.dp, w, h, c.w, c.h)
		}
	}
}
