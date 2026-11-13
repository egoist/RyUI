//go:build windows

package app

import "github.com/egoist/asuka/host/windows"

func init() { desktop = windows.Run }
