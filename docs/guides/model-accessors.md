# Explicit model getters and automatic field documentation

Declare `Access<Field>() (Result, error)` on a handwritten model and run ordinary Foundry generation. The getter is a normal Go method on the actual model value. Its return type can differ from the stored field type, and Go checks every use of that result. There is no extra accessor registration, casts map or reminder-comment configuration.

For example, the [consumer model](../../tests/fixtures/consumer/mutatorqueries/member.go) retains a model-owned `ID`. Its [handwritten getter](../../tests/fixtures/consumer/mutatorqueries/accessors.go) offers a string for a transport boundary:

```go
func (m Member) AccessID() (string, error) {
    return m.ID.String(), nil
}
```

`member.ID` remains a `model.ID[Member]` for queries and relationships. `member.AccessID()` explicitly returns its string representation. Likewise, this consumer's `AccessEmail()` returns a named `DisplayEmail` value, while `member.Email` retains the stored string. Getters do not run during hydration, ordinary field access, query filtering, JSON encoding or change-set capture.

## Choose the value in the DTO

The [compiling consumer response](../../tests/fixtures/consumer/mutatorqueries/accessors_test.go) makes the choice explicit:

```go
storedEmail := member.Email
displayEmail, err := member.AccessEmail()
```

Use the stored value when that is the contract, or check the getter's error and assign its typed result to the intended DTO field. A getter is not necessarily appropriate for every DTO. Persistence models do not automatically become public responses, and JSON encoding does not call these getters for the caller.

Keep getters pure and treat receiver data as read-only. A value receiver copies the struct, not arbitrary reference-containing data inside custom values. Getters receive the actual stored model, unlike automatic write mutators whose receiver is a zero-valued type marker. They must not perform hidden database I/O. Errors have ordinary Go semantics; the framework does not add a transaction or recovery wrapper around a direct method call.

Nullable getters handle NULL explicitly and declare the intended result type. The consumer's `AccessNickname()` returns `value.Nullable[DisplayNickname]`, preserving a missing nickname separately from an empty one. Evaluating it leaves the stored `value.Nullable[string]` field intact. Getter results never replace IDs, relation/cursor keys or lifecycle snapshots.

## Notices beside the actual field

Generation derives field help from the same checked getter and [mutator](model-mutators.md) methods. Generated query descriptors and draft methods always receive this help. Handwritten files are edited only when you opt in with `foundry generate --field-docs`: generation then maintains comments beginning with `// Foundry field behavior (generated):` beside affected handwritten fields. These comments identify the stored model/column, custom method and when it runs. Without the flag, existing notices are left untouched and are not checked.

Opted-in documentation is visible when reading model source and when inspecting an ordinary `member.Email` selector through gopls. Both getter and setter notices appear when a field has both methods. The setter notice points to the automatic persistence behavior and generated draft setter: ordinary struct assignment and draft construction do not normalize a value immediately. Query predicates still operate on stored SQL columns, without invoking a Go getter.

When a setter accepts a [different input type](model-mutators.md#different-input-and-stored-types), the notice also names its input and stored result. The [input consumer's `Member.Email`](../../tests/fixtures/consumer/inputqueries/models.go) documents `EmailInput`, `StoredEmail` and `AccessEmail` together, so source readers and gopls can discover the write boundary and explicit read choice at the actual field.

Existing leading field comments are retained. When a field has only a trailing comment, generation also derives its displayed text into the leading notice; this prevents gopls's preference for leading documentation from hiding the original help. The trailing comment remains the editable source of that text.

Do not maintain the generated notices manually. Adding, renaming or removing a method updates its notice during normal generation. `generate --check` rejects stale or missing notices without modifying files. Handwritten fields, methods and comments remain consumer-owned; the whole model file is not added to the generated-file manifest. Notice changes use the existing source-change checks, publication journal, rollback and recovery pipeline. The generator verifies that only managed comments and standard Go formatting changed, preserving executable tokens and user documentation.

Foundry's [agent command](agent-language-tooling.md) retains the actual gopls documentation in JSON and readable completion output. No custom language-server plugin or editor configuration is required for these field comments. Documentation makes the choice discoverable; it does not force an editor or LLM to request or follow it.

## Discovery and verification

On declared models, the `Access` prefix is reserved for persisted field getters, just as `Mutate` is reserved for write mutators. Use a value receiver, no arguments, one concrete result and the ordinary `error` result. Unknown fields, pointer receivers, extra arguments, missing errors and interface results fail generation before publication. Named result types and nullable wrappers remain typed. Other computed domain methods can use ordinary names such as `FullName()` without claiming an accessor for a persisted field.

Signatures are discovered on fresh checkouts even when method bodies reference generated declarations. Complete-overlay checking still validates those bodies. The consumer tests exercise explicit DTO mapping, getter errors, NULL/empty values, unchanged stored models and PostgreSQL query behavior. Generator tests cover repeated/current output, obsolete notices, user comments, source preservation and publication recovery. [Retrieval callbacks](model-retrieval.md) preserve the same stored fields. Milestone 07 lifecycle, event, outbox and audit capabilities are delivered; use the [master status](../../blueprint/00-master-architecture-and-parity.md) for verification evidence.
