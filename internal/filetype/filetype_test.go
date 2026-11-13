package filetype

import (
	"reflect"
	"strings"
	"testing"
)

// The types survive the -X value, which has no quote or space in it.
func TestEncode(t *testing.T) {
	types := []Type{{Name: `Bob's "Notes"`, Extensions: []string{"note"}, Own: true, Icon: 101}, {Name: "Markdown", Extensions: []string{"md", "markdown"}}}
	s := Encode(types)
	if strings.ContainsAny(s, "\"' ") {
		t.Errorf("Encode = %q", s)
	}
	if got := Decode(s); !reflect.DeepEqual(got, types) {
		t.Errorf("Decode = %+v", got)
	}
	if Encode(nil) != "" || Decode("") != nil || Decode("!!") != nil {
		t.Error("none")
	}
}

// A ProgID is the app's ID and the extension, in letters, digits and
// dots, at most 39 characters (a long ID keeps its end).
func TestProgID(t *testing.T) {
	for id, want := range map[string]string{
		"com.example.notes":                              "com.example.notes.note",
		"dev.asuka.my-notes":                             "dev.asuka.mynotes.note",
		"com.example.a-very-long-application-identifier": "averylongapplicationidentifier.note",
	} {
		if got := ProgID(id, Type{Extensions: []string{"Note"}}); got != want || len(got) > 39 {
			t.Errorf("ProgID(%q) = %q, want %q", id, got, want)
		}
	}
}
