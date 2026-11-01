// Package ops is the contract between RyUI's core and its hosts: batched
// mount operations going down, events coming up.
//
// The core appends ops to a Batch; a host applies a whole batch at once.
// In-process Go hosts (host/fake, desktop later) read Batch.Ops directly;
// mobile hosts read the binary encoding (Batch.Encode), decoded by code
// generated from schema.go.
//
// Binary encoding, little-endian:
//
//	batch:   u32 op count, then the ops
//	op:      u8 code, u32 id, then by code:
//	  Create   u16 kind
//	  Destroy  -
//	  Insert   u32 child, u32 index
//	  Remove   u32 child
//	  SetProp  u16 prop, u8 type, value
//	  SetFrame f32 x, y, w, h
//	  Listen   u16 event
//	  Unlisten u16 event
//	  Command  u16 command, u32 length, bytes
//	  SetRoot  -
//	value:   bool u8 | int i64 | float f64 | string u32 length + UTF-8 |
//	         color u64 | edges 4×f32 | enum i32 | bytes u32 length + bytes
//
//	event:   u32 id, u16 kind, then the payload:
//	  bool u8 | int i64 | float f64 | string u32 length + UTF-8 |
//	  point 2×f64 | environment (see Environment)
package ops

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// NodeID names a native view. The core makes them, from 1, and never
// reuses one in a process. Hosts make IDs for views they create
// themselves (list cells) with the high bit set.
type NodeID uint32

// HostIDBit marks NodeIDs made by a host.
const HostIDBit NodeID = 1 << 31

type (
	Kind      uint16
	PropID    uint16
	EventKind uint16
	CommandID uint16
)

// Code is an op's code.
type Code uint8

const (
	OpCreate Code = iota + 1
	OpDestroy
	OpInsert
	OpRemove
	OpSetProp
	OpSetFrame
	OpListen
	OpUnlisten
	OpCommand
	OpSetRoot
)

var codeNames = [...]string{"", "Create", "Destroy", "Insert", "Remove", "SetProp", "SetFrame", "Listen", "Unlisten", "Command", "SetRoot"}

func (c Code) String() string {
	if int(c) < len(codeNames) {
		return codeNames[c]
	}
	return fmt.Sprintf("Code(%d)", c)
}

// Color is 0 (the platform's default), a semantic color or RGBA.
//
// A semantic color's low 32 bits are the SemanticColor (bits 0-15) and
// its transparency, 255 minus its alpha (bits 24-31): 0, opaque.
type Color uint64

const (
	colorSemantic = 1 << 32
	colorRGBA     = 2 << 32
)

// SemanticColorValue is a color the host resolves (UIKit's semantic
// colors, Material 3 roles), so dark mode needs no app code.
func SemanticColorValue(c SemanticColor) Color { return Color(colorSemantic | uint64(c)) }

// RGBA is a color with 8-bit channels.
func RGBA(r, g, b, a uint8) Color {
	return Color(colorRGBA | uint64(r)<<24 | uint64(g)<<16 | uint64(b)<<8 | uint64(a))
}

// Semantic returns the semantic color, if c is one.
func (c Color) Semantic() (SemanticColor, bool) {
	if c>>32 == 1 {
		return SemanticColor(uint16(c)), true
	}
	return 0, false
}

// Alpha is c's opacity, from 0 to 255: an RGBA color's alpha, or the
// opacity a semantic color is drawn with (Opacity's); 255 for the
// platform's default.
func (c Color) Alpha() uint8 {
	switch c >> 32 {
	case 1:
		return 255 - uint8(c>>24)
	case 2:
		return uint8(c)
	}
	return 255
}

// Opacity returns c drawn at opacity (0 to 1) times its own, as
// SwiftUI's Color.opacity and Compose's Color.copy(alpha) do. A semantic
// color stays one, so it still follows dark mode and the theme: a tint
// of the accent, a faint separator. The platform's default (0) has none
// to change, and stays the default.
func (c Color) Opacity(opacity float32) Color {
	if c == 0 {
		return 0
	}
	a := uint8(float32(c.Alpha())*min(max(opacity, 0), 1) + 0.5)
	switch c >> 32 {
	case 1:
		return Color(colorSemantic | uint64(255-a)<<24 | uint64(uint16(c)))
	case 2:
		return c&^0xff | Color(a)
	}
	return c
}

// RGBA returns the channels, if c is an RGBA color.
func (c Color) RGBA() (r, g, b, a uint8, ok bool) {
	if c>>32 != 2 {
		return
	}
	v := uint32(c)
	return uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v), true
}

// Value is a prop's value.
type Value struct {
	Type  Type
	Int   int64 // bool (0/1), int, enum, color
	Float float64
	Str   string
	Bytes []byte
	Edges [4]float32 // top, right, bottom, left
}

func (v Value) String() string {
	switch v.Type {
	case TBool:
		return fmt.Sprint(v.Int != 0)
	case TInt, TEnum:
		return fmt.Sprint(v.Int)
	case TFloat:
		return fmt.Sprint(v.Float)
	case TString:
		return fmt.Sprintf("%q", v.Str)
	case TColor:
		return fmt.Sprintf("#%x", uint64(v.Int))
	case TEdges:
		return fmt.Sprint(v.Edges)
	case TBytes:
		return fmt.Sprintf("%x", v.Bytes)
	}
	return "?"
}

// Op is one mount operation.
type Op struct {
	Code  Code
	ID    NodeID
	Kind  Kind      // Create
	Prop  PropID    // SetProp
	Event EventKind // Listen, Unlisten
	Cmd   CommandID // Command
	Child NodeID    // Insert, Remove
	Index uint32    // Insert
	Value Value     // SetProp; Command args in Value.Bytes
	Frame [4]float32
}

func (op *Op) String() string {
	switch op.Code {
	case OpCreate:
		return fmt.Sprintf("Create %d %s", op.ID, op.Kind)
	case OpDestroy, OpSetRoot:
		return fmt.Sprintf("%s %d", op.Code, op.ID)
	case OpInsert:
		return fmt.Sprintf("Insert %d %d @%d", op.ID, op.Child, op.Index)
	case OpRemove:
		return fmt.Sprintf("Remove %d %d", op.ID, op.Child)
	case OpSetProp:
		return fmt.Sprintf("SetProp %d %s=%s", op.ID, PropName(op.Prop), op.Value)
	case OpSetFrame:
		return fmt.Sprintf("SetFrame %d %g,%g %gx%g", op.ID, op.Frame[0], op.Frame[1], op.Frame[2], op.Frame[3])
	case OpListen, OpUnlisten:
		return fmt.Sprintf("%s %d %s", op.Code, op.ID, op.Event)
	case OpCommand:
		return fmt.Sprintf("Command %d %s %x", op.ID, op.Cmd, op.Value.Bytes)
	}
	return op.Code.String()
}

// Batch is a list of ops applied together. Reuse it: Reset keeps its
// memory, so a steady-state batch allocates nothing.
type Batch struct {
	Ops   []Op
	arena []byte // command args and bytes values
	buf   []byte // encoding
}

// Len returns the number of ops.
func (b *Batch) Len() int { return len(b.Ops) }

// Reset empties b. Byte slices from earlier ops become invalid.
func (b *Batch) Reset() {
	clear(b.Ops) // drop string and slice references
	b.Ops = b.Ops[:0]
	b.arena = b.arena[:0]
}

func (b *Batch) add(op Op) { b.Ops = append(b.Ops, op) }

func (b *Batch) Create(id NodeID, k Kind) { b.add(Op{Code: OpCreate, ID: id, Kind: k}) }
func (b *Batch) Destroy(id NodeID)        { b.add(Op{Code: OpDestroy, ID: id}) }
func (b *Batch) Insert(parent, child NodeID, index int) {
	b.add(Op{Code: OpInsert, ID: parent, Child: child, Index: uint32(index)})
}
func (b *Batch) Remove(parent, child NodeID) { b.add(Op{Code: OpRemove, ID: parent, Child: child}) }
func (b *Batch) SetRoot(id NodeID)           { b.add(Op{Code: OpSetRoot, ID: id}) }
func (b *Batch) Listen(id NodeID, e EventKind) {
	b.add(Op{Code: OpListen, ID: id, Event: e})
}
func (b *Batch) Unlisten(id NodeID, e EventKind) {
	b.add(Op{Code: OpUnlisten, ID: id, Event: e})
}
func (b *Batch) SetFrame(id NodeID, x, y, w, h float32) {
	b.add(Op{Code: OpSetFrame, ID: id, Frame: [4]float32{x, y, w, h}})
}

func (b *Batch) set(id NodeID, p PropID, v Value) {
	b.add(Op{Code: OpSetProp, ID: id, Prop: p, Value: v})
}

func (b *Batch) SetBool(id NodeID, p PropID, v bool) {
	i := int64(0)
	if v {
		i = 1
	}
	b.set(id, p, Value{Type: TBool, Int: i})
}
func (b *Batch) SetInt(id NodeID, p PropID, v int64) { b.set(id, p, Value{Type: TInt, Int: v}) }
func (b *Batch) SetFloat(id NodeID, p PropID, v float64) {
	b.set(id, p, Value{Type: TFloat, Float: v})
}
func (b *Batch) SetString(id NodeID, p PropID, v string) {
	b.set(id, p, Value{Type: TString, Str: v})
}
func (b *Batch) SetColor(id NodeID, p PropID, c Color) {
	b.set(id, p, Value{Type: TColor, Int: int64(c)})
}
func (b *Batch) SetEnum(id NodeID, p PropID, v int32) {
	b.set(id, p, Value{Type: TEnum, Int: int64(v)})
}
func (b *Batch) SetEdges(id NodeID, p PropID, top, right, bottom, left float32) {
	b.set(id, p, Value{Type: TEdges, Edges: [4]float32{top, right, bottom, left}})
}

// SetBytes copies v into the batch.
func (b *Batch) SetBytes(id NodeID, p PropID, v []byte) {
	b.set(id, p, Value{Type: TBytes, Bytes: b.copyBytes(v)})
}

// Command appends a command; args is copied.
func (b *Batch) Command(id NodeID, c CommandID, args []byte) {
	b.add(Op{Code: OpCommand, ID: id, Cmd: c, Value: Value{Type: TBytes, Bytes: b.copyBytes(args)}})
}

// CommandArgs appends a command whose args the caller writes with f into
// the batch's memory, avoiding a copy.
func (b *Batch) CommandArgs(id NodeID, c CommandID, f func(dst []byte) []byte) {
	start := len(b.arena)
	b.arena = f(b.arena)
	args := b.arena[start:len(b.arena):len(b.arena)]
	b.add(Op{Code: OpCommand, ID: id, Cmd: c, Value: Value{Type: TBytes, Bytes: args}})
}

func (b *Batch) copyBytes(v []byte) []byte {
	start := len(b.arena)
	b.arena = append(b.arena, v...)
	return b.arena[start:len(b.arena):len(b.arena)]
}

// String lists the ops, one per line.
func (b *Batch) String() string {
	var s strings.Builder
	for i := range b.Ops {
		s.WriteString(b.Ops[i].String())
		s.WriteByte('\n')
	}
	return s.String()
}

var le = binary.LittleEndian

// Encode returns the binary encoding of b. The slice is reused by the
// next Encode.
func (b *Batch) Encode() []byte {
	buf := le.AppendUint32(b.buf[:0], uint32(len(b.Ops)))
	for i := range b.Ops {
		op := &b.Ops[i]
		buf = append(buf, byte(op.Code))
		buf = le.AppendUint32(buf, uint32(op.ID))
		switch op.Code {
		case OpCreate:
			buf = le.AppendUint16(buf, uint16(op.Kind))
		case OpInsert:
			buf = le.AppendUint32(buf, uint32(op.Child))
			buf = le.AppendUint32(buf, op.Index)
		case OpRemove:
			buf = le.AppendUint32(buf, uint32(op.Child))
		case OpSetProp:
			buf = le.AppendUint16(buf, uint16(op.Prop))
			buf = append(buf, byte(op.Value.Type))
			buf = appendValue(buf, &op.Value)
		case OpSetFrame:
			for _, f := range op.Frame {
				buf = le.AppendUint32(buf, math.Float32bits(f))
			}
		case OpListen, OpUnlisten:
			buf = le.AppendUint16(buf, uint16(op.Event))
		case OpCommand:
			buf = le.AppendUint16(buf, uint16(op.Cmd))
			buf = le.AppendUint32(buf, uint32(len(op.Value.Bytes)))
			buf = append(buf, op.Value.Bytes...)
		}
	}
	b.buf = buf
	return buf
}

func appendValue(buf []byte, v *Value) []byte {
	switch v.Type {
	case TBool:
		return append(buf, byte(v.Int))
	case TInt, TColor:
		return le.AppendUint64(buf, uint64(v.Int))
	case TFloat:
		return le.AppendUint64(buf, math.Float64bits(v.Float))
	case TString:
		buf = le.AppendUint32(buf, uint32(len(v.Str)))
		return append(buf, v.Str...)
	case TEdges:
		for _, f := range v.Edges {
			buf = le.AppendUint32(buf, math.Float32bits(f))
		}
		return buf
	case TEnum:
		return le.AppendUint32(buf, uint32(int32(v.Int)))
	case TBytes:
		buf = le.AppendUint32(buf, uint32(len(v.Bytes)))
		return append(buf, v.Bytes...)
	}
	panic("ops: bad value type")
}

var errShort = errors.New("ops: truncated batch")

// reader reads the binary encoding.
type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil || len(r.b) < n {
		r.err = errShort
		if n > 8 {
			return nil // a corrupt length mustn't allocate what it claims
		}
		return make([]byte, n)
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() uint8   { return r.take(1)[0] }
func (r *reader) u16() uint16 { return le.Uint16(r.take(2)) }
func (r *reader) u32() uint32 { return le.Uint32(r.take(4)) }
func (r *reader) u64() uint64 { return le.Uint64(r.take(8)) }
func (r *reader) f32() float32 {
	return math.Float32frombits(r.u32())
}
func (r *reader) f64() float64 { return math.Float64frombits(r.u64()) }
func (r *reader) bytes() []byte {
	n := r.u32()
	return r.take(int(n))
}

// Decode appends the ops encoded in data to b (for tests and transports).
func (b *Batch) Decode(data []byte) error {
	r := &reader{b: data}
	n := int(r.u32())
	for i := 0; i < n && r.err == nil; i++ {
		op := Op{Code: Code(r.u8()), ID: NodeID(r.u32())}
		switch op.Code {
		case OpCreate:
			op.Kind = Kind(r.u16())
		case OpDestroy, OpSetRoot:
		case OpInsert:
			op.Child = NodeID(r.u32())
			op.Index = r.u32()
		case OpRemove:
			op.Child = NodeID(r.u32())
		case OpSetProp:
			op.Prop = PropID(r.u16())
			op.Value = r.value(Type(r.u8()))
		case OpSetFrame:
			for j := range op.Frame {
				op.Frame[j] = r.f32()
			}
		case OpListen, OpUnlisten:
			op.Event = EventKind(r.u16())
		case OpCommand:
			op.Cmd = CommandID(r.u16())
			op.Value = Value{Type: TBytes, Bytes: b.copyBytes(r.bytes())}
		default:
			return fmt.Errorf("ops: bad op code %d", op.Code)
		}
		b.add(op)
	}
	if r.err == nil && len(r.b) != 0 {
		return fmt.Errorf("ops: %d bytes after the last op", len(r.b))
	}
	return r.err
}

func (r *reader) value(t Type) Value {
	v := Value{Type: t}
	switch t {
	case TBool:
		v.Int = int64(r.u8())
	case TInt, TColor:
		v.Int = int64(r.u64())
	case TFloat:
		v.Float = r.f64()
	case TString:
		v.Str = string(r.bytes())
	case TEdges:
		for i := range v.Edges {
			v.Edges[i] = r.f32()
		}
	case TEnum:
		v.Int = int64(int32(r.u32()))
	case TBytes:
		v.Bytes = append([]byte(nil), r.bytes()...)
	default:
		r.err = fmt.Errorf("ops: bad value type %d", t)
	}
	return v
}
