// Command nui makes, runs and builds nui apps.
//
//	nui new <dir>                  a project from the template
//	nui dev -ios <dir>             build, run on the iOS simulator, rebuild on save
//	nui dev -android <dir>         build, run on an Android emulator or device, rebuild on save
//	nui dev -mac <dir>             build, run on this Mac, rebuild on save
//	nui dev -windows <dir>         build, run on this PC, rebuild on save
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  nui new <dir>                  make a project from the template
  nui dev -ios [flags] [dir]     run on the iOS simulator, rebuilding on save
  nui dev -android [flags] [dir] run on an Android device or emulator, rebuilding on save
  nui dev -mac [flags] [dir]     run on this Mac, rebuilding on save
  nui dev -windows [flags] [dir] run on this PC, rebuilding on save
  nui build -ios|-android|-mac|-windows [flags] [dir]   a release build

nui dev flags:
`)
}

func main() {
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
	case "-h", "-help", "--help", "help":
		usage()
		devFlags(new(devOptions)).PrintDefaults()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "nui:", err)
		os.Exit(1)
	}
}

type devOptions struct {
	ios, android bool
	mac          bool
	windows      bool
	device       string
	once         bool
	embed        bool
	verbose      bool
}

func devFlags(o *devOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("dev", flag.ContinueOnError)
	fs.BoolVar(&o.ios, "ios", false, "run on the iOS simulator")
	fs.BoolVar(&o.android, "android", false, "run on an Android device or emulator")
	fs.BoolVar(&o.mac, "mac", false, "run on this Mac")
	fs.BoolVar(&o.windows, "windows", false, "run on this PC (Windows)")
	fs.StringVar(&o.device, "device", "", "iOS: simulator name or UDID (default: the booted one, else the newest iPhone); Android: adb serial or AVD name")
	fs.BoolVar(&o.once, "once", false, "build the app with its Go code and launch it once, without watching or streaming logs")
	fs.BoolVar(&o.embed, "embed", false, "build the app with its Go code (the default runs the Go code here, in the player)")
	fs.BoolVar(&o.verbose, "v", false, "print the commands nui runs")
	return fs
}

func cmdDev(args []string) error {
	var o devOptions
	fs := devFlags(&o)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	switch {
	case o.mac:
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		return devMac(dir, &o, stop)
	case o.windows:
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		return devWindows(dir, &o, stop)
	case o.android:
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		return devAndroid(dir, &o, stop)
	case !o.ios:
		return errors.New("pick a platform: nui dev -ios|-android|-mac|-windows <dir>")
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	return devIOS(dir, &o, stop)
}
