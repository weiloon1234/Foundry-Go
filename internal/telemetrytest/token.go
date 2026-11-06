// Package telemetrytest gates synchronous telemetry flushing to framework test helpers.
package telemetrytest

// Token authorizes a test-only capture and flush. Its zero value is invalid.
type Token struct{ granted bool }

// Grant issues a token to framework test helpers.
func Grant() Token { return Token{granted: true} }

// Valid reports whether Grant issued the token.
func (t Token) Valid() bool { return t.granted }
