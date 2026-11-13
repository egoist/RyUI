package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// asuka publish: a release build, numbered after the store's last,
// uploaded, and handed to testers or review. The stores' APIs: App Store
// Connect's (appstore.go) and Google Play's (play.go).

type publishOptions struct {
	buildOptions
	file         string
	build        int
	notes        string
	noWait       bool
	groups       string
	review       bool
	release      string
	track        string
	rollout      float64
	draft        bool
	cancelReview bool
}

func publishFlags(o *publishOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	platformFlag(fs, &o.platform, []string{platformIOS, platformAndroid},
		"the `platform` whose store to upload to: ios, App Store Connect (TestFlight and the App Store); android, Google Play")
	fs.StringVar(&o.file, "file", "", "upload this .ipa or .aab (from asuka build) instead of building one")
	fs.IntVar(&o.build, "build", 0, "the build number (default: one more than the store's highest, at least asuka.json's build)")
	fs.StringVar(&o.notes, "notes", "", "what's new: text in the store's default language, @file likewise, or @dir with a file per language as the store names it (en-US.txt); TestFlight's What to Test and, with -review, the App Store's What's New; Play's release notes")
	fs.StringVar(&o.groups, "groups", "", "iOS: the TestFlight groups to give the build to, by name, comma separated (with an external group, the build goes to beta review)")
	fs.BoolVar(&o.review, "review", false, "iOS: submit the version, with this build, for App Store review")
	fs.StringVar(&o.release, "release", "", "iOS, with -review: once approved, release it automatically (auto) or by hand (manual); default: as App Store Connect has it")
	fs.BoolVar(&o.noWait, "no-wait", false, "iOS: stop once App Store Connect has the upload, without waiting for Apple to process it")
	fs.StringVar(&o.track, "track", "internal", "Android: the track to release to: internal, alpha, beta, production, or a closed track's name")
	fs.Float64Var(&o.rollout, "rollout", 0, "Android: roll the release out to this fraction of users, as 0.1, raised later in the Play Console (default: all)")
	fs.BoolVar(&o.draft, "draft", false, "Android: a draft release, to roll out in the Play Console")
	fs.BoolVar(&o.cancelReview, "cancel-review", false, "Android: if the app has changes in review, cancel that review and send them with this release (default: stop)")
	fs.StringVar(&o.profile, "profile", "", "iOS: a provisioning profile's path, name or UUID (default: asuka.json's, else the best match installed)")
	fs.StringVar(&o.out, "o", "", "the output directory of the build (default: dist in the app's directory)")
	fs.BoolVar(&o.verbose, "v", false, "print the commands asuka runs and its requests to the store")
	fs.BoolVar(&o.vet, "vet", true, "run asuka vet first and print what it finds (warnings: the build goes on)")
	return fs
}

func cmdPublish(args []string) error {
	var o publishOptions
	fs := publishFlags(&o)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if err := o.check(set); err != nil {
		return err
	}
	notes, err := readNotes(o.notes)
	if err != nil {
		return err
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	if o.platform == platformIOS {
		key, err := loadASCKey(app.cfg)
		if err != nil {
			return err
		}
		return publishIOS(newASC(key, o.verbose), app, &o, notes, buildForStore)
	}
	sa, err := loadServiceAccount(app.cfg)
	if err != nil {
		return err
	}
	return publishAndroid(newPlay(sa, o.verbose), app, &o, notes, buildForStore)
}

// check refuses flags that don't go together, before anything is built.
func (o *publishOptions) check(set map[string]bool) error {
	if o.platform == "" {
		return errors.New("usage: asuka publish -platform ios|android [flags] [dir]")
	}
	ios := o.platform == platformIOS
	only := func(platform string, names ...string) error {
		for _, n := range names {
			if set[n] {
				return fmt.Errorf("-%s is for -platform %s", n, platform)
			}
		}
		return nil
	}
	if ios {
		if err := only("android", "track", "rollout", "draft", "cancel-review"); err != nil {
			return err
		}
	} else if err := only("ios", "groups", "review", "release", "no-wait", "profile"); err != nil {
		return err
	}
	if want := map[bool]string{true: ".ipa", false: ".aab"}[ios]; o.file != "" && !strings.EqualFold(filepath.Ext(o.file), want) {
		return fmt.Errorf("-file %s: a %s", o.file, want)
	}
	if o.file != "" && set["build"] {
		return errors.New("-build numbers a build asuka makes; the -file's number is its own")
	}
	if set["build"] && o.build <= 0 {
		return errors.New("-build: a number above 0")
	}
	switch o.release {
	case "", "auto", "manual":
	default:
		return fmt.Errorf("-release %q: auto or manual", o.release)
	}
	if o.release != "" && !o.review {
		return errors.New("-release goes with -review")
	}
	if o.noWait && (o.groups != "" || o.notes != "" || o.review) {
		return errors.New("-no-wait: -groups, -notes and -review need the build processed, so they wait")
	}
	if set["rollout"] && (o.rollout <= 0 || o.rollout >= 1) {
		return fmt.Errorf("-rollout %v: a fraction of users above 0 and below 1, as 0.1", o.rollout)
	}
	if o.draft && set["rollout"] {
		return errors.New("-draft or -rollout: a draft isn't rolled out")
	}
	return nil
}

// buildForStore makes the release build the store takes: its path.
func buildForStore(app *goApp, o *buildOptions) (string, error) {
	if o.out == "" {
		o.out = filepath.Join(app.dir, "dist")
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return "", err
	}
	vetBuild(app.dir, o.vet)
	if o.platform == platformIOS {
		o.distribution = "app-store"
		return ipaPath(app.cfg, o), buildIOS(app, o)
	}
	return aabPath(app.cfg, o), buildAndroid(app, o)
}

func ipaPath(cfg *appConfig, o *buildOptions) string {
	return filepath.Join(o.out, safeName(cfg.Name)+".ipa")
}

func aabPath(cfg *appConfig, o *buildOptions) string {
	return filepath.Join(o.out, safeName(cfg.Name)+".aab")
}

// nextBuild is the build number to publish: want if given, which must
// be above the store's highest; else one more than the store's highest,
// and at least asuka.json's.
func nextBuild(highest, configured, want int) (int, error) {
	if want > 0 {
		if want <= highest {
			return 0, fmt.Errorf("-build %d: the store has build %d already; a build must be numbered higher", want, highest)
		}
		return want, nil
	}
	return max(highest+1, configured), nil
}

// readNotes reads -notes: text, or @file, for the store's default
// language (the key ""); or @dir, a file per language (<locale>.txt).
func readNotes(s string) (map[string]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	path, ok := strings.CutPrefix(s, "@")
	if !ok {
		return map[string]string{"": strings.TrimSpace(s)}, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("-notes: %v", err)
	}
	if !st.IsDir() {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(b)) == "" {
			return nil, fmt.Errorf("-notes: %s is empty", path)
		}
		return map[string]string{"": strings.TrimSpace(string(b))}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	notes := map[string]string{}
	for _, e := range entries {
		locale, ok := strings.CutSuffix(e.Name(), ".txt")
		if !ok || e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			return nil, err
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			notes[locale] = t
		}
	}
	if len(notes) == 0 {
		return nil, fmt.Errorf("-notes: no <locale>.txt with text in %s (as en-US.txt)", path)
	}
	return notes, nil
}

// localizeNotes gives the default language's notes ("") the store's
// default language, and checks each fits the store's limit.
func localizeNotes(notes map[string]string, lang string, limit int, what string) (map[string]string, error) {
	if len(notes) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for k, v := range notes {
		if k == "" {
			k = lang
		}
		if n := utf8.RuneCountInString(v); n > limit {
			return nil, fmt.Errorf("-notes (%s): %d characters; %s takes at most %d", k, n, what, limit)
		}
		out[k] = v
	}
	return out, nil
}

// ipaInfo is what an .ipa's Info.plist says of it.
type ipaInfo struct{ id, version, build string }

var appPlist = regexp.MustCompile(`^Payload/[^/]+\.app/Info\.plist$`)

func readIPAInfo(path string) (ipaInfo, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ipaInfo{}, fmt.Errorf("%s: %v", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !appPlist.MatchString(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ipaInfo{}, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return ipaInfo{}, err
		}
		if bytes.HasPrefix(data, []byte("bplist")) {
			// A binary property list: plutil reads it (macOS).
			c := exec.Command("plutil", "-convert", "xml1", "-o", "-", "--", "-")
			c.Stdin = bytes.NewReader(data)
			if data, err = c.Output(); err != nil {
				return ipaInfo{}, fmt.Errorf("%s: its Info.plist is binary, which plutil (macOS) reads: %v", path, err)
			}
		}
		m, err := plistStrings(data)
		if err != nil {
			return ipaInfo{}, fmt.Errorf("%s: Info.plist: %v", path, err)
		}
		info := ipaInfo{m["CFBundleIdentifier"], m["CFBundleShortVersionString"], m["CFBundleVersion"]}
		if info.id == "" || info.version == "" || info.build == "" {
			return ipaInfo{}, fmt.Errorf("%s: its Info.plist lacks the bundle ID, version or build", path)
		}
		return info, nil
	}
	return ipaInfo{}, fmt.Errorf("%s: no Payload/<app>.app/Info.plist: not an .ipa", path)
}

// plistStrings reads an XML property list's top-level string values.
func plistStrings(data []byte) (map[string]string, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	out := map[string]string{}
	depth := 0
	key := ""
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			// plist › dict › key, value
			if depth != 3 {
				continue
			}
			switch t.Name.Local {
			case "key", "string":
				var s string
				if err := d.DecodeElement(&s, &t); err != nil {
					return nil, err
				}
				depth--
				if t.Name.Local == "key" {
					key = s
				} else if key != "" {
					out[key], key = s, ""
				}
			default:
				key = "" // a value of another type
			}
		case xml.EndElement:
			depth--
		}
	}
}

// publishIOS builds the app (or takes -file), uploads it to App Store
// Connect, waits for Apple to process it, and hands it to TestFlight's
// groups and to App Review as asked.
func publishIOS(c *ascClient, app *goApp, o *publishOptions, notes map[string]string, build func(*goApp, *buildOptions) (string, error)) error {
	return ascHint(publishIOSSteps(c, app, o, notes, build), c.key)
}

func publishIOSSteps(c *ascClient, app *goApp, o *publishOptions, notes map[string]string, build func(*goApp, *buildOptions) (string, error)) error {
	cfg := app.cfg
	id, version := cfg.ID, cfg.Version
	var info ipaInfo
	if o.file != "" {
		var err error
		if info, err = readIPAInfo(o.file); err != nil {
			return err
		}
		if info.id != cfg.ID {
			return fmt.Errorf("%s is %s, but asuka.json's id is %s", o.file, info.id, cfg.ID)
		}
		version = info.version
	}
	// Everything the store can refuse, before the build.
	a, err := c.findApp(id)
	if err != nil {
		return err
	}
	fmt.Printf("asuka: %s (%s) on App Store Connect\n", a.Attributes.Name, id)
	if err := c.checkVersionOpen(a.ID, version); err != nil {
		return err
	}
	groups, err := c.betaGroups(a.ID, splitList(o.groups))
	if err != nil {
		return err
	}
	if notes, err = localizeNotes(notes, a.Attributes.PrimaryLocale, 4000, "App Store Connect"); err != nil {
		return err
	}
	path := o.file
	if path == "" {
		high, err := c.highestBuild(a.ID)
		if err != nil {
			return err
		}
		n, err := nextBuild(high, cfg.Build, o.build)
		if err != nil {
			return err
		}
		cfg.Build = n
		if high > 0 {
			fmt.Printf("asuka: build %d (App Store Connect's highest is %d)\n", n, high)
		}
		if path, err = build(app, &o.buildOptions); err != nil {
			return err
		}
		info = ipaInfo{id, version, strconv.Itoa(n)}
	}
	uploadID, err := c.upload(a.ID, path, info.version, info.build)
	if err != nil {
		return err
	}
	if o.noWait {
		fmt.Printf("asuka: App Store Connect has %s (%s); Apple is processing it, and emails when it's done\n", info.version, info.build)
		return nil
	}
	buildID, err := c.processed(a.ID, uploadID, info.version, info.build)
	if err != nil {
		return err
	}
	if len(notes) > 0 {
		if err := c.betaNotes(buildID, notes); err != nil {
			return err
		}
	}
	if err := c.testFlight(buildID, groups); err != nil {
		return err
	}
	if o.review {
		release := map[string]string{"auto": "AFTER_APPROVAL", "manual": "MANUAL"}[o.release]
		if err := c.submitReview(a.ID, buildID, info.version, notes, release); err != nil {
			return err
		}
	}
	fmt.Printf("asuka: %s %s (%s) is on App Store Connect, in TestFlight\n", a.Attributes.Name, info.version, info.build)
	return nil
}

// ascHint says what to do about App Store Connect's refusals of the key.
func ascHint(err error, key *ascKey) error {
	var ae *ascError
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%v\nApp Store Connect doesn't know the key: check its ID (%s), its issuer's ID and its .p8, and that it isn't revoked (Users and Access › Integrations)", err, key.id)
	case http.StatusForbidden:
		return fmt.Errorf("%v\nthe key %s may lack the role for this (App Manager or Admin), or an agreement awaits the account holder in App Store Connect (Business)", err, key.id)
	}
	return err
}

// publishAndroid builds the app (or takes -file), uploads it to Google
// Play and releases it on the track.
func publishAndroid(p *playClient, app *goApp, o *publishOptions, notes map[string]string, build func(*goApp, *buildOptions) (string, error)) error {
	cfg := app.cfg
	pkg := androidPackage(cfg.ID)
	// A look first: the highest version code and the listing's language,
	// and whether Play takes this app from this account at all.
	edit, err := p.insertEdit(pkg)
	if err != nil {
		return playHint(err, p.sa, pkg)
	}
	high, err := p.highestVersionCode(pkg, edit)
	lang := ""
	if err == nil && len(notes) > 0 {
		lang, err = p.defaultLanguage(pkg, edit)
	}
	p.deleteEdit(pkg, edit)
	if err != nil {
		return playHint(err, p.sa, pkg)
	}
	if notes, err = localizeNotes(notes, lang, 500, "Google Play"); err != nil {
		return err
	}
	fmt.Printf("asuka: %s on Google Play\n", pkg)
	path := o.file
	name := ""
	if path == "" {
		n, err := nextBuild(int(high), cfg.Build, o.build)
		if err != nil {
			return err
		}
		cfg.Build = n
		if high > 0 {
			fmt.Printf("asuka: version code %d (Google Play's highest is %d)\n", n, high)
		}
		if path, err = build(app, &o.buildOptions); err != nil {
			return err
		}
		name = fmt.Sprintf("%s (%d)", cfg.Version, n)
	}
	if edit, err = p.insertEdit(pkg); err != nil {
		return playHint(err, p.sa, pkg)
	}
	committed := false
	defer func() {
		if !committed {
			p.deleteEdit(pkg, edit)
		}
	}()
	vc, err := p.uploadBundle(pkg, edit, path)
	if err != nil {
		return playHint(err, p.sa, pkg)
	}
	rel := playRelease{Name: name, VersionCodes: []string{strconv.FormatInt(vc, 10)}, Status: "completed"}
	switch {
	case o.draft:
		rel.Status = "draft"
	case o.rollout > 0:
		rel.Status, rel.UserFraction = "inProgress", o.rollout
	}
	for _, lang := range slices.Sorted(maps.Keys(notes)) {
		rel.ReleaseNotes = append(rel.ReleaseNotes, playText{Language: lang, Text: notes[lang]})
	}
	status, err := p.updateTrack(pkg, edit, o.track, rel)
	if err != nil {
		return playHint(fmt.Errorf("releasing to the %s track: %w", o.track, err), p.sa, pkg)
	}
	sent, err := p.commit(pkg, edit, o.cancelReview)
	if err != nil {
		return playHint(err, p.sa, pkg)
	}
	committed = true
	what := map[string]string{"completed": "released", "inProgress": fmt.Sprintf("rolling out to %g%% of users", o.rollout*100), "draft": "a draft"}[status]
	fmt.Printf("asuka: version code %d is on Google Play's %s track, %s\n", vc, o.track, what)
	if !sent {
		fmt.Println("asuka: Play holds the app's changes for you to send for review: send them in the Play Console (Publishing overview)")
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
