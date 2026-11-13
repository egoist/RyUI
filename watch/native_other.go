//go:build !darwin && !linux && !windows && !js

package watch

// newPlatform: no file watching here.
func newPlatform(notify func(notice), opts Options) (native, error) {
	return nil, ErrUnsupported
}
