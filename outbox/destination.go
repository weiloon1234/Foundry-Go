package outbox

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Destination is a stable routing namespace, declared once when constructing a
// producer. It is separate from a payload's schema name and version.
type Destination string

func (d Destination) Validate() error {
	if !identifier.Semantic(string(d)) {
		return safeerror.NewFault(fault.Invalid, "outbox destination requires a semantic name")
	}
	return nil
}
