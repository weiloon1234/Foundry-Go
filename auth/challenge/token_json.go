package challenge

import (
	"encoding/json"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// UnmarshalJSON accepts only a canonical opaque challenge. It resets a previous
// value on failure, never exposes input in errors, and does not establish model
// existence, purpose, validity or authority. Consumption checks those in storage.
func (t *Token[M, P]) UnmarshalJSON(data []byte) error {
	if t == nil {
		return fault.New(fault.Invalid, "challenge token destination is missing")
	}
	*t = Token[M, P]{}
	var raw string
	if len(data) > 512 {
		return fault.New(fault.Invalid, "invalid challenge token input")
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fault.New(fault.Invalid, "invalid challenge token input")
	}
	parsed, err := ParseToken[M, P](secret.New(raw))
	if err != nil {
		return fault.New(fault.Invalid, "invalid challenge token input")
	}
	*t = parsed
	return nil
}

// JSONContract lets generated request DTOs retain both model and purpose. Its
// wire value is a string, with native parsing and redacted output. A contract's
// model/purpose name describes the Go declaration, not data claimed by a client.
func (Token[M, P]) JSONContract() contract.JSON[Token[M, P]] {
	typ := reflect.TypeFor[Token[M, P]]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[Token[M, P]](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}
