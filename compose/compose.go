// Package compose opens the system's mail and message composers, filled
// in for the user to send: recipients, a subject, a body, attachments.
//
// iOS shows its own composers over the app (MessageUI), which say
// whether the user sent the mail or message, saved a draft or cancelled.
// Android hands them to the user's mail app (an app for mailto:, picked
// as Android picks one) or its messages app (the default SMS app), which
// don't say. The Mac hands a mail to the user's mail app (a mailto:
// link; with attachments, the mail share service) and a message to
// Messages (NSSharingService). Windows hands a mail to the user's mail
// app: through mailto: without attachments; with them, through Simple
// MAPI (Outlook, Thunderbird). Windows has no message composer.
package compose

import (
	"encoding/json"
	"errors"
	"html"
	"mime"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/ui"
)

// Result is what the user did with the composer.
type Result uint8

const (
	// Cancelled: they closed it without sending (iOS deletes the draft
	// then, or saves it: Saved).
	Cancelled Result = iota
	// Sent: they sent it (iOS; for a mail, it's in the outbox).
	Sent
	// Saved: they saved the mail as a draft (iOS).
	Saved
	// Unknown: it went to another app (Android, the Mac, Windows), which
	// doesn't say what the user did.
	Unknown
)

func (r Result) String() string {
	switch r {
	case Sent:
		return "sent"
	case Saved:
		return "saved"
	case Unknown:
		return "unknown"
	}
	return "cancelled"
}

// ErrUnavailable: the device can't compose this (no mail account or
// mail app; no messages app or SIM; a message on Windows), or not with
// these attachments (see CanSendAttachments).
var ErrUnavailable = errors.New("compose: unavailable")

// Attachment is a file sent with a mail or a message.
type Attachment struct {
	// Source is the file: a path, an asset's (as ui.Image takes it), or
	// an http(s) URL, which the host downloads first.
	Source string
	// Type is its MIME type ("application/pdf"). Default: its
	// extension's.
	Type string
	// Name is its name in the mail ("Invoice.pdf"). Default: the file's.
	Name string
}

// MailMessage is a mail to compose.
type MailMessage struct {
	To, CC, BCC []string // addresses
	Subject     string
	// Body is the mail's text, or its markup if HTML.
	Body string
	// HTML says Body is HTML. Where the mail can't carry markup (a
	// mailto: link, Simple MAPI), it goes as its text.
	HTML        bool
	Attachments []Attachment
}

// Mail opens the mail composer with m. done (if not nil) gets what the
// user did, on the main thread, once the composer is closed (iOS) or the
// mail app has it; ErrUnavailable if the device can't send mail. Main
// thread.
//
// iOS: Mail's composer if the user has a Mail account (CanSendMail); else
// a mailto: link, for the mail app they set (without attachments, which
// fail with ErrUnavailable). Android: the mail apps only (ACTION_SENDTO,
// or ACTION_SEND with mailto: as its selector), the attachments through
// the app's FileProvider, readable by the app picked. The Mac: a mailto:
// link for the user's mail app; with attachments, a new message from the
// mail share service, which takes no CC or BCC (they fail with
// ErrUnavailable then). Windows: a mailto: link;
// with attachments, Simple MAPI, which Outlook and Thunderbird take (the
// new Outlook and Windows's Mail app don't: ErrUnavailable).
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Mail(m MailMessage, done func(Result, error)) {
	text := m.Body
	if m.HTML {
		text = PlainText(m.Body)
	}
	b, _ := json.Marshal(map[string]any{
		"to": nonNil(m.To), "cc": nonNil(m.CC), "bcc": nonNil(m.BCC), "subject": m.Subject,
		"body": m.Body, "html": m.HTML, "text": text, "mailto": mailto(m, text),
		"attachments": attachments(m.Attachments),
	})
	call("mail", b, done)
}

// TextMessage is a text message (SMS, MMS, iMessage) to compose.
type TextMessage struct {
	// To are the recipients' phone numbers (or, for iMessage, addresses).
	To []string
	// Subject is the message's subject, where the user turned subjects on
	// (iOS's Messages); elsewhere it goes before the body.
	Subject string
	Body    string
	// Attachments are sent as MMS (or iMessage): see CanSendAttachments.
	Attachments []Attachment
}

// Message opens the message composer with m. done (if not nil) gets what
// the user did, on the main thread, once the composer is closed (iOS) or
// the messages app has it; ErrUnavailable if the device can't send text
// messages (CanSendText), or attachments (CanSendAttachments). Main
// thread.
//
// iOS: Messages' composer. Android: the default SMS app (smsto:, or
// ACTION_SEND with the attachments). The Mac: a new message in Messages.
// Windows: ErrUnavailable.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Message(m TextMessage, done func(Result, error)) {
	b, _ := json.Marshal(map[string]any{
		"to": nonNil(m.To), "subject": m.Subject, "body": m.Body, "attachments": attachments(m.Attachments),
	})
	call("message", b, done)
}

// Can is what the device can compose.
type Can struct {
	// Mail: the device can send mail: iOS has a Mail account, or a mail
	// app takes mailto: links; elsewhere a mail app takes them.
	Mail bool
	// MailComposer: Mail's own composer shows (iOS), which says what the
	// user did and takes attachments.
	MailComposer bool
	// Text: the device can send text messages (a SIM or iMessage; an app
	// for them).
	Text bool
	// TextAttachments: messages can carry attachments (MMS, iMessage).
	TextAttachments bool
}

// Check is what the device can compose. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Check() (Can, error) {
	b, err := ui.CallWait("compose", "can", nil)
	if err != nil {
		return Can{}, err
	}
	if len(b) < 4 {
		return Can{}, errors.New("compose: a bad reply")
	}
	return Can{Mail: b[0] == 1, MailComposer: b[1] == 1, Text: b[2] == 1, TextAttachments: b[3] == 1}, nil
}

// CanSendMail reports whether the device can send mail (Check's Mail).
// Not on the main thread.
func CanSendMail() bool {
	c, _ := Check()
	return c.Mail
}

// CanSendText reports whether the device can send text messages
// (Check's Text). Not on the main thread.
func CanSendText() bool {
	c, _ := Check()
	return c.Text
}

// CanSendAttachments reports whether text messages can carry
// attachments (Check's TextAttachments). Not on the main thread.
func CanSendAttachments() bool {
	c, _ := Check()
	return c.TextAttachments
}

func call(method string, args []byte, done func(Result, error)) {
	ui.Call("compose", method, args, func(r []byte, err error) {
		if done == nil {
			return
		}
		if err != nil {
			if err.Error() == "unavailable" {
				err = ErrUnavailable
			}
			done(Cancelled, err)
			return
		}
		res := Unknown
		if len(r) > 0 && r[0] <= byte(Unknown) {
			res = Result(r[0])
		}
		done(res, nil)
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type attachment struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Name   string `json:"name"`
}

func attachments(as []Attachment) []attachment {
	out := make([]attachment, len(as))
	for i, a := range as {
		name := a.Name
		if name == "" {
			name = filepath.Base(a.Source)
			if j := strings.IndexAny(name, "?#"); j >= 0 && strings.Contains(a.Source, "://") {
				name = name[:j]
			}
		}
		t := a.Type
		if t == "" {
			t, _, _ = strings.Cut(mime.TypeByExtension(filepath.Ext(name)), ";")
		}
		if t == "" {
			t = "application/octet-stream"
		}
		out[i] = attachment{Source: ui.AssetURL(a.Source), Type: t, Name: name}
	}
	return out
}

// mailto is m as a mailto: link (RFC 6068), its body as text.
func mailto(m MailMessage, text string) string {
	esc := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~!$'()*+,;:@/?", c) >= 0 {
				b.WriteByte(c)
				continue
			}
			b.WriteString("%" + strings.ToUpper(hex2(c)))
		}
		return b.String()
	}
	join := func(as []string) string {
		parts := make([]string, len(as))
		for i, a := range as {
			parts[i] = esc(a)
		}
		return strings.Join(parts, ",")
	}
	u := "mailto:" + join(m.To)
	var q []string
	if len(m.CC) > 0 {
		q = append(q, "cc="+join(m.CC))
	}
	if len(m.BCC) > 0 {
		q = append(q, "bcc="+join(m.BCC))
	}
	if m.Subject != "" {
		q = append(q, "subject="+esc(m.Subject))
	}
	if text != "" {
		// Line breaks are CRLF in a mail (RFC 6068 2).
		q = append(q, "body="+esc(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")))
	}
	if len(q) > 0 {
		u += "?" + strings.Join(q, "&")
	}
	return u
}

func hex2(c byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[c>>4], digits[c&15]})
}

// PlainText is markup's text, as a mail app shows an HTML mail's text
// part: blocks on lines of their own, list items with bullets, links
// with their URL after them, scripts and styles left out, entities
// decoded. For a body that goes where markup can't (a mailto: link).
func PlainText(markup string) string {
	var b strings.Builder
	newline := func(n int) {
		s := b.String()
		have := len(s) - len(strings.TrimRight(s, "\n"))
		if len(s) == have { // nothing written yet
			return
		}
		for ; have < n; have++ {
			b.WriteByte('\n')
		}
	}
	var href []string // the open links' URLs
	pre := 0
	for len(markup) > 0 {
		i := strings.IndexByte(markup, '<')
		if i < 0 {
			i = len(markup)
		}
		if i > 0 {
			text := html.UnescapeString(markup[:i])
			if pre == 0 {
				text = collapse(text, b.Len() == 0 || strings.HasSuffix(b.String(), "\n") || strings.HasSuffix(b.String(), " "))
			}
			b.WriteString(text)
			markup = markup[i:]
			continue
		}
		if strings.HasPrefix(markup, "<!--") {
			end := strings.Index(markup, "-->")
			if end < 0 {
				break
			}
			markup = markup[end+3:]
			continue
		}
		end := strings.IndexByte(markup, '>')
		if end < 0 {
			break
		}
		tag := markup[1:end]
		markup = markup[end+1:]
		closing := strings.HasPrefix(tag, "/")
		tag = strings.TrimPrefix(tag, "/")
		name := strings.ToLower(tag)
		if j := strings.IndexAny(name, " \t\r\n/"); j >= 0 {
			name = name[:j]
		}
		switch name {
		case "script", "style", "head", "title":
			if !closing {
				if k := strings.Index(strings.ToLower(markup), "</"+name); k >= 0 {
					markup = markup[k:]
				} else {
					markup = ""
				}
			}
		case "br":
			b.WriteByte('\n')
		case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "table", "blockquote", "section", "article", "header", "footer":
			if name == "p" || name[0] == 'h' && len(name) == 2 {
				newline(2)
			} else {
				newline(1)
			}
		case "pre":
			newline(2)
			if closing {
				pre = max(pre-1, 0)
			} else {
				pre++
			}
		case "tr", "hr":
			newline(1)
		case "td", "th":
			if closing {
				b.WriteByte('\t')
			}
		case "li":
			if !closing {
				newline(1)
				b.WriteString("• ")
			}
		case "a":
			if closing {
				if n := len(href); n > 0 {
					// The URL after the link's text, unless the text is it.
					if u := strings.TrimPrefix(href[n-1], "mailto:"); u != "" && !strings.HasPrefix(u, "#") && !strings.HasSuffix(b.String(), u) {
						b.WriteString(" (" + u + ")")
					}
					href = href[:n-1]
				}
			} else {
				href = append(href, attr(tag, "href"))
			}
		case "img":
			if alt := attr(tag, "alt"); alt != "" {
				b.WriteString(alt)
			}
		}
	}
	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// collapse is HTML's white space collapsing: each run a space, none at a
// line's start.
func collapse(s string, atStart bool) string {
	var b strings.Builder
	space := atStart
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}
	return b.String()
}

// attr is a tag's attribute's value, decoded ("" if it has none).
func attr(tag, name string) string {
	lower := strings.ToLower(tag)
	for i := 0; ; {
		j := strings.Index(lower[i:], name)
		if j < 0 {
			return ""
		}
		j += i
		k := j + len(name)
		if j > 0 && !strings.ContainsRune(" \t\r\n", rune(lower[j-1])) || k >= len(lower) || lower[k] != '=' {
			i = k
			continue
		}
		v := tag[k+1:]
		if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
			q := v[0]
			if e := strings.IndexByte(v[1:], q); e >= 0 {
				return html.UnescapeString(v[1 : 1+e])
			}
			return html.UnescapeString(v[1:])
		}
		if e := strings.IndexAny(v, " \t\r\n"); e >= 0 {
			v = v[:e]
		}
		return html.UnescapeString(v)
	}
}
