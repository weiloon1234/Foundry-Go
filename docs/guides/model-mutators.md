# Typed model field mutators

A mutation input column separates the value accepted by writes from the value stored in the model. An input assignment retains input until persistence; reads and predicates use the stored type. Declare the appropriate `Input*` carrier even when input and stored types are identical.

The [complete declaration](model-declarations.md) uses `InputText[User, EmailInput, string]` with a named `MutateEmail` method. This fixture uses distinct input and stored types:

<!-- orm-example: inputs-after -->
```go
func input(input coremodels.EmailInput) orm.Assignment[mutators.Member] {
	return orm.InputTextSet(mutators.Members.Email, input)
}
```

The [mutator declaration](../../tests/fixtures/goway/models/mutators/models.go) binds input, codec and accessor types. A nil transform argument to `Input*Of` selects the matching `Mutate<Field>` method. Supply a typed `func(Input) (Stored, error)` instead when the transform is explicit. Do not also declare a named mutator. Ordinary columns do not accept transforms; use the input constructor's transform argument.

## Declaration validation

`Mutate<Field>` names a persisted Go field, independently of its SQL column tag. It requires a value receiver, one concrete non-null input, the field's exact scalar stored result, and `error`. A nullable stored field still uses a scalar input/result transform. Unknown fields, pointer receivers, variadic methods, interface inputs and invalid results fail startup validation.

The receiver is a zero-valued type marker. Transform the argument rather than reading receiver fields. Keep mutators pure and free of I/O; cross-field and transactional work belongs in [hooks](model-hooks.md). Ordinary columns with named mutators also fail startup, naming the field and required `Input*Of` carrier. `application.WithTables` checks these contracts before acquiring resources.

## Different input and stored types

Omitted fields skip the transform, including database defaults and untouched update fields. `orm.NullableInputTextSetNull(column)` preserves SQL NULL for a nullable input text column and skips a nullable field's scalar transform. Zero, false and empty string are supplied inputs and do run it.

Before-write hooks read `value.Optional[Input]` through `WriteGet` and `value.Optional[value.Nullable[Input]]` through `WriteNullableGet`. `WriteSet` replaces inputs; `WriteUnset` restores omission. After-write changes contain stored types, never fresh inputs.

An input primary key is declared with `InputKeyOf` over the single table key. Stored-key lookup types remain unchanged. A distinct input disables automatic UUID synthesis: supply input explicitly or use a database-owned default. `PrimaryOf` cannot be combined with `InputKeyOf`.

## Execution and safe failures

Each assigned input transforms once per attempt in model declaration order, inside the transaction/savepoint after before hooks and conventions. Final shape validation precedes transforms; codecs validate their results before SQL. Reusing assignments starts from original inputs. Mutator failures roll back the scope without retrying.

Errors retain identity through `errors.Is` and `errors.As`; safe formatting names the field without application error text. Panic and `runtime.Goexit` are contained. Cancellation does not release callback ownership before the callback exits. Reads, direct struct assignments and assignment construction never invoke mutators.

Encrypted columns reject Go mutators. Transform plaintext before assigning the encrypted value; encryption then uses the persisted identity boundary. Password transformations use an appropriate input column and stored password codec. Do not treat an arbitrary string normalizer as password hashing.

## Set-based and conflict writes

Bulk inserts transform each supplied input. Conflict literals supplied through `orm.ConflictSet` transform once per statement, independently of the number of proposed rows. Reusing a conflict policy retains its original input. `ConflictNull` preserves NULL.

`ConflictIncoming` copies the proposed stored value without running the Go transform twice. An own-field proposed expression can do the same. Other SQL calculations into mutated fields are rejected before SQL: PostgreSQL cannot execute an application Go mutator per row. Input carriers are excluded from source `MapInsert` and `MapUpdate`; fixed `Values` inputs still transform once per attempt. Use per-model batches when each source value needs Go behavior.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
