package orm

import (
	"database/sql/driver"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Changes contains persisted snapshots, excluding relations and ignored fields.
// Example: original, exists := changes.Before().Get().
type Changes[M any] struct {
	_     [0]*M
	state *changeState
}

type changeState struct {
	table             *tableCore
	before, after     unsafe.Pointer
	fields            map[*columnCore]changeFlags
	assigned, changed bool
	operation         value.Optional[lifecycle.Operation]
}

type changeFlags struct{ assigned, changed bool }

// Before returns an owned persisted original, absent for creation. Example: changes.Before().Get().
func (c Changes[M]) Before() value.Optional[M] {
	if c.state == nil {
		return value.Optional[M]{}
	}
	return optionalSnapshot[M](c.state.table, c.state.before)
}

// After returns an owned stored result, absent for physical deletion. Example: changes.After().Get().
func (c Changes[M]) After() value.Optional[M] {
	if c.state == nil {
		return value.Optional[M]{}
	}
	return optionalSnapshot[M](c.state.table, c.state.after)
}

// Operation identifies an automatic write; standalone comparisons have none. Example: changes.Operation().Get().
func (c Changes[M]) Operation() value.Optional[lifecycle.Operation] {
	if c.state == nil {
		return value.Optional[lifecycle.Operation]{}
	}
	return c.state.operation
}

// Assigned reports effective assignments, including managed fields. Example: changes.Assigned().
func (c Changes[M]) Assigned() bool { return c.state != nil && c.state.assigned }

// Changed reports canonical stored differences. Example: changes.Changed().
func (c Changes[M]) Changed() bool { return c.state != nil && c.state.changed }

// String redacts snapshots. Example: fmt.Sprint(changes).
func (Changes[M]) String() string { return "orm changes" }

// GoString redacts snapshots. Example: fmt.Sprintf("%#v",changes).
func (Changes[M]) GoString() string { return "orm changes" }

// ChangeOf selects a required persisted field with its stored type. Example: ChangeOf(changes, NumberExpr(Orders.TotalCents)).
func ChangeOf[M, V any](changes Changes[M], field interface{ ormColumn(*M) columnData[V] }) (lifecycle.FieldChange[V], error) {
	c := field.ormColumn(nil)
	if err := changeField(changes.state, c.core, false); err != nil {
		return lifecycle.FieldChange[V]{}, err
	}
	return compareTypedField(c.core, c.policy, changes.state)
}

// NullableChangeOf retains SQL NULL independently of model existence. Example: NullableChangeOf(changes, NullableInputTextExpr(Members.Note)).
func NullableChangeOf[M, V any](changes Changes[M], field interface{ ormColumn(*M) columnData[V] }) (lifecycle.FieldChange[value.Nullable[V]], error) {
	c := field.ormColumn(nil)
	if err := changeField(changes.state, c.core, true); err != nil {
		return lifecycle.FieldChange[value.Nullable[V]]{}, err
	}
	policy, ok := c.core.nullablePolicy.(nullableFieldPolicy[V])
	if !ok {
		return lifecycle.FieldChange[value.Nullable[V]]{}, invalid("change field has a different stored type")
	}
	return compareTypedField(c.core, policy.policy, changes.state)
}
func changeField(state *changeState, field *columnCore, nullable bool) error {
	if state == nil || field == nil || field.owner != state.table || field.metadata.Nullable != nullable {
		return invalid("change requires a matching persisted column")
	}
	return nil
}
func compareTypedField[V any](field *columnCore, c lifecycle.FieldPolicy[V], state *changeState) (lifecycle.FieldChange[V], error) {
	var before, after value.Optional[V]
	if state.before != nil {
		before = value.Set(*(*V)(unsafe.Add(state.before, field.offset)))
	}
	if state.after != nil {
		after = value.Set(*(*V)(unsafe.Add(state.after, field.offset)))
	}
	return lifecycle.CompareFieldPolicy(c, before, after, state.fields[field].assigned)
}

// One canonical codec lets capture validate/compare stored driver values without
// instantiating typed field-change APIs for every declared read-only column.
var storedChangeCodec = codec.New(func(v driver.Value) (driver.Value, error) { return v, nil }, func(v any) (driver.Value, error) { return v, nil })

func compareStoredField(c *columnCore, before, after unsafe.Pointer, assigned bool) (bool, error) {
	if c.equal != nil {
		if assigned && after == nil {
			return false, invalid("deleted field cannot be assigned")
		}
		if before == nil && after == nil {
			return false, invalid("field change requires a model snapshot")
		}
		if before == nil || after == nil {
			return true, nil
		}
		return !c.equal(before, after), nil
	}
	var left, right value.Optional[driver.Value]
	if before != nil {
		v, err := c.get(before)
		if err != nil {
			return false, lifecore.Failure("compare original model field failed", err)
		}
		left = value.Set(v)
	}
	if after != nil {
		v, err := c.get(after)
		if err != nil {
			return false, lifecore.Failure("compare resulting model field failed", err)
		}
		right = value.Set(v)
	}
	change, err := lifecycle.CompareField(storedChangeCodec, left, right, assigned)
	return change.Changed(), err
}

// Compare compares stored snapshots without running hooks, accessors or mutators.
// Example: Compare(Orders.Table, value.Set(before), value.Set(after), orm.NumberSet(Orders.TotalCents, 0)).
func Compare[M any, K comparable](table Table[M, K], before, after value.Optional[M], assigned ...Assignment[M]) (Changes[M], error) {
	if err := tableError(table.core); err != nil {
		return Changes[M]{}, err
	}
	as := make([]querycore.MutationAssignment, 0, len(assigned))
	seen := map[string]bool{}
	for _, a := range assigned {
		if a.err != nil {
			return Changes[M]{}, a.err
		}
		if a.owner != table.core || a.adjust != nil || seen[a.core.Field.Column] {
			return Changes[M]{}, invalid("comparison requires unique persisted assignments from its table")
		}
		seen[a.core.Field.Column] = true
		as = append(as, a.core)
	}
	var left, right unsafe.Pointer
	if v, ok := before.Get(); ok {
		left = unsafe.Pointer(&v)
	}
	if v, ok := after.Get(); ok {
		right = unsafe.Pointer(&v)
	}
	state, err := captureChanges(table.core, left, right, as, value.Optional[lifecycle.Operation]{})
	return Changes[M]{state: state}, err
}
func snapshotPointer(table *tableCore, src unsafe.Pointer) unsafe.Pointer {
	if src == nil {
		return nil
	}
	dst := reflect.New(table.typ).UnsafePointer()
	for _, c := range table.columns {
		c.snapshot(dst, src)
	}
	return dst
}
func optionalSnapshot[M any](table *tableCore, src unsafe.Pointer) value.Optional[M] {
	ptr := snapshotPointer(table, src)
	if ptr == nil {
		return value.Optional[M]{}
	}
	return value.Set(*(*M)(ptr))
}
func captureChanges(table *tableCore, before, after unsafe.Pointer, assigned []querycore.MutationAssignment, operation value.Optional[lifecycle.Operation]) (*changeState, error) {
	before, after = snapshotPointer(table, before), snapshotPointer(table, after)
	state := &changeState{table: table, before: before, after: after, fields: make(map[*columnCore]changeFlags, len(table.columns)), operation: operation}
	for _, c := range table.columns {
		present := false
		for _, a := range assigned {
			if a.Field.Column == c.metadata.Name {
				present = true
				break
			}
		}
		changed, err := compareStoredField(c, before, after, present)
		if err != nil {
			return nil, err
		}
		if c == table.primary && before != nil && after != nil && (changed || present) {
			return nil, invalid("model change cannot replace or assign its existing primary key")
		}
		state.fields[c] = changeFlags{assigned: present, changed: changed}
		state.assigned = state.assigned || present
		state.changed = state.changed || changed
	}
	return state, nil
}

// EncryptedChangeOf selects an encrypted field using decoded equality, without binding ciphertext.
// Example: EncryptedChangeOf(changes, Vaults.Secret).
func EncryptedChangeOf[M any, V encrypted.Value[V]](changes Changes[M], field Encrypted[M, V]) (lifecycle.FieldChange[V], error) {
	c := field.encrypted
	if err := changeField(changes.state, c.core, false); err != nil {
		return lifecycle.FieldChange[V]{}, err
	}
	return compareTypedField(c.core, c.policy, changes.state)
}

// NullableEncryptedChangeOf retains nullable encrypted snapshots without exposing predicates.
// Example: NullableEncryptedChangeOf(changes, Vaults.Backup).
func NullableEncryptedChangeOf[M any, V encrypted.Value[V]](changes Changes[M], field NullableEncrypted[M, V]) (lifecycle.FieldChange[value.Nullable[V]], error) {
	c := field.encrypted.base
	if err := changeField(changes.state, c.core, true); err != nil {
		return lifecycle.FieldChange[value.Nullable[V]]{}, err
	}
	policy, ok := c.core.nullablePolicy.(nullableFieldPolicy[V])
	if !ok {
		return lifecycle.FieldChange[value.Nullable[V]]{}, invalid("change field has a different stored type")
	}
	return compareTypedField(c.core, policy.policy, changes.state)
}
