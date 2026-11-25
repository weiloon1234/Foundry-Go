# Typed model field mutators

<a id="agent-entry"></a>
## Agent entry

- Transform: [Mutation inputs](#recipe-mutate-field)

<a id="recipe-mutate-field"></a>
## Assign mutation inputs

Further reading: [Bulk writes](#set-based-and-conflict-writes).

A mutation input column separates the value accepted by writes from the value stored in the model. An input assignment retains input until persistence; reads and predicates use the stored type. Declare `input=Type` even when input and stored types are identical.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `database/orm`.

The [complete declaration](model-declarations.md) uses a distinct `SlugInput` with a value-receiver `MutateSlug` method. `Member` is the application's model; `Members` is its generated table; `coremodels` is the application's package declaring `EmailInput`, the input type of `Member.Email`, which is stored as a different type. `emailInput` builds the assignment:

<!-- compiled-source: tests/fixtures/goway/models/mutators/guide_examples.go#inputs-after -->
```go
func emailInput(input coremodels.EmailInput) orm.Assignment[Member] {
	return orm.Set(Members.Email, input)
}
```

The model declaration binds input, codec and accessor types. With `input=Type`, the generator selects the matching value-receiver `Mutate<Field>` method. To use an explicit transform, add `mutate=name` for a package-local `func(Input) (Stored, error)`. Do not declare both.

## Declaration validation

`Mutate<Field>` names a persisted Go field, independently of its SQL column tag. It requires a value receiver, one concrete non-null input, the field's exact scalar stored result, and `error`. A nullable stored field still uses a scalar input/result transform. Unknown fields, pointer receivers, variadic methods, interface inputs and invalid results fail startup validation.

The receiver is a zero-valued type marker. Transform the argument rather than reading receiver fields. Keep mutators pure and free of I/O; cross-field and transactional work belongs in [hooks](model-hooks.md). A field with a conventional mutator and no input tag fails generation with `Mutate<Field> requires input`. `application.WithTables` checks these contracts before acquiring resources.

## Different input and stored types

Omitted fields skip the transform, including database defaults and untouched update fields. `orm.SetNull(column)` preserves SQL NULL for a nullable input text column and skips a nullable field's scalar transform. Zero, false and empty string are supplied inputs and do run it.

Before-write hooks read `value.Optional[Input]` through `WriteGet` and `value.Optional[value.Nullable[Input]]` through `WriteNullableGet`. `WriteSet` replaces inputs; `WriteUnset` restores omission. After-write changes contain stored types, never fresh inputs.

An input primary key uses `member=ID,key-role=input,input=Type` on the key field. Stored-key lookup types remain unchanged. A distinct input disables automatic UUID synthesis: supply input explicitly or use a database-owned default. An input key cannot also declare a primary insert-select role.

This is the complete input-key declaration; its conventional mutator returns the
exact stored key type.

<!-- compiled-source: tests/fixtures/goway/models/mutators/key.go -->
```go
package mutators

import (
	"github.com/weiloon1234/Foundry-Go/model"
)

// KeyRecord exercises a distinct primary-key input.
//
//foundry:model table=input_key_records var=KeyRecords key=ID identity=false
type KeyRecord struct {
	ID    model.ID[KeyRecord] `foundry:"input=FreshID,member=ID,key-role=input"`
	Label string              `foundry:"default=database"`
}
type FreshID struct{ Value model.ID[KeyRecord] }

func (KeyRecord) MutateID(input FreshID) (model.ID[KeyRecord], error) { return input.Value, nil }
```

## Execution and safe failures

Each assigned input transforms once per attempt in model declaration order, inside the transaction/savepoint after before hooks and conventions. Final shape validation precedes transforms; codecs validate their results before SQL. Reusing assignments starts from original inputs. Mutator failures roll back the scope without retrying.

Errors retain identity through `errors.Is` and `errors.As`; safe formatting names the field without application error text. Panic and `runtime.Goexit` are contained. Cancellation does not release callback ownership before the callback exits. Reads, direct struct assignments and assignment construction never invoke mutators.

Encrypted columns reject Go mutators. Transform plaintext before assigning the encrypted value; encryption then uses the persisted identity boundary. Password transformations use an appropriate input column and stored password codec. Do not treat an arbitrary string normalizer as password hashing.

## Set-based and conflict writes

Bulk inserts transform each supplied input. Conflict literals supplied through `orm.ConflictSet` transform once per statement, independently of the number of proposed rows. Reusing a conflict policy retains its original input. `ConflictNull` preserves NULL.

`ConflictIncoming` copies the proposed stored value without running the Go transform twice. An own-field proposed expression can do the same. Other SQL calculations into mutated fields are rejected before SQL: PostgreSQL cannot execute an application Go mutator per row. Input carriers are excluded from source `MapInsert` and `MapUpdate`; fixed `Values` inputs still transform once per attempt. Use per-model batches when each source value needs Go behavior.
