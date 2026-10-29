// Package parser parses Markdown (CommonMark, with GitHub's extensions,
// footnotes and math) incrementally: a Document takes text appended to it,
// or edits, and parses only what they change, and while streaming it shows
// the half-written syntax at its end as what it will most likely become.
//
// It comes from MyGo's ui/markdown/parser (github.com/egoist/mygo, MIT
// License, Copyright (c) 2026 MyGo contributors).
package parser
