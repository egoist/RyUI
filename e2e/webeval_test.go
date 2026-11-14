//go:build e2e

package e2e

import "testing"

// webEval runs on a page whose Content Security Policy blocks eval
// (e2e/testdata/webeval): EvalJSResult still answers with the last
// statement's value, as JSON, and with what a script threw; a syntax
// error is an error. A promise isn't awaited there: iOS says so, and
// Android's WebView answers with the promise's JSON, {}. On a page that
// doesn't block eval, a promise is awaited.
func webEval(t *testing.T, s screen, promise string) {
	waitFor(t, s, []string{
		"status: done",
		`open: "Open7"`,
		`title: "Locked"`,
		"eval: error eval",
		"sum: 6",
		`object: {"a":[1,"x",null]}`,
		"var: null",
		"kept: 5",
		"throw: error nope",
		"syntax: error syntax",
		"promise: " + promise,
	})
}

func TestWebEvalIOS(t *testing.T) {
	webEval(t, iosApp(t, "e2e/testdata/webeval", "dev.asuka.webeval"), "error policy")
}

func TestWebEvalAndroid(t *testing.T) {
	webEval(t, androidApp(t, "e2e/testdata/webeval", "dev.asuka.webeval"), "{}")
}
