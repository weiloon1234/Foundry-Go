package mfa

import (
	"encoding/json"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// MarshalJSON preserves the public enrollment identifier; it is not a secret or
// proof. The model owner remains part of its Go type and generated contract.
func (id EnrollmentID[M]) MarshalJSON() ([]byte, error) {
	if id.IsZero() {
		return nil, safeerror.NewFault(fault.Invalid, "enrollment ID is empty")
	}
	return json.Marshal(id.String())
}
func (id *EnrollmentID[M]) UnmarshalJSON(data []byte) error {
	if id == nil {
		return safeerror.NewFault(fault.Invalid, "enrollment ID destination is missing")
	}
	*id = EnrollmentID[M]{}
	var text string
	if len(data) > 256 {
		return safeerror.NewFault(fault.Invalid, "invalid enrollment ID input")
	}
	if err := json.Unmarshal(data, &text); err != nil {
		return safeerror.NewFault(fault.Invalid, "invalid enrollment ID input")
	}
	parsed, err := ParseEnrollmentID[M](text)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
func (EnrollmentID[M]) JSONContract() contract.JSON[EnrollmentID[M]] {
	typ := reflect.TypeFor[EnrollmentID[M]]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[EnrollmentID[M]](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind, Format: contract.UUIDFormat}}})
}
