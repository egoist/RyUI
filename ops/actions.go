package ops

// Actions as the core sends them: a view's Menu, a List cell's
// SwipeActions and LeadingSwipeActions, a Screen's bar buttons
// (Actions) and an Alert's Buttons, read by every host with
// DecodeActions (the Swift, Kotlin and JavaScript hosts with their own
// readers of the same encoding).
//
// A menu is u32 count, then its items. An item is u8 AlertButtonRole, a
// u32-length title, a u32-length icon (an SF Symbol name; empty: none)
// and u8 MenuFlags (MenuDisabled, MenuChecked, MenuSubmenu,
// MenuInline); with MenuSubmenu, its items follow, a menu: a submenu,
// or with MenuInline a section shown in place.
//
// Items are numbered depth first, as a menu bar's are: a submenu or a
// section is an item, numbered before its own items. An Action event
// carries the chosen item's number.

// A Screen's Action events say which of its menus the item is in: its
// bar buttons' (Actions) carry the item's number, its Toolbar's
// ScreenToolbarActions + the number, and its Primary's
// ScreenPrimaryAction + the number (0: the primary action, its children
// after it). A menu's numbers stay under 1<<32: a count is a u32.
const (
	ScreenToolbarActions int64 = 1 << 32
	ScreenPrimaryAction  int64 = 2 << 32
)

// ActionItem is an item of an action menu.
type ActionItem struct {
	// Index is the item's number in its menu, depth first.
	Index int
	Role  AlertButtonRole
	Title string
	Icon  string
	Flags MenuFlags
	// Items are its submenu's or section's (MenuSubmenu).
	Items []ActionItem
}

// Is reports whether the item has all of flags.
func (it *ActionItem) Is(flags MenuFlags) bool { return it.Flags&flags == flags }

// AppendActions appends a menu of actions. Their Index is ignored: the
// position numbers them. An item with Items, or MenuInline, gets
// MenuSubmenu.
func AppendActions(b []byte, items []ActionItem) []byte {
	b = le.AppendUint32(b, uint32(len(items)))
	for i := range items {
		it := &items[i]
		flags := it.Flags &^ MenuSubmenu
		if len(it.Items) > 0 || flags&MenuInline != 0 {
			flags |= MenuSubmenu
		}
		b = append(b, byte(it.Role))
		b = appendStr(b, it.Title)
		b = appendStr(b, it.Icon)
		b = append(b, byte(flags))
		if flags&MenuSubmenu != 0 {
			b = AppendActions(b, it.Items)
		}
	}
	return b
}

// DecodeActions reads what AppendActions writes, numbering the items
// depth first; a short menu ends where it's cut.
func DecodeActions(b []byte) []ActionItem {
	if len(b) == 0 {
		return nil
	}
	r := &reader{b: b}
	n := 0
	return decodeActions(r, &n, 0)
}

func decodeActions(r *reader, n *int, depth int) []ActionItem {
	count := r.u32()
	var items []ActionItem
	for i := uint32(0); i < count && r.err == nil; i++ {
		it := ActionItem{Index: *n, Role: AlertButtonRole(r.u8())}
		it.Title = string(r.bytes())
		it.Icon = string(r.bytes())
		it.Flags = MenuFlags(r.u8())
		if r.err != nil {
			break
		}
		*n++
		if it.Flags&MenuSubmenu != 0 {
			if depth >= menuDepth {
				r.err = errShort
				break
			}
			it.Items = decodeActions(r, n, depth+1)
		}
		items = append(items, it)
	}
	return items
}

// FlattenActions lists items and their submenus' and sections' items
// depth first, so that an item's Index is its place in the list.
func FlattenActions(items []ActionItem) []ActionItem {
	var out []ActionItem
	var walk func([]ActionItem)
	walk = func(items []ActionItem) {
		for _, it := range items {
			out = append(out, it)
			walk(it.Items)
		}
	}
	walk(items)
	return out
}

// CountActions returns how many numbers a menu's items take: each item,
// and its submenu's or section's items.
func CountActions(items []ActionItem) int {
	n := 0
	for i := range items {
		n += 1 + CountActions(items[i].Items)
	}
	return n
}
