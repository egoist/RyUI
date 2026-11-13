//go:build windows

package windows

import (
	"slices"
	"testing"
)

func TestFirstWeekday(t *testing.T) {
	// Windows' 0 is Monday; the environment's 1 is Sunday.
	for s, want := range map[string]uint8{"0": 2, "1": 3, "4": 6, "5": 7, "6": 1, "7": 0, "": 0, "x": 0} {
		if got := firstWeekday(s); got != want {
			t.Errorf("%q: %d, want %d", s, got, want)
		}
	}
}

func TestTimeFormatCycle(t *testing.T) {
	for s, want := range map[string]uint8{"HH:mm:ss": 24, "h:mm:ss tt": 12, "tt h:mm": 12, "'h'HH": 24, "": 0} {
		if got := timeFormatCycle(s); got != want {
			t.Errorf("%q: %d, want %d", s, got, want)
		}
	}
}

func TestSplitMultiString(t *testing.T) {
	buf := []uint16{'e', 'n', 0, 'd', 'e', 0, 0, 'x'}
	if got := splitMultiString(buf); !slices.Equal(got, []string{"en", "de"}) {
		t.Errorf("%q", got)
	}
}
