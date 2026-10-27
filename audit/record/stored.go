package record

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryStoredField is the framework's deferred stored-change boundary.
// Bind runs only for selected, disclosed snapshots.
// Example: FoundryStoredField{Name: "name", Type: codec.TypeText, After: true, Changed: true, Bind: bindStored}.
type FoundryStoredField struct {
	Name                             string
	Type                             codec.ParameterType
	Sensitive                        bool
	Before, After, Assigned, Changed bool
	Bind                             func(after bool) (driver.Value, error)
}

// FoundryCapture reuses capture policy with already computed lifecycle flags.
// A valid metadata capability is required before any callback.
// Example: FoundryCapture(builder, field, Automatic, sqlowner.Metadata()).
func FoundryCapture[M, K any](builder *Builder[M, K], field FoundryStoredField, disclosure Disclosure, seal sqlowner.MetadataSeal) error {
	if !seal.Valid() {
		return safeerror.NewFault(fault.Invalid, "invalid audit metadata capability")
	}
	if builder == nil || builder.seen == nil {
		return safeerror.NewFault(fault.Invalid, "audit builder is not initialized")
	}
	if err := disclosure.Validate(); err != nil {
		return builder.fail(err)
	}
	if builder.skips(field.Name, field.Assigned, field.Changed) {
		return builder.skip(field.Name)
	}
	captured, err := captureStoredField(field, disclosure)
	if err != nil {
		return builder.fail(err)
	}
	return builder.Add(Field[M]{field: captured, before: field.Before, after: field.After, excluded: disclosure == Exclude})
}
