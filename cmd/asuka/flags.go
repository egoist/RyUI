package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// The CLI's flags, as --long and -l: --name value or --name=value, -l
// value or -lvalue, bool shorts grouped (-vx), anywhere among the
// arguments, until --. A bool flag takes no value: one that's on by
// default is turned off by a flag of its own (--no-vet).

// errHelp is what parse returns for -h or --help, once it has printed
// the help: asuka exits 0.
var errHelp = errors.New("help requested")

// A flagSet is a command's flags, and the arguments left once they're
// parsed.
type flagSet struct {
	cmd   string // "dev", "i18n add": as asuka's help names it
	usage string // the arguments after the flags: "[dir]"
	about string // what the command does, under the usage line
	flags []*cliFlag
	set   map[string]bool // by long name: what parse set
	args  []string
	out   io.Writer // the help: stdout by default
}

type cliFlag struct {
	long  string
	short rune // 0: none
	usage string
	def   string // shown as the default, "" for none
	bool  bool
	value func(string) error
}

func newFlagSet(cmd, usage string) *flagSet {
	return &flagSet{cmd: cmd, usage: usage, set: map[string]bool{}}
}

func (fs *flagSet) add(f *cliFlag) {
	for _, g := range fs.flags {
		if g.long == f.long || f.short != 0 && g.short == f.short {
			panic("asuka: flag --" + f.long + " defined twice")
		}
	}
	fs.flags = append(fs.flags, f)
}

// Bool defines a flag that sets *p when given.
func (fs *flagSet) Bool(p *bool, long string, short rune, usage string) {
	fs.add(&cliFlag{long: long, short: short, usage: usage, bool: true, value: func(string) error {
		*p = true
		return nil
	}})
}

// String defines a flag that takes a string, def when not given.
func (fs *flagSet) String(p *string, long string, short rune, def, usage string) {
	*p = def
	fs.add(&cliFlag{long: long, short: short, usage: usage, def: def, value: func(s string) error {
		*p = s
		return nil
	}})
}

// Int defines a flag that takes an integer, def when not given.
func (fs *flagSet) Int(p *int, long string, short rune, def int, usage string) {
	*p = def
	f := &cliFlag{long: long, short: short, usage: usage, value: func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("%q is not a whole number", s)
		}
		*p = n
		return nil
	}}
	if def != 0 {
		f.def = strconv.Itoa(def)
	}
	fs.add(f)
}

// Float defines a flag that takes a number, def when not given.
func (fs *flagSet) Float(p *float64, long string, short rune, def float64, usage string) {
	*p = def
	f := &cliFlag{long: long, short: short, usage: usage, value: func(s string) error {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("%q is not a number", s)
		}
		*p = n
		return nil
	}}
	if def != 0 {
		f.def = strconv.FormatFloat(def, 'g', -1, 64)
	}
	fs.add(f)
}

// Func defines a flag whose value fn checks and keeps.
func (fs *flagSet) Func(long string, short rune, usage string, fn func(string) error) {
	fs.add(&cliFlag{long: long, short: short, usage: usage, value: fn})
}

func (fs *flagSet) lookup(long string) *cliFlag {
	for _, f := range fs.flags {
		if f.long == long {
			return f
		}
	}
	return nil
}

func (fs *flagSet) lookupShort(c rune) *cliFlag {
	for _, f := range fs.flags {
		if f.short == c {
			return f
		}
	}
	return nil
}

// name is how an error names f: as it was given.
func (f *cliFlag) name(short bool) string {
	if short {
		return "-" + string(f.short)
	}
	return "--" + f.long
}

// parse sets the flags in args and keeps the rest, in order, as the
// command's arguments.
func (fs *flagSet) parse(args []string) error {
	fs.args = fs.args[:0]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			fs.args = append(fs.args, args[i+1:]...)
			return nil
		case a == "-h" || a == "--help":
			fs.help()
			return errHelp
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(a[2:], "=")
			f := fs.lookup(name)
			if f == nil {
				return fs.unknown("--" + name)
			}
			if f.bool {
				if hasValue {
					return fmt.Errorf("--%s takes no value", name)
				}
			} else if !hasValue {
				if i+1 == len(args) {
					return fmt.Errorf("--%s needs a value", name)
				}
				i++
				value = args[i]
			}
			if err := fs.setFlag(f, false, value); err != nil {
				return err
			}
		case len(a) > 1 && a[0] == '-':
			// -l, -lvalue, -l value, or bool shorts grouped: -vx. A long
			// name after one dash is a mistake, not shorts: -platform.
			if name, _, _ := strings.Cut(a[1:], "="); len(name) > 1 && fs.lookup(name) != nil {
				return fmt.Errorf("-%s: did you mean --%s?", name, name)
			}
			cs := []rune(a[1:])
			for j := 0; j < len(cs); j++ {
				f := fs.lookupShort(cs[j])
				if f == nil {
					return fs.unknown("-" + string(cs[j]))
				}
				if f.bool {
					if err := fs.setFlag(f, true, ""); err != nil {
						return err
					}
					continue
				}
				value := string(cs[j+1:])
				if value == "" {
					if i+1 == len(args) {
						return fmt.Errorf("-%c needs a value", cs[j])
					}
					i++
					value = args[i]
				}
				if err := fs.setFlag(f, true, value); err != nil {
					return err
				}
				break
			}
		default:
			fs.args = append(fs.args, a)
		}
	}
	return nil
}

func (fs *flagSet) setFlag(f *cliFlag, short bool, value string) error {
	if err := f.value(value); err != nil {
		return fmt.Errorf("%s: %v", f.name(short), err)
	}
	fs.set[f.long] = true
	return nil
}

func (fs *flagSet) unknown(name string) error {
	return fmt.Errorf("unknown flag %s (asuka %s --help lists them)", name, fs.cmd)
}

// changed reports whether parse set the flag named long.
func (fs *flagSet) changed(long string) bool { return fs.set[long] }

// Args are the arguments that aren't flags.
func (fs *flagSet) Args() []string { return fs.args }

// Arg is the ith argument that isn't a flag, "" if there's none.
func (fs *flagSet) Arg(i int) string {
	if i < len(fs.args) {
		return fs.args[i]
	}
	return ""
}

func (fs *flagSet) help() {
	w := fs.out
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(w, "usage: asuka %s", fs.cmd)
	if len(fs.flags) > 0 {
		fmt.Fprint(w, " [flags]")
	}
	if fs.usage != "" {
		fmt.Fprint(w, " "+fs.usage)
	}
	fmt.Fprintln(w)
	if fs.about != "" {
		fmt.Fprintf(w, "\n%s\n", fs.about)
	}
	if len(fs.flags) > 0 {
		fmt.Fprint(w, "\nflags:\n")
		fs.printFlags(w)
	}
}

// printFlags lists the flags: their names, the value they take as their
// usage names it in backquotes, and their usage, wrapped.
func (fs *flagSet) printFlags(w io.Writer) {
	for _, f := range fs.flags {
		arg, usage := unquote(f)
		line := "      "
		if f.short != 0 {
			line = "  -" + string(f.short) + ", "
		}
		line += "--" + f.long
		if arg != "" {
			line += " <" + arg + ">"
		}
		if f.def != "" {
			usage += " (default: " + f.def + ")"
		}
		fmt.Fprintln(w, line)
		for _, l := range wrap(usage, 72) {
			fmt.Fprintln(w, "        "+l)
		}
	}
}

// unquote takes the name of f's value out of the backquotes in its
// usage, as Go's flag package does: "the `platform` to run on" names it
// platform. A flag without one names it after its long name.
func unquote(f *cliFlag) (arg, usage string) {
	usage = f.usage
	if i := strings.IndexByte(usage, '`'); i >= 0 {
		if j := strings.IndexByte(usage[i+1:], '`'); j >= 0 {
			arg = usage[i+1 : i+1+j]
			usage = usage[:i] + arg + usage[i+1+j+1:]
		}
	}
	if arg == "" && !f.bool {
		arg = f.long
	}
	return arg, usage
}

// wrap breaks s into lines of at most width, at spaces.
func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
