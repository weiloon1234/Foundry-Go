package record

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FieldValue retains the declared storage type while exposing disclosure state.
// A redacted value cannot hydrate T; JSON export remains the explicitly redacted
// audit representation rather than a partial application model.
type FieldValue[V any] struct {
	snapshot Snapshot
	decode   func(any) (V, error)
}

func (v FieldValue[V]) State() State                 { return v.snapshot.state }
func (v FieldValue[V]) Snapshot() Snapshot           { return v.snapshot }
func (FieldValue[V]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("typed audit value")) }

// Get returns absence only when the model snapshot was absent. A nullable field
// decodes to its concrete Nullable type. Redaction is an explicit error, never
// a fabricated zero field value. Each decode gives the caller its own value.
func (v FieldValue[V]) Get() (value.Optional[V], error) {
	if err := v.snapshot.validate(); err != nil {
		return value.Optional[V]{}, err
	}
	if v.snapshot.state == Absent {
		return value.Optional[V]{}, nil
	}
	if v.snapshot.state == Oversized {
		return value.Optional[V]{}, safeerror.NewFault(fault.Missing, "audit value exceeded its bound; only its digest was retained")
	}
	if v.snapshot.state != Disclosed {
		return value.Optional[V]{}, safeerror.NewFault(fault.Missing, "audit value is redacted")
	}
	var result V
	err := callback.Isolated("decode audit field", func() error {
		bound, err := v.snapshot.value.Decode()
		if err != nil {
			return err
		}
		if v.decode == nil {
			return safeerror.NewFault(fault.Invalid, "audit field codec is not initialized")
		}
		result, err = v.decode(bound)
		return err
	})
	if err != nil {
		return value.Optional[V]{}, safeerror.WrapFailure(fault.Invalid, "audit field decode failed", err)
	}
	return value.Set(result), nil
}

// FieldChange contains a stored audit field with concrete before/after types.
// The surrounding Optional returned by generated readers represents a field
// omitted by policy or absent from an older record's schema.
type FieldChange[V any] struct {
	before, after     FieldValue[V]
	assigned, changed bool
}

func (c FieldChange[V]) Before() FieldValue[V] { return c.before }
func (c FieldChange[V]) After() FieldValue[V]  { return c.after }
func (c FieldChange[V]) Assigned() bool        { return c.assigned }
func (c FieldChange[V]) Changed() bool         { return c.changed }
func (FieldChange[V]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("typed audit change"))
}

// View indexes an immutable model audit once for generated typed field readers.
// Its map is private and owned; reading many fields does not repeatedly decode
// the entire payload. A zero view is invalid. A field missing from an update
// record was neither assigned nor changed by that operation.
type View[M, K any] struct {
	_      [0]*M
	_      [0]*K
	fields map[string]capturedField
}

func (View[M, K]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit view")) }

func Inspect[M, K any](record Model[M, K]) (View[M, K], error) {
	if err := record.entry.Validate(); err != nil {
		return View[M, K]{}, err
	}
	fields := record.entry.data.fields
	view := View[M, K]{fields: make(map[string]capturedField, len(fields))}
	for _, field := range fields {
		view.fields[field.name] = field
	}
	return view, nil
}

// ReadField is a generated declaration boundary. Model adapters supply the
// column and existing codec; ordinary application code uses their named fields.
func ReadField[M, K, V any](view View[M, K], name string, c codec.Codec[V]) (value.Optional[FieldChange[V]], error) {
	return readField(view, name, c.ParameterType(), c.Decode)
}

// FoundryReadField retains a declaration's existing decoder without rebuilding a codec.
// The metadata capability is checked before the decoder is retained.
// Example: FoundryReadField(view, "name", codec.TypeText, decode, sqlowner.Metadata()).
func FoundryReadField[M, K, V any](view View[M, K], name string, kind codec.ParameterType, decode func(any) (V, error), seal sqlowner.MetadataSeal) (value.Optional[FieldChange[V]], error) {
	if !seal.Valid() {
		return value.Optional[FieldChange[V]]{}, safeerror.NewFault(fault.Invalid, "invalid audit metadata capability")
	}
	return readField(view, name, kind, decode)
}

func readField[M, K, V any](view View[M, K], name string, kind codec.ParameterType, decode func(any) (V, error)) (value.Optional[FieldChange[V]], error) {
	if view.fields == nil {
		return value.Optional[FieldChange[V]]{}, safeerror.NewFault(fault.Invalid, "audit view is not initialized")
	}
	if field, present := view.fields[name]; present {
		if field.typ != kind {
			return value.Optional[FieldChange[V]]{}, safeerror.NewFault(fault.Invalid, "audit field codec does not match its stored representation")
		}
		return value.Set(FieldChange[V]{before: FieldValue[V]{snapshot: field.before, decode: decode},
			after: FieldValue[V]{snapshot: field.after, decode: decode}, assigned: field.assigned, changed: field.changed}), nil
	}
	return value.Optional[FieldChange[V]]{}, nil
}
