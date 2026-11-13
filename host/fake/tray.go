package fake

import (
	"encoding/binary"
	"errors"
	"sort"

	"github.com/egoist/asuka/ops"
)

// Tray is a desktop's tray and Dock menu, as the desktop hosts' "tray"
// module keeps them (package tray), for tests: Host.Tray installs it.
type Tray struct {
	// Icons are the icons shown, by id.
	Icons map[uint32]*TrayIcon
	// Dock is the Dock menu's items as last set; DockSets counts the
	// sets.
	Dock     []ops.MenuItem
	DockSets int
	// Sets and Removes count the calls.
	Sets, Removes int

	h       *Host
	waiting func([]byte, error)
	queue   [][]byte
}

// TrayIcon is a tray icon as the core last set it.
type TrayIcon struct {
	ID                                  uint32
	Template, Clickable                 bool
	Image, Symbol, Title, Tooltip, Name string
	Menu                                []ops.MenuItem
}

// Tray gives the host a tray (the "tray" module), as a desktop host's.
func (h *Host) Tray() *Tray {
	t := &Tray{Icons: map[uint32]*TrayIcon{}, h: h}
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["tray"] = t.call
	return t
}

func (t *Tray) call(method string, args []byte, reply func([]byte, error)) {
	le := binary.LittleEndian
	switch method {
	case "supported":
		reply([]byte{1}, nil)
	case "set":
		if len(args) < 5 {
			reply(nil, errors.New("bad args"))
			return
		}
		ic := &TrayIcon{ID: le.Uint32(args), Template: args[4]&1 != 0, Clickable: args[4]&2 != 0}
		b := args[5:]
		str := func() string {
			if len(b) < 4 || int(le.Uint32(b)) > len(b)-4 {
				b = nil
				return ""
			}
			n := int(le.Uint32(b))
			s := string(b[4 : 4+n])
			b = b[4+n:]
			return s
		}
		ic.Image, ic.Symbol, ic.Title, ic.Tooltip, ic.Name = str(), str(), str(), str(), str()
		ic.Menu = ops.DecodeMenu(b)
		t.Icons[ic.ID] = ic
		t.Sets++
		reply(nil, nil)
	case "remove":
		if len(args) >= 4 {
			delete(t.Icons, le.Uint32(args))
		}
		t.Removes++
		reply(nil, nil)
	case "dock":
		t.Dock = ops.DecodeMenu(args)
		t.DockSets++
		reply(nil, nil)
	case "next":
		if len(t.queue) > 0 {
			e := t.queue[0]
			t.queue = t.queue[1:]
			reply(e, nil)
			return
		}
		t.waiting = reply
	case "stop":
		if w := t.waiting; w != nil {
			t.waiting = nil
			w(nil, errors.New("stopped"))
		}
		reply(nil, nil)
	default:
		reply(nil, errors.New("no module method tray."+method))
	}
}

// Following reports whether the core waits for what happens ("next").
func (t *Tray) Following() bool { return t.waiting != nil }

// List returns the icons shown, by id.
func (t *Tray) List() []*TrayIcon {
	out := make([]*TrayIcon, 0, len(t.Icons))
	for _, ic := range t.Icons {
		out = append(out, ic)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Click clicks icon id, and runs what it posts.
func (t *Tray) Click(id uint32) { t.happen(id, 1, 0) }

// Choose chooses item index of icon id's menu (id 0: the Dock menu's), as
// ops.MenuItem.Index numbers them, and runs what it posts.
func (t *Tray) Choose(id uint32, index int) { t.happen(id, 2, index) }

func (t *Tray) happen(id uint32, kind byte, index int) {
	b := binary.LittleEndian.AppendUint32(nil, id)
	b = append(b, kind)
	b = binary.LittleEndian.AppendUint32(b, uint32(index))
	if w := t.waiting; w != nil {
		t.waiting = nil
		w(b, nil)
	} else {
		t.queue = append(t.queue, b)
	}
	t.h.RunPosted()
}
