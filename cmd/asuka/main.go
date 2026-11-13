// Command Asuka makes, runs and builds Asuka apps.
//
//	asuka new <dir>                          a project from the template
//	asuka dev --platform ios <dir>           build, run on the iOS simulator, rebuild on save
//	asuka dev --platform android <dir>       build, run on an Android emulator or device, rebuild on save
//	asuka dev --platform mac <dir>           build, run on this Mac, rebuild on save
//	asuka dev --platform windows <dir>       build, run on this PC, rebuild on save
//	asuka dev --platform linux <dir>         build, run on this Linux machine, rebuild on save
//	asuka dev --platform web <dir>           build, serve and open in the browser, rebuild on save
//	asuka dev <dir>                          on macOS, Windows or Linux: run on this computer
package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/asuka/vet"
	"golang.org/x/tools/go/analysis/unitchecker"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  asuka new <dir>                           make a project from the template
  asuka dev [-p platform] [flags] [dir]     run, rebuilding on save, on platform: ios (the
                                            simulator), android (an emulator or device), mac,
                                            windows, linux or web (the browser); default on
                                            macOS, Windows and Linux: this computer
  asuka build [-p platform] [flags] [dir]   a release build for platform, likewise
  asuka publish -p platform [flags] [dir]
                                            a release build, uploaded to TestFlight or Google Play;
                                            mac, windows, linux: its update, to asuka.json's updates
  asuka update keygen                       make the key pair that signs a desktop app's updates
  asuka plugin publish [dir]                upload a plugin's built libraries, record them in its manifest
  asuka native generate|check|new|add       native packages: Go interfaces their Swift, Kotlin, JavaScript implement
  asuka generate [dir]                      write the functions of the app's catalogs
  asuka i18n init|add|check                 start, add languages to, and check the catalogs
  asuka vet [flags] [packages]              report what builds but runs wrong, as a signal read once

asuka <command> --help lists its flags.
`)
}

func main() {
	if isVetTool(os.Args[1:]) {
		// go vet -vettool=asuka: its analyzers, by go vet's protocol.
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
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "plugin":
		err = cmdPlugin(os.Args[2:])
	case "native":
		err = cmdNative(os.Args[2:])
	case "vet":
		err = cmdVet(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if errors.Is(err, errHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "asuka:", err)
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
	noVet     bool
}

func devFlags(o *devOptions) *flagSet {
	fs := newFlagSet("dev", "[dir]")
	fs.about = "Runs the app, and runs it again with each change you save."
	platformFlag(fs, &o.platform, []string{platformIOS, platformAndroid, platformMac, platformWindows, platformLinux, platformWeb},
		"the `platform` to run on: ios (the simulator), android (a device or emulator), mac (this Mac), windows (this PC), linux (this Linux machine, or ASUKA_LINUX_RUN's), or web (the browser: the Go code as WebAssembly, the views the page's DOM); default on macOS, Windows and Linux: this computer")
	fs.Int(&o.port, "port", 0, 0, "web: the dev server's `port` (default 8790, or a free one)")
	fs.Bool(&o.lan, "lan", 0, "web: serve to this network too (a phone's browser), not only this machine")
	fs.Bool(&o.noOpen, "no-open", 0, "web: don't open the page in the browser")
	fs.String(&o.device, "device", 'd', "", "iOS: simulator name or UDID (default: the booted one, else the newest iPhone); Android: adb serial or AVD name")
	fs.Bool(&o.once, "once", 0, "build the app with its Go code and launch it once, without watching or streaming logs")
	fs.Bool(&o.embed, "embed", 0, "build the app with its Go code (the default runs the Go code here, in the player)")
	fs.String(&o.lang, "lang", 0, "", "show this `language` for this run, as de or zh-Hans, or a pseudo locale: en-XA (accented, a third longer) or ar-XB (right to left)")
	fs.String(&o.dir, "dir", 0, "", "lay the app out in this `direction` for this run, rtl or ltr, whatever its language")
	fs.Bool(&o.noVet, "no-vet", 0, "don't run asuka vet in the background (by default it runs on start and on each change, and prints what it finds)")
	fs.Bool(&o.verbose, "verbose", 'v', "print the commands asuka runs")
	return fs
}

func cmdDev(args []string) error {
	var o devOptions
	fs := devFlags(&o)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := setDevLocale(o.lang, o.dir); err != nil {
		return err
	}
	devVet.on = !o.noVet
	dir := "."
	if fs.Arg(0) != "" {
		dir = fs.Arg(0)
	}
	if o.platform == "" {
		if o.platform = thisComputer(); o.platform == "" {
			return errors.New("pick a platform: asuka dev --platform ios|android|web <dir>")
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
