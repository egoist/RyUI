package ops

// A SplitView's sidebar as the core sends it (Split's Items), read by the
// Go hosts with DecodeSidebar (the Swift, Kotlin and JavaScript hosts
// with their own readers of the same encoding).
//
// It's u32 section count, then per section a u32-length title (empty:
// none) and u32 item count, then per item a u32-length title, icon (an
// SF Symbol name; empty: none) and badge (empty: none). It's set again
// whenever any of them changes; while the sections' and their items'
// counts stay, a host updates the rows in place (SameSidebarShape)
// rather than building its list again, so a badge counting up neither
// moves the user's focus nor scrolls the sidebar.

// SidebarSection is a titled group of a sidebar's destinations.
type SidebarSection struct {
	Title string
	Items []SidebarItem
}

// SidebarItem is a destination in a sidebar.
type SidebarItem struct {
	Title, Icon string
	// Badge is shown at the row's trailing end: usually a count, any
	// short text (BadgeNumber).
	Badge string
}

// AppendSidebar appends a sidebar's sections.
func AppendSidebar(b []byte, sections []SidebarSection) []byte {
	b = le.AppendUint32(b, uint32(len(sections)))
	for i := range sections {
		s := &sections[i]
		b = appendStr(b, s.Title)
		b = le.AppendUint32(b, uint32(len(s.Items)))
		for j := range s.Items {
			it := &s.Items[j]
			b = appendStr(b, it.Title)
			b = appendStr(b, it.Icon)
			b = appendStr(b, it.Badge)
		}
	}
	return b
}

// DecodeSidebar reads what AppendSidebar writes; a short one ends where
// it's cut, with the items read whole.
func DecodeSidebar(b []byte) []SidebarSection {
	if len(b) == 0 {
		return nil
	}
	r := &reader{b: b}
	count := r.u32()
	var out []SidebarSection
	for i := uint32(0); i < count && r.err == nil; i++ {
		s := SidebarSection{Title: string(r.bytes())}
		n := r.u32()
		if r.err != nil {
			break
		}
		for j := uint32(0); j < n; j++ {
			it := SidebarItem{Title: string(r.bytes()), Icon: string(r.bytes()), Badge: string(r.bytes())}
			if r.err != nil {
				break
			}
			s.Items = append(s.Items, it)
		}
		out = append(out, s)
	}
	return out
}

// SameSidebarShape reports whether a and b have as many sections, each
// as many items: a host updates its rows in place, not building them
// again.
func SameSidebarShape(a, b []SidebarSection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i].Items) != len(b[i].Items) {
			return false
		}
	}
	return true
}

// BadgeNumber reports the count a badge says, if it's only ASCII digits
// (at most nine of them): a host shows a count in its platform's way
// (BadgeDrawable's number, AdwViewStackPage's badge-number) and a screen
// reader says it as so many items; other text is shown as it is.
func BadgeNumber(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
