package contract

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// ResolveJSONKey evaluates a typed declaration factory during construction.
// It contains panic/Goexit and rejects an invalid returned key. Generated custom
// key bindings use it without invoking the factory during request handling.
func ResolveJSONKey[K comparable](factory func() JSONKey[K]) JSONKey[K] {
	if factory == nil {
		return JSONKey[K]{err: invalidSchema()}
	}
	var key JSONKey[K]
	err := callback.Isolated("resolve JSON key contract", func() error {
		key = factory()
		return key.Validate()
	})
	if err != nil {
		return JSONKey[K]{err: fault.Wrap(fault.Invalid, "invalid JSON key contract", err)}
	}
	return key
}
