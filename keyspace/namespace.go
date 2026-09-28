// Package keyspace owns namespace syntax and typed key codecs shared by infrastructure features.
package keyspace

import "github.com/weiloon1234/Foundry-Go/fault"

// Namespace separates applications and environments sharing a backend.
// Components contain 1–128 ASCII letters, digits, dots, underscores or hyphens.
type Namespace struct{ Application, Environment string }

func (n Namespace) Validate() error {
	if !ValidName(n.Application) || !ValidName(n.Environment) {
		return fault.New(fault.Invalid, "invalid keyspace application or environment")
	}
	return nil
}

// ValidName reports whether a bounded namespace/family component is valid.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// MaxKeyBytes bounds a logical key before hashing; features may impose a lower limit.
const MaxKeyBytes = 4096
