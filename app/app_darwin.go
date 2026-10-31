//go:build darwin && !ios

package app

import "github.com/egoist/ryui/host/darwin"

func init() { desktop = darwin.Run }
