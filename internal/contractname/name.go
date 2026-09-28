// Package contractname provides stable ASCII names for generated schema symbols.
package contractname

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Schemas keeps names stable when unrelated declarations are added. The full
// source identity participates in the suffix; any collision fails explicitly.
func Schemas(types []contract.Type) (map[contract.TypeID]string, error) {
	result, used := make(map[contract.TypeID]string, len(types)), make(map[string]contract.TypeID)
	for _, typ := range types {
		name := Symbol(string(typ.ID))
		if old, found := used[name]; found && old != typ.ID {
			return nil, fault.New(fault.Conflict, "generated schema names collide")
		}
		used[name], result[typ.ID] = typ.ID, name
	}
	return result, nil
}

func Symbol(identity string) string {
	tail := identity
	if i := strings.LastIndexAny(tail, "/."); i >= 0 {
		tail = tail[i+1:]
	}
	var name strings.Builder
	for _, r := range tail {
		if name.Len() >= 40 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			name.WriteRune(r)
		} else {
			name.WriteByte('_')
		}
	}
	stem := strings.Trim(name.String(), "_")
	if stem == "" {
		stem = "Type"
	}
	if stem[0] >= '0' && stem[0] <= '9' {
		stem = "Type_" + stem
	}
	digest := sha256.Sum256([]byte(identity))
	return strings.ToUpper(stem[:1]) + stem[1:] + "_" + hex.EncodeToString(digest[:8])
}
