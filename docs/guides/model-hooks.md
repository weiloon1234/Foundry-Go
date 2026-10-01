# Typed model hooks

A model declares its hooks factory explicitly. Generation checks callback names, model snapshots, drafts and change sets as ordinary Go types. Normal `Create`, `Update` and `Delete` calls invoke these hooks automatically.

[Retrieval events](model-retrieval.md) use a separate typed factory and registration
family. Reads never instantiate these normal-write hooks.

```go
//foundry:model table=users hooks=userHooks
type User struct {
    ID    model.ID[User]
    Email string
}

func userHooks() UserHooks {
    return UserHooks{
        Creating: func(ctx context.Context, tx *database.Tx, draft *UserDraft) error {
            if !draft.Email().IsSet() {
                *draft = draft.SetEmail("support@example.test")
            }
            return nil
        },
        Updated: func(ctx context.Context, tx *database.Tx, changes UserChanges) error {
            if changes.Fields().Email.Changed() {
                // Perform related domain writes using ctx and tx.
            }
            return nil
        },
    }
}

func (User) MutateEmail(email string) (string, error) {
    return strings.ToLower(strings.TrimSpace(email)), nil
}
```

The example uses `context`, `strings`, and Foundry's `database` and `model` packages. The factory is a package function with no arguments returning the generated `UserHooks`. A misspelled directive target, callback such as `Creatng`, or another model's draft fails generation before publication. The [independent consumer](../../tests/fixtures/consumer/hookqueries/member.go) demonstrates a default introducer lookup and typed assignment in the same transaction, without field-name strings or manual callback dispatch.

## Provider observers

Each generated model also provides `<Model>Observer`, `New<Model>Observer`, and `Register<Model>Observer`. Register an observer inside a provider's registration callback, targeting the same typed pool key used by `database.Module` or `postgres.Module`. The generated helper fixes the model and callback types; another model's identifier or hook factory fails compilation.

For example, with `poolKey` and a typed `defaultEmail` service key declared by the application:

```go
observer := models.NewUserObserver("user.default-email")
return models.RegisterUserObserver(r, poolKey, observer,
    func(s foundation.Resolver) (func() models.UserHooks, error) {
        email, err := foundation.Resolve(s, defaultEmail)
        if err != nil {
            return nil, err
        }
        return func() models.UserHooks {
            return models.UserHooks{
                Creating: func(ctx context.Context, tx *database.Tx, draft *models.UserDraft) error {
                    if !draft.Email().IsSet() {
                        *draft = draft.SetEmail(email)
                    }
                    return nil
                },
            }
        }, nil
    })
```

Dependency construction runs once during application construction. The returned factory runs once per normal write, after any existing row has been locked. Keep write-specific state inside that factory; injected application services must support concurrent use. No observer factory runs when building query metadata, reading models, inserting a batch, or performing an SQL upsert.

Within each callback stage, callbacks from model-local hooks run first, followed by registered observers in provider dependency/registration order. All `Saving` callbacks precede all `Creating` or `Updating` callbacks. They share one typed draft. Mutators and SQL execute once, followed by all `Created`/`Updated` callbacks and then all `Saved` callbacks, sharing the captured stored changes. The first transactional failure stops later stages and rolls back the write. After-commit callbacks are registered individually; failure in one does not skip later callbacks or roll back committed data.

Models without `hooks=Factory` can still have registered observers. Observer sets belong to a specific pool and survive sessions, nested savepoints, and custom transactor wrappers. A wrapper's actual supplied `*database.Tx` determines ownership. Framework owners without applicable hooks retain the ordinary mutation path; an unknown wrapper may need to enter its transaction before completing required-field validation, because a registered hook can supply those fields. A manually constructed or stale model definition without its observer adapter reports an error when registrations exist rather than skipping them. Regenerate models when upgrading the generated API.

An observer that only reacts to deletions can be declared with
`lifecycle.NewDeletionObserver[M, H](name)` instead of `New<Model>Observer` and
registered through the same `Register<Model>Observer`. It runs only for delete,
soft delete and force delete, through the same hooks type. Creates, updates,
restores and set-based updates neither construct it nor take the observed path
because of it, so a model observed only for deletions keeps its unhooked create
and update paths; set-based deletions still require `WithoutModelHooks()`.
Generated [model extension slot](model-extension-slots.md#deletion-cleanup)
cleanup uses it on every connection.

The [independent observer consumer](../../tests/fixtures/consumer/observerqueries/observers_postgres_test.go) exercises automatic writes, multiple providers, stage ordering, transactional effects, cancellation, bulk behavior and concurrent operation-local factories.

## Callback contracts

For a model named `User`, generation provides these optional function fields:

| Callback | Arguments after `context.Context, *database.Tx` |
| --- | --- |
| `Saving` | `value.Optional[User], *UserDraft` |
| `Creating` | `*UserDraft` |
| `Updating` | `User, *UserDraft` |
| `Deleting`, `Restoring`, `ForceDeleting` | `User` |
| `Created`, `Updated`, `Deleted`, `Saved`, `Restored`, `ForceDeleted` | `UserChanges` |

All return `error`. `Saving` receives an absent model on creation and the locked stored model on update. Update and delete snapshots are complete stored values. Query filters remain in force. A missing, ambiguously selected or invalidly decoded model fails before invoking its factory or callbacks.

The factory runs once per normal write, inside the transaction, after locking an existing row. It can return closures sharing state for that operation. Constructing a query, reading models, or inspecting metadata does not invoke the factory. Nil callback fields are skipped. Independent writes receive separate factory instances.

`AfterCommit` has the signature `func(context.Context, lifecycle.Operation, UserChanges) error`; it receives no transaction. The operation is `lifecycle.Create`, `lifecycle.Update`, `lifecycle.Delete`, `lifecycle.SoftDelete`, `lifecycle.Restore` or `lifecycle.ForceDelete`. The same value is available through `changes.Operation()`. Its context is the outer transaction's commit context. Capture any operation-specific attribution needed by this callback in the factory's per-write closures; a nested write's context values are not implicitly merged into the outer context.

## Execution and assignment

- Create: `Saving → Creating → managed timestamps → field mutators → INSERT → Created → Saved`.
- Update: lock and read, then `Saving → Updating → managed timestamps → field mutators → UPDATE → Updated → Saved`.
- Physical delete on ordinary models: lock and read, then `Deleting → DELETE → Deleted`.

[Soft deletion, restoration and force deletion](model-soft-deletes.md#lifecycle-time-and-failures) have separate callback sequences and operation values. They do not also emit saving/updating callbacks.

Generated drafts retain immutable setters: a callback must write the returned draft back through `*draft`. The caller's draft remains unchanged. Hooks may supply omitted required creation values or populate an otherwise empty patch. Foundry-generated UUIDs are already assigned when creation hooks run. Invalid assignment shape and existing-primary-key assignment are rejected before callbacks; final required fields, nonempty updates, codecs and constraints are checked after the hooks.

Only assigned fields run their [mutators](model-mutators.md), once after the before hooks. [Managed timestamps](model-timestamps.md) add effective assignments using the transaction owner's clock; before hooks see the supplied draft, and changes include automatic assignments. Explicit NULL and omission remain distinct. Database defaults are hydrated from the result and do not pass through Go mutators. A hook failure, panic, `runtime.Goexit`, cancellation, invalid final value or failed SQL aborts the write scope.

The generated adapter builds [typed change sets](model-changes.md) automatically from the locked pre-write snapshot, complete SQL result, and effective assignment presence. Framework UUIDs and hook assignments are included. Assigning an email that normalizes to its existing value is assigned but unchanged. Creation/physical deletion use model absence on the appropriate side; soft deletion retains both stored snapshots. Hooks do not need to call `CompareUser` themselves.

## Transactions and ownership

Before and after callbacks run synchronously with result rows closed. Use their supplied context and transaction for related domain operations. Do not retain the draft or transaction pointer, start concurrent work on that transaction, or perform irreversible external effects inside the write transaction.

Passing an existing transaction to a normal write creates a savepoint. Failure rolls back the model and related hook writes; a successful savepoint transfers its after-commit callbacks to the parent. The outer transaction must commit before they run, and rolling back an ancestor discards them. A post-write callback can still veto persistence. An after-commit callback cannot roll back committed data.

The existing [model write outcomes](model-writes.md) apply: an uncertain commit or an after-commit failure from a write that owns the outer transaction retains a typed reconciliation candidate. A write inside a caller-owned transaction returns before that transaction's final outcome; the caller must handle its outer transaction error. No write or callback is automatically retried. In-process callbacks do not provide crash durability.

Nested hook-aware writes propagate a context-bound depth counter and fail at `query.MaxLifecycleDepth`. This diagnoses accidental recursion without preventing useful writes to other models. Errors retain their identity through `errors.Is`; a nesting diagnostic is available through `errors.As` into `*fault.Error`. Always propagate the supplied context, since replacing it discards cancellation and nesting state.

## Bulk behavior and remaining lifecycle work

`CreateMany`, `Upsert`, and `UpsertMany` explicitly skip per-model factories and callbacks, even for one input. Their assigned field inputs and conflict literals retain the documented mutator behavior. A normal `Create` or `Update` never silently switches to bulk semantics. [Relation writes](model-relation-writes.md) use the ordinary pivot lifecycle: attach creates one model, while bounded detach invokes deletion for each selected pivot inside one atomic operation.

This implementation covers model-local and provider-registered create/update/delete/restore/force-delete callbacks, automatic write capture, and composition by callback stage. [Explicit read getters](model-accessors.md) preserve stored fields and have automatic field documentation; they are separate from [retrieval callbacks](model-retrieval.md). [Distinct mutation inputs](model-mutators.md#different-input-and-stored-types) remain typed in before-hook drafts while after-write changes retain stored snapshots. [Per-model batch writes](model-batch-writes.md) reuse these normal lifecycle stages with a bounded atomic candidate set. [Lookup writes](model-lookup-writes.md) provide typed `FirstOrCreate` and `UpdateOrCreate` through the same ordinary write pipeline, with explicit branch and concurrency behavior. Events, outbox and audit remain milestone 07 work. See the [owning blueprint](../../blueprint/07-model-lifecycle-events-and-audit.md).
