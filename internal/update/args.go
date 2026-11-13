package update

import "strings"

// OpenArgs returns the command that opens the app bundle with the
// arguments args, through Launch Services.
func OpenArgs(bundle string, args []string) []string {
	argv := []string{"/usr/bin/open", bundle}
	if len(args) > 0 {
		argv = append(append(argv, "--args"), args...)
	}
	return argv
}

// SetArgs returns the command of /bin/sh that makes args its positional
// parameters.
func SetArgs(args []string) string {
	var b strings.Builder
	b.WriteString("set --")
	for _, a := range args {
		b.WriteString(" '")
		b.WriteString(strings.ReplaceAll(a, "'", `'\''`))
		b.WriteString("'")
	}
	b.WriteString("\n")
	return b.String()
}
