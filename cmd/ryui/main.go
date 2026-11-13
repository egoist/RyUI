// Command RyUI makes, runs and builds RyUI apps.
//
//	ryui new <dir>                         a project from the template
//	ryui dev -platform ios <dir>           build, run on the iOS simulator, rebuild on save
//	ryui dev -platform android <dir>       build, run on an Android emulator or device, rebuild on save
//	ryui dev -platform mac <dir>           build, run on this Mac, rebuild on save
//	ryui dev -platform windows <dir>       build, run on this PC, rebuild on save
//	ryui dev -platform linux <dir>         build, run on this Linux machine, rebuild on save
//	ryui dev -platform web <dir>           build, serve and open in the browser, rebuild on save
//	ryui dev <dir>                         on macOS, Windows or Linux: run on this computer
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/ryui/vet"
	"golang.org/x/tools/go/analysis/unitchecker"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  ryui new <dir>                  make a project from the template
  ryui dev [-platform p] [flags] [dir]     run, rebuilding on save, on p: ios (the simulator), android (an
                                           emulator or device), mac, windows, linux or web (the browser);
                                           default on macOS, Windows and Linux: this computer
  ryui build [-platform p] [flags] [dir]   a release build for p, likewise (ryui build -h)
  ryui publish -platform ios|android [flags] [dir]   a release build, uploaded to TestFlight or Google Play (ryui publish -h)
  ryui generate [dir]             write the functions of the app's catalogs
  ryui i18n init|add|check        start, add languages to, and check the catalogs
  ryui vet [flags] [packages]     report what builds but runs wrong, as a signal read once

ryui dev flags:
`)
}

func main() {
	if isVetTool(os.Args[1:]) {
		// go vet -vettool=ryui: its analyzers, by go vet's protocol.
		unitchecker.Main(vet.Analyzers...)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "dev":
		err = cmdDev(os.Args[2:])
	case "build":
		err = cmdBuild(os.Args[2:])
	case "publish":
		err = cmdPublish(os.Args[2:])
	case "generate":
		err = cmdGenerate(os.Args[2:])
	case "i18n":
		err = cmdI18n(os.Args[2:])
	case "vet":
		err = cmdVet(os.Args[2:])
	case "-h", "-help", "--help", "help":
		usage()
		devFlags(new(devOptions)).PrintDefaults()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ryui:", err)
		os.Exit(1)
	}
}

type devOptions struct {
	platform  string
	port      int  // web: the dev server's port
	lan       bool // web: served to the network too, for phones
	noOpen    bool // web: don't open the browser
	device    string
	once      bool
	embed     bool
	verbose   bool
	lang, dir string
	vet       bool
}

func devFlags(o *devOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("dev", flag.ContinueOnError)
	platformFlag(fs, &o.platform, []string{platformIOS, platformAndroid, platformMac, platformWindows, platformLinux, platformWeb},
		"the `platform` to run on: ios (the simulator), android (a device or emulator), mac (this Mac), windows (this PC), linux (this Linux machine, or RYUI_LINUX_RUN's), or web (the browser: the Go code as WebAssembly, the views the page's DOM); default on macOS, Windows and Linux: this computer")
	fs.IntVar(&o.port, "port", 0, "web: the dev server's port (default 8790, or a free one)")
	fs.BoolVar(&o.lan, "lan", false, "web: serve to this network too (a phone's browser), not only this machine")
	fs.BoolVar(&o.noOpen, "no-open", false, "web: don't open the page in the browser")
	fs.StringVar(&o.device, "device", "", "iOS: simulator name or UDID (default: the booted one, else the newest iPhone); Android: adb serial or AVD name")
	fs.BoolVar(&o.once, "once", false, "build the app with its Go code and launch it once, without watching or streaming logs")
	fs.BoolVar(&o.embed, "embed", false, "build the app with its Go code (the default runs the Go code here, in the player)")
	fs.BoolVar(&o.verbose, "v", false, "print the commands ryui runs")
	fs.StringVar(&o.lang, "lang", "", "show this language for this run, as de or zh-Hans, or a pseudo locale: en-XA (accented, a third longer) or ar-XB (right to left)")
	fs.StringVar(&o.dir, "dir", "", "lay the app out rtl or ltr for this run, whatever its language")
	fs.BoolVar(&o.vet, "vet", true, "run ryui vet in the background, on start and on each change, and print what it finds")
	return fs
}

func cmdDev(args []string) error {
	var o devOptions
	fs := devFlags(&o)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := setDevLocale(o.lang, o.dir); err != nil {
		return err
	}
	devVet.on = o.vet
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if o.platform == "" {
		if o.platform = thisComputer(); o.platform == "" {
			return errors.New("pick a platform: ryui dev -platform ios|android|web <dir>")
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	switch o.platform {
	case platformWeb:
		return devWeb(dir, &o, stop)
	case platformMac:
		return devMac(dir, &o, stop)
	case platformWindows:
		return devWindows(dir, &o, stop)
	case platformLinux:
		return devLinux(dir, &o, stop)
	case platformAndroid:
		return devAndroid(dir, &o, stop)
	}
	return devIOS(dir, &o, stop)
}
