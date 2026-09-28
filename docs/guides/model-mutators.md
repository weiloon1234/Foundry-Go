# Automatic model field mutators

Declare `Mutate<Field>` on a handwritten model and regenerate. Foundry calls the method automatically when persisting an assigned field. Draft setters keep the supplied value until persistence.

```go
//foundry:model table=users
type User struct {
    ID    model.ID[User]
    Email string
}

func (User) MutateEmail(email string) (string, error) {
    return strings.ToLower(strings.TrimSpace(email)), nil
}

// After generation:
user, err := QueryUsers().Create(ctx, db,
    UserDraft{}.SetEmail("  ADA@EXAMPLE.TEST  "))
// On success, user.Email == "ada@example.test".
```

The model imports Foundry's `model` package and Go's `strings` package. The complete [consumer model](../../tests/fixtures/consumer/mutatorqueries/member.go) and [PostgreSQL example](../../tests/fixtures/consumer/mutatorqueries/mutators_postgres_test.go) exercise the generated API without manual normalization calls.

## Typed discovery

`Mutate<Field>` names a persisted Go field, independently of its database column tag. It requires a value receiver and exactly `func(Input) (Stored, error)`, where `Stored` is the field's exact non-null type. `Input` can be the same type or another concrete Go type; it does not need a SQL codec. Interfaces and `value.Nullable` input wrappers are rejected. These methods are pure field transformations: the generated adapter binds a zero-valued receiver, so the method must use its argument rather than read receiver fields. They receive no connection and should not perform I/O. Cross-field behavior and transaction-bound domain work belong in [typed lifecycle hooks](model-hooks.md).

The `Mutate` method prefix is reserved on generated models. An unknown field, pointer receiver, variadic signature, missing error result or incompatible input/output produces a generation error. Signatures are checked from handwritten declarations on a fresh checkout; bodies are checked with the complete generated overlay. Invalid generation preserves existing output. No runtime method-name lookup is used, and gopls resolves the generated registration to the handwritten method.

Generation automatically adds [field behavior notices](model-accessors.md#notices-beside-the-actual-field) beside the stored field and to generated descriptors/draft methods. They identify the custom setter, its persistence timing and any explicit read getter. No extra configuration or handwritten reminder is required.

For `value.Nullable[Stored]`, declare `Mutate<Field>(Input) (Stored, error)`. Foundry preserves explicit NULL without calling the scalar mutator. It also skips omitted fields, including database defaults and untouched patch fields. Explicit zero, false and empty string are present values and do invoke the mutator.

## Different input and stored types

The [independent input model](../../tests/fixtures/consumer/inputqueries/models.go) makes the distinction visible to the compiler:

```go
type EmailInput struct{ Address string }
type StoredEmail string

// Member.Email is StoredEmail.
func (Member) MutateEmail(input EmailInput) (StoredEmail, error) {
    return StoredEmail(strings.ToLower(strings.TrimSpace(input.Address))), nil
}

draft := MemberDraft{}.SetEmail(EmailInput{Address: " ADA@EXAMPLE.TEST "})
member, err := QueryInputMembers().Create(ctx, db, draft)

// Comparisons use the stored column's type; conflict setters accept fresh input.
fields := MemberFields()
filtered := QueryInputMembers().Where(fields.Email.Eq(StoredEmail("ada@example.test")))
conflict := query.OnConflict(fields.ID).DoUpdate(
    fields.Email.Set(EmailInput{Address: " NEXT@EXAMPLE.TEST "}))
```

The surrounding function supplies `ctx` and `db` and handles the returned error. The [PostgreSQL consumer](../../tests/fixtures/consumer/inputqueries/inputs_postgres_test.go) exercises these calls, hooks, bulk writes and reusable conflict policies. Passing `StoredEmail` to either setter, or `EmailInput` to `Eq`, fails compilation. Stored operators, JSON paths, relation keys and alias ownership retain their existing types. Automatic field notices name both the input and stored types alongside any explicit getter.

`draft.Email()` returns `value.Optional[EmailInput]`. Before-write hooks see that fresh input and may replace it through the typed setter. After-write change sets contain `StoredEmail` snapshots, with separate assignment and change flags; they do not capture the original input. Nullable stored fields produce `Optional[Nullable[Input]]` draft getters, scalar `Set<Field>(Input)`, and the existing `Clear`/`Unset` methods.

An omitted UUID primary key with a distinct input mutator cannot be synthesized as that input type. Supply its input explicitly or declare a database-owned default. Key lookup and relation comparisons continue to use the stored key type. Concrete password hashing and verification arrive with authentication; this API supplies a type distinction that a plaintext/hash implementation can use.

## Execution and failures

Each assigned field transforms once per write attempt, in model declaration order. Reusing a draft starts from its original input. Transformation runs inside the write transaction/savepoint, before final codec validation and SQL compilation. For example, a named enum's supplied spelling can normalize into a valid enum before membership validation. Invalid transformed output fails the write.

Mutator errors retain their identity through `errors.Is`. With Foundry's transaction runtime, a panic or `runtime.Goexit` aborts the write scope without exposing the panic payload. The caller's parent transaction can continue after the failed savepoint. There is no automatic retry. Cancellation is checked between transformations and before SQL; an individual pure Go function must return before execution can continue.

Reads, model field assignment and draft setters do not invoke write mutators. A returned model is hydrated from PostgreSQL, including any changes from database defaults or triggers. Pending values are omitted from ordinary mutation diagnostic formatting. Generated drafts implement `fmt.Formatter`, printing only a model-specific draft label for ordinary formatting verbs, including `%+v` and `%#v`. The persisted Go field name `Format` is reserved because it would collide with that method. Explicit draft getters still expose their typed input to the caller; this formatting behavior does not make caller-created logs, errors or serialized payloads safe automatically.

## Bulk and upsert inputs

`CreateMany`, `Upsert` and `UpsertMany` apply the same field transformations to explicitly supplied draft values. Their set-based contract still skips per-model observers. Empty batches validate the declared query/conflict policy without invoking field mutators or opening a transaction. A failed transformation aborts the whole batch before its INSERT statement executes.

An upsert's conflict target sees the transformed proposed row. Literal conflict assignments made with `Set` run the destination field's mutator once per statement, independently of the number of proposed rows. `SetNull` preserves NULL. Reusing a conflict policy starts with its original literal inputs. Final codec validation uses each transformed value.

`Incoming()`, `Update(fields...)`, and `SetConflictValue` with the destination's own proposed field copy that value without another Go transformation. A field with a Go mutator rejects other SQL conflict expressions, including calculations and copies from a different proposed field, before SQL. An arbitrary Go function cannot be applied inside PostgreSQL; rejecting those expressions prevents an alternate assignment path from silently bypassing the field behavior. Fields without mutators retain their full typed SQL conflict expressions. Database constraints remain necessary, and omitted database defaults do not pass through Go mutators.

## Milestone boundary

This milestone 07 increment supplies field normalization with the same or distinct input types. [Typed hooks and provider observers](model-hooks.md) run before mutators and receive captured [change sets](model-changes.md) after SQL. [Explicit read getters and automatic field documentation](model-accessors.md) preserve stored fields and make getter/mutator choices discoverable. [Retrieval callbacks](model-retrieval.md) also preserve stored fields. Soft deletion, relation/bulk-source writes and events/outbox/audit integration are also delivered. A string normalizer alone does not establish password hashing or prevent double hashing. See the [milestone contract](../../blueprint/07-model-lifecycle-events-and-audit.md) for the delivered contracts and verification status.
