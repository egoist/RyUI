//go:build !unix && !windows && !(js && wasm)

package crashrun

import "os"

func lock(f *os.File)                  {}
func Alive(path string) bool           { return false }
func redirectStderr(path string)       {}
func Lock(path string) (unlock func()) { return func() {} }
