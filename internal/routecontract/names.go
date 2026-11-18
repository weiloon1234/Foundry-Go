// Package routecontract owns the names shared by transport generation and route tooling.
package routecontract

import "strings"

// HelperName removes one terminal transport-role suffix, then appends Body or Response.
// It never resolves a collision by inventing another public spelling.
func HelperName(name, suffix string) string {
	if strings.HasSuffix(name, "Request") {
		name = strings.TrimSuffix(name, "Request")
	} else {
		name = strings.TrimSuffix(name, "Resource")
	}
	return name + suffix
}
