package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
)

// shareTarget is ryui.json's shareTarget (expo-sharing's plugin options,
// for both platforms at once): true takes texts and links; an object
// says what else.
//
//	"shareTarget": {"types": ["text", "image/*", "video/*"], "max": 10}
type shareTarget struct {
	on bool
	// Types are what the app is offered for: "text" (texts and web
	// links), and files by MIME type ("image/*", "video/*", "audio/*",
	// "application/pdf", "*/*": any). Default: text.
	Types []string `json:"types"`
	// Max is how many files it takes at once (default 1). More than one:
	// Android's ACTION_SEND_MULTIPLE as well; iOS's activation rule's
	// counts.
	Max int `json:"max"`
}

func (s *shareTarget) UnmarshalJSON(b []byte) error {
	*s = shareTarget{}
	switch string(bytes.TrimSpace(b)) {
	case "null", "false":
		return nil
	case "true":
		s.on = true
	default:
		type plain shareTarget
		if err := json.Unmarshal(b, (*plain)(s)); err != nil {
			return errors.New("shareTarget: want true, or {\"types\": [...], \"max\": n}")
		}
		s.on = true
	}
	if len(s.Types) == 0 {
		s.Types = []string{"text"}
	}
	if s.Max == 0 {
		s.Max = 1
	}
	return nil
}

var mimeType = regexp.MustCompile(`^(\*/\*|[a-z0-9][a-z0-9.+-]*/(\*|[a-z0-9][a-z0-9.+-]*))$`)

func (s *shareTarget) check() error {
	if !s.on {
		return nil
	}
	for i, t := range s.Types {
		t = strings.ToLower(strings.TrimSpace(t))
		s.Types[i] = t
		if t != "text" && !mimeType.MatchString(t) {
			return fmt.Errorf("type %q (want \"text\", or a MIME type as \"image/*\" or \"application/pdf\")", t)
		}
	}
	if s.Max < 1 {
		return fmt.Errorf("max %d (want 1 or more)", s.Max)
	}
	return nil
}

// textShares is shareTarget: true.
var textShares = shareTarget{on: true, Types: []string{"text"}, Max: 1}

// files reports whether the app takes files, which on iOS come through
// an App Group (appGroup).
func (s shareTarget) files() bool {
	return s.on && slices.ContainsFunc(s.Types, func(t string) bool { return t != "text" })
}

// appGroup is the App Group the app shares with its extensions: the one
// the share extension leaves files in for the app, when it takes files;
// where an app with a share sheet keeps its data for both; where the app
// leaves its widgets' entries for the widget extension ("" if none). A
// device build's profiles must have it, the app's and the extensions'
// (the App Groups capability).
func (c *appConfig) appGroup() string {
	if !c.ShareTarget.files() && !(c.ShareTarget.on && c.shareSheet) && len(c.Widgets) == 0 {
		return ""
	}
	return "group." + c.ID
}

// intentFilters are the Android activity's filters: ACTION_SEND for each
// type, ACTION_SEND_MULTIPLE for the files' when it takes more than one.
func (s shareTarget) intentFilters() string {
	var b strings.Builder
	filter := func(action string, types []string) {
		if len(types) == 0 {
			return
		}
		fmt.Fprintf(&b, `            <intent-filter>
                <action android:name="android.intent.action.%s" />
                <category android:name="android.intent.category.DEFAULT" />
`, action)
		for _, t := range types {
			fmt.Fprintf(&b, "                <data android:mimeType=\"%s\" />\n", html.EscapeString(t))
		}
		b.WriteString("            </intent-filter>\n")
	}
	var single, multiple []string
	for _, t := range s.Types {
		if t == "text" {
			t = "text/plain"
		} else if s.Max > 1 {
			multiple = append(multiple, t)
		}
		if !slices.Contains(single, t) {
			single = append(single, t)
		}
	}
	filter("SEND", single)
	filter("SEND_MULTIPLE", multiple)
	return b.String()
}

// activationRule is the iOS extension's NSExtensionActivationRule: a
// dictionary of the keys Apple has for texts, links, images, movies and
// any attachment; a predicate on the attachments' types when it takes
// another (audio, a PDF), where max isn't kept.
func (s shareTarget) activationRule() any {
	rule := map[string]any{}
	dict := true
	for _, t := range s.Types {
		switch t {
		case "text":
			rule["NSExtensionActivationSupportsText"] = true
			rule["NSExtensionActivationSupportsWebURLWithMaxCount"] = 1
		case "image/*":
			rule["NSExtensionActivationSupportsImageWithMaxCount"] = s.Max
		case "video/*":
			rule["NSExtensionActivationSupportsMovieWithMaxCount"] = s.Max
		case "*/*":
			rule["NSExtensionActivationSupportsAttachmentsWithMaxCount"] = s.Max
			rule["NSExtensionActivationSupportsFileWithMaxCount"] = s.Max
			rule["NSExtensionActivationSupportsImageWithMaxCount"] = s.Max
			rule["NSExtensionActivationSupportsMovieWithMaxCount"] = s.Max
		default:
			dict = false
		}
	}
	if dict {
		return rule
	}
	var utis []string
	for _, t := range s.Types {
		for _, u := range utisFor(t) {
			if !slices.Contains(utis, u) {
				utis = append(utis, u)
			}
		}
	}
	conds := make([]string, len(utis))
	for i, u := range utis {
		conds[i] = fmt.Sprintf("ANY $a.registeredTypeIdentifiers UTI-CONFORMS-TO %q", u)
	}
	return "SUBQUERY(extensionItems, $e, SUBQUERY($e.attachments, $a, " + strings.Join(conds, " OR ") + ").@count >= 1).@count >= 1"
}

// utisFor are the Uniform Type Identifiers a share type is on iOS: the
// common MIME types' own, else their family's (an unknown image type is
// any image).
func utisFor(t string) []string {
	if t == "text" {
		return []string{"public.plain-text", "public.url"}
	}
	if u, ok := mimeUTIs[t]; ok {
		return []string{u}
	}
	family, _, _ := strings.Cut(t, "/")
	switch family {
	case "image":
		return []string{"public.image"}
	case "video":
		return []string{"public.movie"}
	case "audio":
		return []string{"public.audio"}
	case "text":
		return []string{"public.text"}
	}
	return []string{"public.data"}
}

var mimeUTIs = map[string]string{
	"*/*":                      "public.item",
	"image/*":                  "public.image",
	"video/*":                  "public.movie",
	"audio/*":                  "public.audio",
	"text/*":                   "public.text",
	"text/plain":               "public.plain-text",
	"text/html":                "public.html",
	"text/csv":                 "public.comma-separated-values-text",
	"text/calendar":            "public.calendar-event",
	"text/vcard":               "public.vcard",
	"application/pdf":          "com.adobe.pdf",
	"application/json":         "public.json",
	"application/zip":          "public.zip-archive",
	"application/epub+zip":     "org.idpf.epub-container",
	"application/rtf":          "public.rtf",
	"application/xml":          "public.xml",
	"image/jpeg":               "public.jpeg",
	"image/png":                "public.png",
	"image/gif":                "com.compuserve.gif",
	"image/heic":               "public.heic",
	"image/webp":               "org.webmproject.webp",
	"image/svg+xml":            "public.svg-image",
	"video/mp4":                "public.mpeg-4",
	"video/quicktime":          "com.apple.quicktime-movie",
	"audio/mpeg":               "public.mp3",
	"audio/mp4":                "public.mpeg-4-audio",
	"audio/x-m4a":              "com.apple.m4a-audio",
	"audio/wav":                "com.microsoft.waveform-audio",
	"audio/aac":                "public.aac-audio",
	"application/msword":       "com.microsoft.word.doc",
	"application/vnd.ms-excel": "com.microsoft.excel.xls",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "org.openxmlformats.wordprocessingml.document",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       "org.openxmlformats.spreadsheetml.sheet",
}

// keychainGroup is the keychain access group of an app with a share
// sheet, which the sheet's extension has too ("" without one: the
// profile's).
func (c *appConfig) keychainGroup(team string) string {
	if !c.ShareTarget.on || !c.shareSheet {
		return ""
	}
	return team + "." + c.ID
}
