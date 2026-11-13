//go:build darwin && !ios

package app

import "github.com/egoist/asuka/host/darwin"

func init() { desktop = darwin.Run }
