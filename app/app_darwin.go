//go:build darwin && !ios

package app

import "github.com/egoist/nui/host/darwin"

func init() { desktop = darwin.Run }
