//go:build windows || (linux && !android)

package greeter

// On Windows and Linux the package's native code is Go.
func init() {
	implementGreeter(func() (Greeter, error) { return goGreeter{}, nil })
}

type goGreeter struct{}

func (goGreeter) Hello(name string) (string, error) { return "hello " + name + " from Go", nil }
