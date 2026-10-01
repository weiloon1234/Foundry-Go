// Package lifecycle defines typed model lifecycle data shared by generated
// hooks, observers and audit integrations.
package lifecycle

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
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
	// Share the immutable codec across copies of one captured change. Keeping
	// function-valued codec internals directly in this value would make even
	// scalar changes non-comparable and break copied observer snapshots.
	codec *codec.Codec[V]
}

func (c FieldChange[V]) Before() value.Optional[V] {
	if c.codec == nil {
		return c.before
	}
	return c.codec.CloneOptional(c.before)
}
func (c FieldChange[V]) After() value.Optional[V] {
	if c.codec == nil {
		return c.after
	}
	return c.codec.CloneOptional(c.after)
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
	if !before.IsSet() && !after.IsSet() {
		return FieldChange[V]{}, fault.New(fault.Invalid, "field change requires a model snapshot")
	}
	if assigned && !after.IsSet() {
		return FieldChange[V]{}, fault.New(fault.Invalid, "deleted field cannot be assigned")
	}
	same, err := sameField(c, before, after)
	if err != nil {
		return FieldChange[V]{}, err
	}
	return FieldChange[V]{before: c.CloneOptional(before), after: c.CloneOptional(after), assigned: assigned, codec: &c,
		changed: before.IsSet() != after.IsSet() || !same}, nil
}

// sameField compares decoded values when the codec defines equality, because
// such a codec binds non-deterministically (for example a fresh encryption
// nonce), and otherwise compares canonical bound values.
func sameField[V any](c codec.Codec[V], before, after value.Optional[V]) (bool, error) {
	if c.ComparesValues() {
		left, leftSet := before.Get()
		right, rightSet := after.Get()
		if !leftSet || !rightSet {
			// Creation or deletion already changes the field.
			return true, nil
		}
		same, _ := c.Equal(left, right)
		return same, nil
	}
	left, err := bindSnapshot(c, before)
	if err != nil {
		return false, fmt.Errorf("compare original model field: %w", err)
	}
	right, err := bindSnapshot(c, after)
	if err != nil {
		return false, fmt.Errorf("compare resulting model field: %w", err)
	}
	return sameStoredValue(left, right)
}

func bindSnapshot[V any](c codec.Codec[V], snapshot value.Optional[V]) (driver.Value, error) {
	v, present := snapshot.Get()
	if !present {
		return nil, nil
	}
	return c.Bind(v)
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
