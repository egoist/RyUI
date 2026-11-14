//go:build windows || (linux && !android)

package lottie

// player is package render's, drawn with ui.Canvas: Windows and Linux
// have no Lottie player to call.
var player = canvasPlayer
