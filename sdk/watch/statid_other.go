//go:build !windows

package watch

import "io/fs"

// statIdentity returns info: a stat carries the file's identity from the
// moment it is taken everywhere but on Windows.
func statIdentity(info fs.FileInfo) fs.FileInfo { return info }
