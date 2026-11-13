package l10n

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/asuka/internal/i18ngen"
)

// Every translation has every word, as `asuka generate` checks an app's.
func TestCatalogs(t *testing.T) {
	c, diags, err := i18ngen.Load(FS(), i18ngen.Options{Dir: "messages", Source: "en", Namespace: Namespace})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Error(d)
	}
	names, _ := fs.Glob(FS(), "*.json")
	if len(c.Languages) != len(names) {
		t.Errorf("%d languages of %d files", len(c.Languages), len(names))
	}
}

func TestWords(t *testing.T) {
	for _, c := range []struct{ locale, ok, search, back string }{
		{"en", "OK", "Search", "Back"},
		{"de-AT", "OK", "Suchen", "Zurück"},
		{"zh-TW", "好", "搜尋", "返回"},
		{"pt-PT", "OK", "Pesquisar", "Voltar"},
		{"pt-BR", "OK", "Buscar", "Voltar"},
		{"sw", "OK", "Search", "Back"}, // not translated: English
		{"", "OK", "Search", "Back"},
	} {
		if OK(c.locale) != c.ok || Search(c.locale) != c.search || Back(c.locale) != c.back {
			t.Errorf("%q: %q %q %q", c.locale, OK(c.locale), Search(c.locale), Back(c.locale))
		}
	}
	if a := testing.AllocsPerRun(100, func() { OK("de") }); a != 0 {
		t.Errorf("saying a word again allocates %v times", a)
	}
}

// The app menu's words take the app's name, wherever the language puts it.
func TestMenuWords(t *testing.T) {
	for _, c := range []struct{ locale, about, hide, quit, others, zoom string }{
		{"en", "About Notes", "Hide Notes", "Quit Notes", "Hide Others", "Zoom"},
		{"de", "Über „Notes“", "„Notes“ ausblenden", "„Notes“ beenden", "Andere ausblenden", "Zoomen"},
		{"ja", "Notesについて", "Notesを非表示", "Notesを終了", "ほかを非表示", "拡大/縮小"},
		{"sw", "About Notes", "Hide Notes", "Quit Notes", "Hide Others", "Zoom"}, // not translated: English
	} {
		got := []string{About(c.locale, "Notes"), Hide(c.locale, "Notes"), Quit(c.locale, "Notes"), HideOthers(c.locale), Zoom(c.locale)}
		want := []string{c.about, c.hide, c.quit, c.others, c.zoom}
		if !slices.Equal(got, want) {
			t.Errorf("%q: %q, want %q", c.locale, got, want)
		}
	}
	// Every language says the name it's given.
	names, _ := fs.Glob(FS(), "*.json")
	for _, name := range names {
		locale := strings.TrimSuffix(name, ".json")
		for _, s := range []string{About(locale, "Notes"), Hide(locale, "Notes"), Quit(locale, "Notes")} {
			if !strings.Contains(s, "Notes") {
				t.Errorf("%s: %q lost the app's name", locale, s)
			}
		}
	}
}
