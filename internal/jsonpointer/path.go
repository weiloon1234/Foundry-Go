// Package jsonpointer owns JSON Pointer segment escaping for framework errors.
package jsonpointer

import "strings"

var escape = strings.NewReplacer("~", "~0", "/", "~1")

// Append appends one literal segment to an existing pointer. Callers validate
// names and own path-size bounds; the empty parent denotes the document root.
func Append(parent, segment string) string { return parent + "/" + escape.Replace(segment) }
