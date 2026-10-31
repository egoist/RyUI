//go:build windows && amd64

package app

import "github.com/egoist/ryui/host/windows"

func init() { desktop = windows.Run }
