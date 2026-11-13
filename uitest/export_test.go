package uitest

import "testing"

// SetT sends a's failures to t.
func (a *App) SetT(t testing.TB) { a.t = t }
