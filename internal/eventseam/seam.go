// Package eventseam gates the event bus interception seam to framework test
// helpers (testkit/events). Code outside this module cannot import it, so it
// cannot obtain a valid Token and silently suppress production listeners.
package eventseam

// Token authorizes one events.Bus.Intercept call. The zero Token is invalid.
type Token struct{ granted bool }

// Grant issues a valid token to framework test helpers.
func Grant() Token { return Token{granted: true} }

// Valid reports whether the token was issued by Grant.
func (t Token) Valid() bool { return t.granted }
