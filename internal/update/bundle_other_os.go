package update

import "errors"

// Staged is a Mac app's staged update (bundle.go); Windows has none.
type Staged struct{}

// StageBundle stages a Mac app's bundle; Windows has none.
func StageBundle(work, bundle, target string, pid int) (*Staged, error) {
	return nil, errors.New("app bundles are macOS's")
}

func (s *Staged) Relaunch(argv []string) error { return errors.New("app bundles are macOS's") }
func (s *Staged) Target() string               { return "" }
func (s *Staged) Cancel()                      {}

func processAlive(pid int) bool { return false }
