package compose

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ui"
)

// Mail sends the mail, its text, a mailto: link and the attachments with
// their types and names; the results and errors come back.
func TestMail(t *testing.T) {
	h := fake.New()
	var method string
	var got map[string]any
	reply := []byte{1}
	var fail error
	h.Modules = map[string]func(string, []byte) ([]byte, error){"compose": func(m string, a []byte) ([]byte, error) {
		method = m
		got = nil
		if err := json.Unmarshal(a, &got); err != nil {
			t.Fatal(err)
		}
		return reply, fail
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	defer r.Dispose()
	run := func(f func(done func(Result, error))) (Result, error) {
		var res Result
		var err error
		f(func(r Result, e error) { res, err = r, e })
		r.Flush()
		h.RunPosted()
		return res, err
	}

	res, err := run(func(d func(Result, error)) {
		Mail(MailMessage{To: []string{"a@example.com"}, BCC: []string{"b@example.com"}, Subject: "Q3 & more",
			Body: "<p>Hello <b>you</b></p>", HTML: true,
			Attachments: []Attachment{{Source: "/tmp/Report.pdf"}, {Source: "https://example.com/x/photo.jpg?s=2", Name: ""}, {Source: "/tmp/data", Type: "text/csv", Name: "data.csv"}}}, d)
	})
	if res != Sent || err != nil || method != "mail" {
		t.Fatalf("%v %v %s", res, err, method)
	}
	if got["text"] != "Hello you" || got["html"] != true || got["subject"] != "Q3 & more" ||
		got["mailto"] != "mailto:a@example.com?bcc=b@example.com&subject=Q3%20%26%20more&body=Hello%20you" {
		t.Fatalf("mail %v", got)
	}
	if cc, ok := got["cc"].([]any); !ok || len(cc) != 0 {
		t.Fatalf("cc %v", got["cc"])
	}
	as := got["attachments"].([]any)
	want := [][3]string{
		{"/tmp/Report.pdf", "application/pdf", "Report.pdf"},
		{"https://example.com/x/photo.jpg?s=2", "image/jpeg", "photo.jpg"},
		{"/tmp/data", "text/csv", "data.csv"},
	}
	for i, w := range want {
		a := as[i].(map[string]any)
		if a["source"] != w[0] || a["type"] != w[1] || a["name"] != w[2] {
			t.Fatalf("attachment %d: %v", i, a)
		}
	}

	reply = []byte{3}
	if res, _ := run(func(d func(Result, error)) { Message(TextMessage{To: []string{"+15551234"}, Body: "hi"}, d) }); res != Unknown || method != "message" || got["body"] != "hi" {
		t.Fatalf("message: %v %s %v", res, method, got)
	}
	fail = errors.New("unavailable")
	if _, err := run(func(d func(Result, error)) { Message(TextMessage{}, d) }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable: %v", err)
	}
}

// Check reads the host's four answers.
func TestCheck(t *testing.T) {
	h := fake.New()
	h.Modules = map[string]func(string, []byte) ([]byte, error){"compose": func(m string, a []byte) ([]byte, error) {
		return []byte{1, 0, 1, 1}, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	done := make(chan Can, 1)
	go func() {
		c, _ := Check()
		done <- c
	}()
	for range 5000 { // a safety net, not what passes
		h.RunPosted()
		r.Flush()
		select {
		case c := <-done:
			if c != (Can{Mail: true, Text: true, TextAttachments: true}) {
				t.Fatalf("%+v", c)
			}
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("no answer")
}

func TestMailto(t *testing.T) {
	got := mailto(MailMessage{To: []string{"a@x.com", "b+c@y.org"}, CC: []string{"d@z.net"}, Subject: "Hi there?"}, "Line 1\nLine 2 = é")
	want := "mailto:a@x.com,b+c@y.org?cc=d@z.net&subject=Hi%20there?&body=Line%201%0D%0ALine%202%20%3D%20%C3%A9"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := mailto(MailMessage{}, ""); got != "mailto:" {
		t.Fatal(got)
	}
}

func TestPlainText(t *testing.T) {
	for _, c := range [][2]string{
		{"<p>Hello <b>you</b>,</p>\n<p>  See   the\n report.</p>", "Hello you,\n\nSee the report."},
		{"<h1>Title</h1><ul><li>One</li><li>Two &amp; three</li></ul>", "Title\n\n• One\n• Two & three"},
		{`<p>Visit <a href="https://go.dev">Go</a> or <a href='https://x.org'>https://x.org</a>.</p>`, "Visit Go (https://go.dev) or https://x.org."},
		{`<a href="mailto:me@x.com">me@x.com</a>`, "me@x.com"},
		{"<style>p { color: red }</style><script>alert(1)</script>Hi<br>there<!-- no -->", "Hi\nthere"},
		{"<pre>a\n  b</pre>", "a\n  b"},
		{`<img alt="Logo" src="x.png"> &lt;3`, "Logo <3"},
		{"plain text", "plain text"},
	} {
		if got := PlainText(c[0]); got != c[1] {
			t.Errorf("%q:\ngot  %q\nwant %q", c[0], got, c[1])
		}
	}
}
