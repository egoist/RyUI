// Package template is what `asuka new` copies.
package template

import "embed"

// Files is the new project's files; names ending in .tmpl are
// text/template sources.
//
//go:embed files
var Files embed.FS
