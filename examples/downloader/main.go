// Downloader saves videos from links: a link pasted or shared from
// another app (ui.OnShare), downloaded while the app is out of sight
// (ryui/background, its progress and Pause and Cancel in a notification),
// on Wi-Fi only if asked (ryui/network), into the gallery (ryui/library).
// Its browser finds the videos a page plays, from a script that runs
// before the page's own (WebViewProps.Preload), and blocks ads; its icons
// are SVG (ryui/svg); ryui/js runs a script off screen.
//
//	go run ./cmd/ryui dev -android -once examples/downloader
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/background"
	"github.com/egoist/ryui/js"
	"github.com/egoist/ryui/library"
	"github.com/egoist/ryui/network"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/store"
	"github.com/egoist/ryui/svg"
	"github.com/egoist/ryui/ui"
)

// Lucide's icons.
var (
	iconDownload = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>`)
	iconWifi     = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h.01"/><path d="M2 8.82a15 15 0 0 1 20 0"/><path d="M5 12.859a10 10 0 0 1 14 0"/><path d="M8.5 16.429a5 5 0 0 1 7 0"/></svg>`)
	iconGlobe    = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/></svg>`)
)

// sample is a short video to try with.
const sample = "https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4"

var wifiOnly = store.Signal("wifiOnly", false)

func App() ui.Node {
	link := signal.New(sample)
	status := signal.New("")
	ui.OnShare(func(text string) {
		// "A title https://...": the link in it.
		if u := regexp.MustCompile(`https?://\S+`).FindString(text); u != "" {
			link.Set(u)
			status.Set("Shared: " + u)
		}
	})
	net := network.Use()
	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 16},
			ui.TextField(ui.TextFieldProps{Value: link, Placeholder: ui.Val("A link to a video"), Keyboard: ui.KeyboardURL}),
			ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
				svg.View(svg.Props{Icon: iconWifi, Style: ui.Style{Width: ui.Pt(22), Height: ui.Pt(22)},
					Color: signal.Func(func() ui.Color {
						if n := net.Get(); n.Connected && !n.Metered {
							return ui.Green
						}
						return ui.SecondaryLabel
					})}),
				ui.Text(signal.Func(func() string {
					n := net.Get()
					if !n.Connected {
						return "Offline"
					}
					s := n.Type.String()
					if n.Metered {
						s += ", metered"
					}
					return s
				}), ui.Style{Grow: 1, Color: ui.SecondaryLabel}),
				ui.StaticText("Wi-Fi only"),
				ui.Toggle(ui.ToggleProps{Value: wifiOnly}),
			),
			ui.Button(ui.ButtonProps{Title: ui.Val("Download"), Variant: ui.ButtonFilled, OnPress: func() {
				if n := signal.Untrack(net.Get); wifiOnly.Peek() && n.Metered {
					status.Set("Waiting for Wi-Fi: this connection is metered")
					return
				}
				download(link.Peek(), status)
			}}),
			ui.Row(ui.Style{Gap: 12},
				ui.Button(ui.ButtonProps{Title: ui.Val("Browse"), Variant: ui.ButtonGray, OnPress: func() {
					nav.Push(func() ui.Node { return browser(link, status) }, ui.ScreenOptions{Title: ui.Val("Browser")})
				}}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Run JS"), Variant: ui.ButtonGray, OnPress: func() {
					go func() {
						v, err := js.Eval(context.Background(), `const xs = [1, 2, 3].map(x => x * 2); Promise.resolve(xs.join("+") + "=" + xs.reduce((a, b) => a + b))`)
						ui.Post(func() { status.Set(fmt.Sprintf("JS: %s %v", v, err)) })
					}()
				}}),
			),
			ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter},
				svg.View(svg.Props{Icon: iconDownload, Style: ui.Style{Width: ui.Pt(18), Height: ui.Pt(18)}}),
				ui.Text(status, ui.Style{Grow: 1, Color: ui.SecondaryLabel, Font: ui.Footnote}),
			),
		)
	}, ui.ScreenOptions{Title: ui.Val("Downloader")})
}

// download fetches url in a background task, its progress in the task's
// notification, then saves it to the gallery's videos.
func download(url string, status *signal.Signal[string]) {
	name := path.Base(strings.SplitN(url, "?", 2)[0])
	if name == "" || name == "/" || name == "." {
		name = "video.mp4"
	}
	var paused, cancelled atomic.Bool
	actions := func() []background.Action {
		if paused.Load() {
			return []background.Action{{ID: "resume", Title: "Resume"}, {ID: "cancel", Title: "Cancel"}}
		}
		return []background.Action{{ID: "pause", Title: "Pause"}, {ID: "cancel", Title: "Cancel"}}
	}
	task := background.Start(background.Options{Title: "Downloading " + name, Progress: -1, Actions: actions()}, func(a string) {
		switch a {
		case "pause":
			paused.Store(true)
		case "resume":
			paused.Store(false)
		case "cancel":
			cancelled.Store(true)
		}
	})
	status.Set("Downloading " + name)
	go func() {
		file := filepath.Join(store.Dir(), "downloads", name)
		err := fetch(url, file, &paused, &cancelled, func(done, total int64) {
			ui.Post(func() {
				p := -1.0
				text := fmt.Sprintf("%.1f MB", float64(done)/1e6)
				if total > 0 {
					p = float64(done) / float64(total)
					text = fmt.Sprintf("%.0f%% of %.1f MB", p*100, float64(total)/1e6)
				}
				if paused.Load() {
					text = "Paused · " + text
				}
				task.Update(background.Options{Title: "Downloading " + name, Text: text, Progress: p, Actions: actions()})
			})
		})
		ui.Post(func() {
			if err != nil {
				task.End()
				status.Set("Couldn't: " + err.Error())
				return
			}
			task.Update(background.Options{Title: "Saving " + name, Progress: -1})
			library.Save(file, library.Options{Collection: library.Movies, Folder: "Downloader", Move: true}, func(where string, err error) {
				task.End()
				if err != nil {
					status.Set("Couldn't save: " + err.Error())
					return
				}
				status.Set("Saved: " + where)
			})
		})
	}()
}

// fetch downloads url to file, reporting its progress about ten times a
// second; paused holds it, cancelled stops it.
func fetch(url, file string, paused, cancelled *atomic.Bool, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", res.Status)
	}
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 64<<10)
	var done int64
	last := time.Now()
	for {
		for paused.Load() && !cancelled.Load() {
			time.Sleep(200 * time.Millisecond)
		}
		if cancelled.Load() {
			os.Remove(file)
			return fmt.Errorf("cancelled")
		}
		n, err := res.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if time.Since(last) > 100*time.Millisecond {
				last = time.Now()
				progress(done, res.ContentLength)
			}
		}
		if err == io.EOF {
			progress(done, res.ContentLength)
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// preload runs before each page's own scripts: it notes the media the
// page fetches or plays, for take().
const preload = `(() => {
  const seen = new Set(), found = [];
  const add = (u) => { try { u = new URL(u, location.href).href } catch { return }
    if (/\.(m3u8|mpd|mp4|webm|m4a|mp3)(\?|$)/i.test(u) && !seen.has(u)) { seen.add(u); found.push(u) } };
  const f = window.fetch;
  window.fetch = function (input, init) { add(input instanceof Request ? input.url : String(input)); return f.apply(this, arguments) };
  const open = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (m, u) { add(u); return open.apply(this, arguments) };
  document.addEventListener("play", (e) => { if (e.target.currentSrc) add(e.target.currentSrc) }, true);
  window.__found = { take: () => found.splice(0) };
})()`

// browser is a web view with an address bar, finding the media its pages
// load (from the preload's hooks, and on Android each request too).
func browser(link, status *signal.Signal[string]) ui.Node {
	nav := ui.UseNavigator()
	state := signal.New(ui.WebState{})
	media := signal.NewFunc([]string(nil), nil)
	addMedia := func(u string) {
		for _, m := range media.Peek() {
			if m == u {
				return
			}
		}
		media.Set(append(append([]string(nil), media.Peek()...), u))
	}
	var web ui.Node
	web = ui.WebView(ui.WebViewProps{
		URL:     ui.Val("https://test-videos.co.uk/bigbuckbunny/mp4-h264"),
		Preload: preload,
		Block:   []string{"doubleclick.net", "googlesyndication.com", "google-analytics.com"},
		OnState: func(s ui.WebState) { state.Set(s) },
		OnLoad: func(string) {
			ui.EvalJSResult(web, "window.__found ? window.__found.take() : []", func(v json.RawMessage, err error) {
				for _, u := range regexp.MustCompile(`"(https?://[^"]+)"`).FindAllStringSubmatch(string(v), -1) {
					addMedia(u[1])
				}
			})
		},
		OnRequest: func(u string) {
			if regexp.MustCompile(`\.(m3u8|mpd|mp4|webm)(\?|$)`).MatchString(u) {
				addMedia(u)
			}
		},
		OnDownload: func(d ui.WebDownload) { addMedia(d.URL) },
		Style:      ui.Style{Grow: 1},
	})
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 12), Align: ui.AlignCenter},
			ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: "Back", Variant: ui.ButtonGray,
				Disabled: signal.Func(func() bool { return !state.Get().CanGoBack }), OnPress: func() { ui.WebBack(web) }}),
			svg.View(svg.Props{Icon: iconGlobe, Style: ui.Style{Width: ui.Pt(18), Height: ui.Pt(18)}}),
			ui.Text(signal.Func(func() string {
				s := state.Get()
				if s.Loading {
					return fmt.Sprintf("%.0f%% · %s", s.Progress*100, s.URL)
				}
				if s.Title != "" {
					return s.Title
				}
				return s.URL
			}), ui.Style{Grow: 1, Lines: 1, Font: ui.Footnote}),
			ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: "Reload", Variant: ui.ButtonGray, OnPress: func() { ui.WebReload(web) }}),
		),
		web,
		ui.Column(ui.Style{Padding: ui.All(12), Gap: 8},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("Videos found: %d", len(media.Get())) }), ui.Style{Font: ui.Headline}),
			ui.For(signal.Func(func() []string {
				m := media.Get()
				return m[max(0, len(m)-3):]
			}), func(u string) string { return u }, func(u signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
				return ui.Button(ui.ButtonProps{Title: signal.Func(func() string { return path.Base(u.Get()) }), Variant: ui.ButtonBordered,
					OnPress: func() {
						link.Set(signal.Untrack(u.Get))
						ui.WebCookies(signal.Untrack(u.Get), func(c string, err error) {
							status.Set(fmt.Sprintf("Picked from the browser (%d cookies)", strings.Count(c, "=")))
						})
						nav.Pop()
					}})
			}),
		),
	)
}

func main() { app.Run(App) }
