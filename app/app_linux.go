//go:build linux && !android

package app

import "github.com/egoist/asuka/host/linux"

// Linux: GTK 4 and libadwaita, in this process.
func init() { desktop = linux.Run }
