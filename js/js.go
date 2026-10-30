// Package js runs JavaScript off screen, in the platform's own engine
// (a web view that's never shown: Android's WebView, WKWebView): for
// code that only has a JavaScript form, as a site's player whose
// functions sign its video URLs.
package js

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	"github.com/egoist/nui/ui"
)

// DefaultTimeout is how long a script may run when ctx has no deadline.
const DefaultTimeout = time.Minute

// Eval runs script, a page's worth of JavaScript (statements, as a
// <script>'s), in a fresh page of its own, and returns its value as
// JSON: the last statement's value, or what the promise it returns
// resolves to (JSON null for undefined). A thrown error, or a rejected
// promise, is the error. The page is gone after: what the script set
// isn't there for the next. Not on the main thread.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Eval(ctx context.Context, script string) (json.RawMessage, error) {
	timeout := DefaultTimeout
	if d, ok := ctx.Deadline(); ok {
		timeout = time.Until(d)
	}
	if timeout <= 0 {
		return nil, context.DeadlineExceeded
	}
	args := binary.LittleEndian.AppendUint32(nil, uint32(timeout/time.Millisecond))
	args = append(args, script...)
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := ui.CallWait("js", "eval", args)
		ch <- result{b, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, &Error{Message: r.err.Error()}
		}
		if !json.Valid(r.b) {
			return nil, errors.New("js: the result isn't JSON")
		}
		return json.RawMessage(r.b), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// EvalString is Eval for a script whose value is a string.
func EvalString(ctx context.Context, script string) (string, error) {
	raw, err := Eval(ctx, script)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("js: the value isn't a string: " + string(raw))
	}
	return s, nil
}

// Error is what a script threw (its message and stack), or "timeout".
type Error struct{ Message string }

func (e *Error) Error() string { return "js: " + e.Message }
