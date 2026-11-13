//go:build darwin && !ios

package watch

// newPlatform starts FSEvents, or kqueue when the tests ask (or when
// FSEvents can't be loaded, which a Mac always has).
func newPlatform(notify func(notice), opts Options) (native, error) {
	if opts.backend == "kqueue" {
		return newKqueue(notify)
	}
	n, err := newFSEvents(notify)
	if err != nil {
		return newKqueue(notify)
	}
	return n, nil
}
