// Package lifecycle defines typed model lifecycle data shared by generated
// hooks, observers and audit integrations.
package lifecycle

import (
	"bytes"
	"database/sql/driver"
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FieldChange preserves one field's snapshots and distinguishes assignment from
// a change in its stored value. An absent snapshot means the model did not exist;
// a present Nullable value can independently represent a NULL field. The zero
// FieldChange contains no snapshots and does not represent a completed write.
// Values follow their codec's ownership policy. Built-in binary fields own
// their buffers; custom values containing references must be treated as read-only.
type FieldChange[V any] struct {
	before, after value.Optional[V]
	assigned      bool
	changed       bool
	// Share immutable codec policy across copies of one captured change. Keeping
	// function-valued codec internals directly in this value would make even
	// scalar changes non-comparable and break copied observer snapshots.
	policy *FieldPolicy[V]
}

func (c FieldChange[V]) Before() value.Optional[V] {
	if c.policy == nil {
		return c.before
	}
	return cloneOptional(c.policy, c.before)
}
func (c FieldChange[V]) After() value.Optional[V] {
	if c.policy == nil {
		return c.after
	}
	return cloneOptional(c.policy, c.after)
}
func (c FieldChange[V]) Assigned() bool { return c.assigned }
func (c FieldChange[V]) Changed() bool  { return c.changed }

// String and GoString keep snapshot values out of routine diagnostics.
func (FieldChange[V]) String() string     { return "model field change" }
func (c FieldChange[V]) GoString() string { return c.String() }

// CompareField constructs change data from stored snapshots, using the field's
// codec for validation and canonical comparison. It never runs write mutators,
// read accessors or database operations. Generated lifecycle adapters supply the
// locked original and fully hydrated write result, with the effective assignment
// flag. Creation/deletion changes existence even for a NULL field; an assignment
// whose canonical value is unchanged still reports Assigned.
//
// At least one snapshot is required. A deletion cannot assign a value. Codecs
// must bind canonical finite driver scalars, byte buffers or timestamps. Interval
// codecs preserve calendar components; SQL relation-key equality is deliberately
// a separate contract. A failure returns no partial change data.
func CompareField[V any](c codec.Codec[V], before, after value.Optional[V], assigned bool) (FieldChange[V], error) {
	return CompareFieldPolicy(FieldPolicyOf(c), before, after, assigned)
}

// CompareFieldPolicy reuses declared codec metadata without reconstructing a codec.
// Its snapshots and errors follow CompareField. Example: CompareFieldPolicy(policy, value.Set(before), value.Set(after), true).
func CompareFieldPolicy[V any](policy FieldPolicy[V], before, after value.Optional[V], assigned bool) (FieldChange[V], error) {
	if policy.bind == nil || policy.clone == nil {
		return FieldChange[V]{}, fault.New(fault.Invalid, "field change requires a declared codec policy")
	}

	if !before.IsSet() && !after.IsSet() {
		return FieldChange[V]{}, fault.New(fault.Invalid, "field change requires a model snapshot")
	}
	if assigned && !after.IsSet() {
		return FieldChange[V]{}, fault.New(fault.Invalid, "deleted field cannot be assigned")
	}
	same, err := sameField(policy, before, after)
	if err != nil {
		return FieldChange[V]{}, err
	}
	return FieldChange[V]{before: cloneOptional(&policy, before), after: cloneOptional(&policy, after), assigned: assigned, policy: &policy,
		changed: before.IsSet() != after.IsSet() || !same}, nil
}

// sameField compares decoded values when the codec defines equality, because
// such a codec binds non-deterministically (for example a fresh encryption
// nonce), and otherwise compares canonical bound values.
func sameField[V any](c FieldPolicy[V], before, after value.Optional[V]) (bool, error) {
	if c.equal != nil {
		left, leftSet := before.Get()
		right, rightSet := after.Get()
		if !leftSet || !rightSet {
			// Creation or deletion already changes the field.
			return true, nil
		}
		return c.equal(left, right), nil
	}
	left, err := bindSnapshot(c, before)
	if err != nil {
		return false, lifecore.Failure("compare original model field failed", err)
	}
	right, err := bindSnapshot(c, after)
	if err != nil {
		return false, lifecore.Failure("compare resulting model field failed", err)
	}
	return sameStoredValue(left, right)
}

func bindSnapshot[V any](c FieldPolicy[V], snapshot value.Optional[V]) (driver.Value, error) {
	v, present := snapshot.Get()
	if !present {
		return nil, nil
	}
	return c.bind(v)
}

func sameStoredValue(left, right driver.Value) (bool, error) {
	// Validate both sides even when their representations differ, so malformed
	// custom codec output cannot become an apparently valid "changed" result.
	for _, item := range []driver.Value{left, right} {
		switch v := item.(type) {
		case nil, bool, int64, string, []byte, time.Time:
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false, fault.New(fault.Invalid, "field change requires a finite number")
			}
		default:
			return false, fault.New(fault.Invalid, "unsupported field change codec representation")
		}
	}
	switch v := left.(type) {
	case nil:
		return right == nil, nil
	case []byte:
		other, ok := right.([]byte)
		return ok && (v == nil) == (other == nil) && bytes.Equal(v, other), nil
	case time.Time:
		other, ok := right.(time.Time)
		return ok && v.Round(0).Equal(other.Round(0)), nil
	default:
		// The validated remaining alternatives are comparable scalar types.
		return left == right, nil
	}
}
