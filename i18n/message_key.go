package i18n

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// MessageKey identifies a UI message independently of its locale. Generated
// message declarations and feature labels share this identity; a cast is an
// explicit dynamic-key boundary and does not prove catalog membership.
type MessageKey string

func (k MessageKey) Validate() error {
	if !identifier.Semantic(string(k)) {
		return fault.New(fault.Invalid, "invalid message key")
	}
	return nil
}
