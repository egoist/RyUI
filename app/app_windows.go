//go:build windows && amd64

package app

import "github.com/egoist/nui/host/windows"

func init() { desktop = windows.Run }
