# Typed model history and domain auditing

Audit is opt-in. Foundry records stored model changes through the actual write transaction, using generated observers and the same field codecs as persistence. Application code selects model policies and maps history into its own response DTOs.

## Register models and storage

The independent [audit consumer](../../tests/fixtures/consumer/auditqueries/domain.go) registers its recorder alongside its database provider:

```go
return audit.Register(r, Pool, History,
    audit.Config{Area: "accounts", RetentionDays: 90},
    AccountAuditing(AccountAuditPolicy{Note: record.Exclude}),
    LabelAuditing(LabelAuditPolicy{}),
)
```

`Pool` is a `foundation.Key[*database.DB]`; `History` is a `foundation.Key[*audit.Recorder]`. The generated policy names actual persisted fields and retains its model owner. A policy for another model fails compilation. Duplicate auditing declarations for one model fail application construction through the ordinary observer registry. Recorder construction performs no I/O, owns no second database pool, and shares no global registration state between applications.

Register `audit.Migrations()` with the application's ordinary migration definitions and run the migration command explicitly. It creates `foundry_audit` in the selected database schema. Migrations and model writes must use the same schema. Missing audit storage fails an enabled model write; Foundry never treats missing storage as permission to discard history.

## Transaction and lifecycle behavior

Normal create, update, physical delete, soft delete, restoration and force deletion record their existing captured changes. No additional subject lookup, getter evaluation, model JSON serialization or second mutation comparison occurs. Field mutators have already run, so audit values describe stored data.

Auditing runs in the operation's transaction at `Created`, `Updated`, `Deleted` or `Restored`. `ForceDelete` records once through `Deleted`, using its distinct operation value. A later `Saved`/observer failure rolls back the audit row and model write together. Outer transaction and savepoint rollback retain the existing database semantics. A returned audit ID is not confirmation of outer commit.

Explicit set-based writes skip per-model observers and audit. Use the existing `CreateEach`, `UpdateEach` and `DeleteEach` alternatives when every affected model requires lifecycle history. An explicit domain audit action can record the intent/result of a bulk operation without claiming to contain every model's before/after values.

## Read typed history

```go
page, err := audit.ModelHistory(ctx, db, recorder,
    account.FoundryReference(),
    query.PageRequest{Number: 1, Size: 20},
)
```

The result is `query.Page[audit.ModelRecord[Account, model.ID[Account]]]`. Natural-key models preserve their concrete key type instead. `Items` is a native slice; numbered pagination retains the query package's row bounds and count/rows consistency rules. Use an appropriate transaction isolation level when both queries must observe one snapshot.

Each result exposes `ID`, `Changes`, `Area`, `Origin` and `CreatedAt`. `Changes` is a model-owned audit representation. It is never an incompletely hydrated application model. The historical subject may already have been physically deleted.

```go
result, err := audit.FindModel(ctx, db, recorder,
    (Account{}).FoundryReference(), auditID,
)
```

`auditID` has type `audit.ModelID[Account]`; another model's ID fails compilation. `ParseModelID[Account]` supplies the explicit string/transport boundary. A foreign model or area is absent. A malformed persisted snapshot fails validation. Neither lookup authenticates the caller nor fetches the original model.

Generated `AccountAuditFields(history.Changes())` exposes concrete historical field types. For an email field, the resulting `fields.Email` is `value.Optional[record.FieldChange[string]]`. `Before` and `After` return typed `FieldValue[string]` values; `Get` returns an optional stored string plus an error. The [compiled consumer example](../../tests/fixtures/consumer/auditqueries/history.go) demonstrates this path.

The layers preserve different meanings:

- An absent field means policy exclusion or a field absent from that historical schema.
- `record.Absent` means no model snapshot existed, as before creation or after physical deletion.
- A disclosed SQL NULL remains a concrete nullable value; it is not a missing model or scalar zero.
- `Assigned` and `Changed` remain separate. An email normalized to its existing value is assigned but unchanged.
- Exact decimals, interval components and temporal instants use their existing database codecs.

## Redaction and presentation

The zero policy applies automatic sensitive-name redaction. Password, secret, token, credential, authorization and API/private-key conventions are recognized across common Go/SQL/JSON naming styles. Nested JSON objects and arrays use the same rule. `record.Redact` hides a particular field's values; `record.Exclude` removes the field entirely. Neither policy invokes that field's codec to capture the hidden value.

Convention matching cannot identify a secret with an unrelated name. Declare its typed field policy explicitly. An audit subject still stores its primary identity: policies that attempt to exclude/redact that same primary field fail rather than imply the identity was hidden.

Redacted fields keep assignment/change metadata and explicit redaction states. `FieldValue.Get` returns a `fault.Missing` error for redacted data. Partially redacted JSON is available as an audit snapshot, but cannot be decoded into a fabricated partial instance of its original Go type. Persisted redaction flags are checked against the representation on reload.

`Changes().Entry().Payload()` and snapshot JSON are deliberate audit export boundaries. Routine formatting hides stored contents. They contain stored audit data, not automatically computed getter values. Generated history fields carry the same getter/setter notices as the original model, so source readers and gopls can identify the relevant behavior before mapping a DTO.

## Explicit domain events

Declare one reusable descriptor for a concrete audit DTO:

```go
type Approval struct {
    Reason string `json:"reason"`
}

var Approved = audit.Define[Approval]("account.approved", 1)

id, err := audit.RecordFor(ctx, tx, recorder, Approved,
    account.FoundryReference(), Approval{Reason: "review completed"})
```

Use `Approved.Record(ctx, tx, recorder, payload)` for an event without a model subject. Both paths require the actual `*database.Tx` and preserve its outcome. They do not deliver notifications or publish events. Payloads and subject references retain their concrete types; the serialized identity is confined to the persisted audit metadata boundary.

`Approved.Find(ctx, db, recorder, id)` returns optional `ActionRecord[Approval]`, restricted to the descriptor's name/version and selected area. `id` is `audit.ActionID[Approval]`; `ParseActionID[Approval]` is its transport parser. `Document().Decode()` returns a fresh DTO only when complete and unredacted. `Document().Payload()` exposes the sanitized audit representation when a sensitive JSON key was redacted. Explicit domain DTOs should omit fields that must not appear even as markers. Custom JSON callbacks run under the framework's existing fault isolation and must remain pure and deterministic.

## Origin, areas and retention

`attribution.WithContext` supplies immutable human/system and request provenance. Audit does not store authentication credentials or depend on an Actor wrapper. `audit.WithArea(ctx, area)` selects an area for subsequent history reads and writes; the recorder's configured area is the default. Areas are metadata/scoping, not an authorization mechanism. Applications must authorize access to audit history.

`Recorder.PruneBefore(ctx, tx, cutoff, limit)` removes at most the requested number of old rows from the selected area. `PruneRetention(ctx, tx, now, limit)` derives its cutoff from configured retention days, each 24 hours. Both use the shared typed query compiler, preserve transaction rollback, avoid loading payloads and perform one bounded batch. The maximum batch size is `query.MaxPageSize`; concurrent pruning may remove fewer rows than requested.

No pruning runs automatically. Zero `RetentionDays` disables the configured helper; `DefaultConfig()` uses that setting. A maintenance command or later scheduler integration must explicitly invoke retention. These APIs remove audit history deliberately; they do not reset tables or databases.
