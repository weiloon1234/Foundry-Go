// Package emaildiag marks framework-owned email classifications at their source.
package emaildiag

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

const (
	Construction = "email construction failed"
	Permanent    = "email permanently rejected"
	Transient    = "email temporarily unavailable before acceptance"
	Ambiguous    = "email acceptance is unknown"
)

// Failure snapshots only built-in classification text without calling methods.
// Application-defined string classifications remain unmarked.
func Failure[K interface {
	~string
	error
}](kind K) error {
	switch string(kind) {
	case Construction, Permanent, Transient, Ambiguous:
		return safeerror.Owned(string(kind), kind)
	default:
		return kind
	}
}
