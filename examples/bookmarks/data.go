package main

import (
	"context"
	"database/sql"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/egoist/ryui/sqlite"
	"github.com/egoist/ryui/store"
	"github.com/egoist/ryui/ui"
)

// The bookmarks are in SQLite, in the app's data directory (store.Dir),
// which the share sheet's process shares: what it saves, the app lists.

type bookmark struct {
	ID          int64
	URL         string
	Title       string
	Description string
	Note        string
	Cover       string // an image's URL, or a file of the app's
	Collection  int64  // 0: Unsorted
	Tags        string // comma-separated
	Created     int64
}

type collection struct {
	ID   int64
	Name string
}

var db *sqlite.DB

func openDB() {
	var err error
	if db, err = sqlite.Open("bookmarks.db"); err != nil {
		panic(err)
	}
	if err := db.Migrate(
		`create table collections (id integer primary key, name text not null unique)`,
		`insert into collections (name) values ('Reading'), ('Design'), ('Development')`,
		`create table bookmarks (
			id integer primary key,
			url text not null default '',
			title text not null default '',
			description text not null default '',
			note text not null default '',
			cover text not null default '',
			collection integer not null default 0,
			tags text not null default '',
			created integer not null
		)`,
	); err != nil {
		panic(err)
	}
}

// write runs a statement off the main thread; a failure shows.
func write(query string, args ...any) {
	db.ExecAsync(query, args, func(_ sql.Result, err error) {
		if err != nil {
			ui.Alert(ui.AlertProps{Title: "Couldn't save", Message: err.Error()})
		}
	})
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// host is a link's site, without "www.".
func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return strings.TrimPrefix(u.Host, "www.")
}

var linkRE = regexp.MustCompile(`https?://\S+`)

// keepFile copies a shared file (the sheet's to read only while it's
// open) to the app's data directory, returning the copy's path.
func keepFile(path string) (string, error) {
	dir := filepath.Join(store.Dir(), "covers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	dst := filepath.Join(dir, time.Now().Format("20060102-150405.000")+filepath.Ext(path))
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	return dst, out.Close()
}

// page is what a web page says of itself: its title, description and
// image (Open Graph's, else its <title>).
type page struct{ Title, Description, Image string }

var (
	metaRE  = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	attrRE  = regexp.MustCompile(`(?is)(property|name|content)\s*=\s*("([^"]*)"|'([^']*)')`)
	titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// fetchPage reads the head of the page at link. Not on the main thread.
func fetchPage(link string) (page, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", link, nil)
	if err != nil {
		return page{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return page{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	doc := string(b)
	var p page
	for _, m := range metaRE.FindAllString(doc, -1) {
		var key, content string
		for _, a := range attrRE.FindAllStringSubmatch(m, -1) {
			v := a[3] + a[4]
			if strings.EqualFold(a[1], "content") {
				content = v
			} else {
				key = strings.ToLower(v)
			}
		}
		content = strings.TrimSpace(html.UnescapeString(content))
		switch key {
		case "og:title", "twitter:title":
			if p.Title == "" {
				p.Title = content
			}
		case "og:description", "twitter:description", "description":
			if p.Description == "" {
				p.Description = content
			}
		case "og:image", "twitter:image":
			if p.Image == "" {
				p.Image = content
			}
		}
	}
	if p.Title == "" {
		if m := titleRE.FindStringSubmatch(doc); m != nil {
			p.Title = strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	// An image's link relative to the page: from the page's.
	if p.Image != "" {
		if base, err := url.Parse(resp.Request.URL.String()); err == nil {
			if u, err := base.Parse(p.Image); err == nil {
				p.Image = u.String()
			}
		}
	}
	return p, nil
}
