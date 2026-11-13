// Package web is Asuka's web host's JavaScript (src): the DOM host the
// app's Go code (WebAssembly) drives through host/web, its generated
// decoder (src/ops.js), the loader (src/asuka.js) and the stylesheet. The
// Asuka CLI serves and ships it with each web app.
package web

import "embed"

// Src is the host's files, as src/: host.js, asuka.js, style.css...
//
//go:embed src
var Src embed.FS

// SQLite is SQLite's own WebAssembly build, as sqlite/: sqlite3.mjs and
// sqlite3.wasm, which src/sqlite.js loads for the apps that import
// asuka/sqlite (the CLI ships it with them only). SQLite 3.54.1, from
// https://sqlite.org/2026/sqlite-wasm-3540100.zip (SHA3-256
// 87e24f6a4588692310b7942dc2fa7c7275bb9ba2e01d553e4115aa4a904a0050),
// its jswasm/ files unchanged. SQLite is in the public domain.
//
//go:embed sqlite
var SQLite embed.FS
