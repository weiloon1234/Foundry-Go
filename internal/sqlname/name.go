// Package sqlname owns the common identifier grammar used at SQL declaration
// boundaries. SQL compilers must still quote identifiers and bind data values.
package sqlname

import (
	"regexp"
	"strings"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MaxBytes matches PostgreSQL's default identifier storage bound. Declarations
// above it must fail instead of being silently truncated to another identifier.
const MaxBytes = 63

func Valid(name string) bool { return len(name) <= MaxBytes && identifier.MatchString(name) }
func Table(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !Valid(part) {
			return false
		}
	}
	return true
}
