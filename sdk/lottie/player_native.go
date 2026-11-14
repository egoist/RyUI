//go:build !windows && !(linux && !android)

package lottie

import "github.com/egoist/asuka/sdk/lottie/internal/lottieplayer"

// player is the platform's own Lottie player (lottie-ios, lottie-android,
// lottie-web).
var player = lottieplayer.Player
