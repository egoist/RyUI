//go:build !js

package app

import "github.com/egoist/ryui/internal/remote"

func init() { remoteRun = remote.Run }
