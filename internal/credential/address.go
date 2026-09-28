package credential

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

func ValidateAddress(namespace keyspace.Namespace, guard, provider, table string) error {
	if err := namespace.Validate(); err != nil {
		return err
	}
	if !identifier.Semantic(guard) || !identifier.Semantic(provider) || !sqlname.Table(table) {
		return fault.New(fault.Invalid, "invalid credential address")
	}
	return nil
}
func AddressKey(namespace keyspace.Namespace, guard, provider, table string) (string, error) {
	if err := ValidateAddress(namespace, guard, provider, table); err != nil {
		return "", err
	}
	return key([]string{namespace.Application, namespace.Environment, guard, provider, table}), nil
}
func SubjectKey(scope, table string, identity model.Identity) (string, error) {
	if err := identity.Validate(); err != nil {
		return "", err
	}
	if identity.ModelName() != table {
		return "", fault.New(fault.Invalid, "credential identity belongs to a different model")
	}
	encoded, err := identity.KeyJSON()
	if err != nil {
		return "", err
	}
	return key([]string{scope, encoded}), nil
}
func key(parts []string) string {
	// JSON components are length-delimited: separators inside values cannot collide.
	return Fingerprint(parts...).Hex()
}

// Fingerprint hashes exact ordered UTF-8 components using the same unambiguous
// encoding as credential addresses. Callers validate their own size/shape limits.
func Fingerprint(parts ...string) Digest {
	encoded, _ := json.Marshal(parts)
	return Digest{bytes: sha256.Sum256(encoded)}
}
