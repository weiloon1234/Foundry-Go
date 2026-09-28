// Package httptoken shares the RFC HTTP token grammar across transport boundaries.
package httptoken

import "strings"

func Byte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))
}
func Valid(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !Byte(s[i]) {
			return false
		}
	}
	return true
}
