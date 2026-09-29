// Package sqlname owns the common identifier grammar used at SQL declaration
// boundaries. SQL compilers must still quote identifiers and bind data values.
package sqlname

import "strings"

// MaxBytes matches PostgreSQL's default identifier storage bound. Declarations
// above it must fail instead of being silently truncated to another identifier.
const MaxBytes = 63

// Valid accepts [A-Za-z_][A-Za-z0-9_]* of at most MaxBytes bytes. It is checked
// on every compiled statement, so it scans bytes instead of running a regexp.
func Valid(name string) bool {
	if name == "" || len(name) > MaxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Table accepts one identifier or a schema-qualified schema.table pair.
func Table(name string) bool {
	schema, table, qualified := strings.Cut(name, ".")
	if !qualified {
		return Valid(name)
	}
	return Valid(schema) && Valid(table)
}
