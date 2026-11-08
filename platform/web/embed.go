// Package web is RyUI's web host's JavaScript (src): the DOM host the
// app's Go code (WebAssembly) drives through host/web, its generated
// decoder (src/ops.js), the loader (src/ryui.js) and the stylesheet. The
// RyUI CLI serves and ships it with each web app.
package web

import "embed"

// Src is the host's files, as src/: host.js, ryui.js, style.css...
//
//go:embed src
var Src embed.FS
