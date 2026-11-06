// Package attachmenttest gates backing-object removal to framework test helpers.
package attachmenttest

// Token authorizes a test-only storage mutation. Its zero value is invalid.
type Token struct{ granted bool }

// Grant issues a token to framework test helpers.
func Grant() Token { return Token{granted: true} }

// Valid reports whether Grant issued the token.
func (t Token) Valid() bool { return t.granted }
