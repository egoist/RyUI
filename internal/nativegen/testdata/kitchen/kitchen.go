// Package kitchen declares a native module and a view with every kind of
// value that crosses: nativegen's tests generate its code on each side.
package kitchen

import (
	"context"
	"io"
	"time"

	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// ErrBusy is the kitchen's: it's cooking something else.
var ErrBusy = native.NewError("kitchen: busy")

// Unit is a weight's unit.
type Unit string

const (
	UnitGram  Unit = "g"
	UnitOunce Unit = "oz"
)

// Level is how hot.
type Level int32

const (
	LevelLow Level = iota
	LevelHigh
)

// Item is something in the kitchen.
type Item struct {
	Name   string
	Weight float64
	Unit   Unit
	Tags   []string
	Notes  map[string]string
	Best   *time.Time // when it's best before, if it says
	Next   *Item
	Data   []byte
	Took   time.Duration
	ID     uint64
	Count  int8
	Ratio  float32
	Fresh  bool
}

// Kitchen is the platform's kitchen.
//
//asuka:native
type Kitchen interface {
	// Name is the kitchen's name.
	Name() string
	Items(filter string) ([]Item, error)
	Add(item Item) error
	// Cook cooks a recipe, saying how far along it is.
	Cook(ctx context.Context, recipe string, progress func(done float64)) (Item, error)
	Ask(question func(q string) bool) (bool, error)
	Open(name string) (Oven, error)
	//asuka:mainthread
	Show() error
	Level() Level
	Counts() map[string]int64
	When(at time.Time, every time.Duration) (*time.Time, error)
}

// Oven is one of the kitchen's ovens.
//
//asuka:native
//asuka:mainthread
type Oven interface {
	io.Closer
	Heat(to float32, unit Unit) error
}

// DialProps are a Dial's: a value to pick, and what it says.
//
//asuka:view
type DialProps struct {
	Value   signal.Accessor[float64]
	Label   string
	Items   signal.Accessor[[]Item]
	Changed func(value float64)
	Picked  func(item Item, index int)
	Style   ui.Style
}
