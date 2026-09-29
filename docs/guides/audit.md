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

The second migration, `000002_add_history_keys`, rewrites the table once. It gives every existing row an insertion `sequence` in its previous `(created_at, id)` order, records that existing rows used redaction policy 1, copies each row's attribution request ID into `correlation`, and adds database-generated subject/actor lookup keys with their history indexes. Rows written by an older binary during a rollout still receive a sequence and lookup keys from the database. The lookup keys hash UTF-8 text, so the migration requires a `UTF8` (or `SQL_ASCII`) database encoding and fails otherwise. Run it during a maintenance window for large audit tables.

## Transaction and lifecycle behavior

Normal create, update, physical delete, soft delete, restoration and force deletion record their existing captured changes. No additional subject lookup, getter evaluation, model JSON serialization or second mutation comparison occurs. Field mutators have already run, so audit values describe stored data.

Creation and deletion records contain every field that the policy includes. Updates, soft deletions and restorations contain only fields that the write assigned or changed, plus the subject key; untouched columns never invoke their codec and do not enlarge the row.

A large value never fails or rolls back the business write. A value larger than `Config.MaxValueBytes` (zero selects `audit.DefaultMaxValueBytes`, 64 KiB; explicit values are bounded by `audit.ValueBytesFloor` and `audit.ValueBytesCeiling`) is stored as a `record.Oversized` marker. `Snapshot().Digest()` returns its byte size and the SHA-256 of the stored bytes: UTF-8 text, raw binary bytes or the already-redacted canonical JSON. JSON too large to inspect for sensitive keys keeps only its size. When the complete record would still exceed its representation bound, the largest remaining values are digested until it fits. `FieldValue.Get` on a digested value returns a `fault.Missing` error; it never fabricates the value.

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

History is newest first by the database-assigned insertion `Sequence`. Rows written in one transaction share its `created_at` timestamp; the sequence keeps a creation and a later update in that same transaction in their actual order.

For long histories, `audit.ModelTimeline` reads one indexed keyset page without a count query:

```go
first, err := audit.ModelTimeline(ctx, db, recorder, account.FoundryReference(),
    audit.TimelineRequest{Size: 20})
next, more := first.Next.Get()
if more {
    older, err := audit.ModelTimeline(ctx, db, recorder, account.FoundryReference(),
        audit.TimelineRequest{Size: 20, Before: value.Set(next)})
}
```

`Size` is 1–`query.MaxPageSize`. `Next` is a position hint, not a snapshot across concurrent writes.

Each result exposes `ID`, `Changes`, `Sequence`, `Area`, `Origin`, `Correlation`, `Route` and `CreatedAt`. `Changes` is a model-owned audit representation. It is never an incompletely hydrated application model. The historical subject may already have been physically deleted.

```go
result, err := audit.FindModel(ctx, db, recorder,
    (Account{}).FoundryReference(), auditID,
)
```

`auditID` has type `audit.ModelID[Account]`; another model's ID fails compilation. `ParseModelID[Account]` supplies the explicit string/transport boundary. A foreign model or area is absent. A malformed persisted snapshot fails validation. Neither lookup authenticates the caller nor fetches the original model.

Generated `AccountAuditFields(history.Changes())` exposes concrete historical field types. For an email field, the resulting `fields.Email` is `value.Optional[record.FieldChange[string]]`. `Before` and `After` return typed `FieldValue[string]` values; `Get` returns an optional stored string plus an error. The [compiled consumer example](../../tests/fixtures/consumer/auditqueries/history.go) demonstrates this path.

The layers preserve different meanings:

- An absent field means policy exclusion, a field absent from that historical schema, or a field that an update, soft deletion or restoration neither assigned nor changed.
- `record.Absent` means no model snapshot existed, as before creation or after physical deletion.
- A disclosed SQL NULL remains a concrete nullable value; it is not a missing model or scalar zero.
- `Assigned` and `Changed` remain separate. An email normalized to its existing value is assigned but unchanged.
- Exact decimals, interval components and temporal instants use their existing database codecs.

## Redaction and presentation

The zero policy applies automatic sensitive-name redaction. Names are split at snake, kebab, dotted and camel-case boundaries and at letter/digit boundaries; digit suffixes and simple plurals are ignored (`apiKeys2` matches `api key`). Secret, token, password, passphrase, credential, authorization, API/private key, OTP, PIN, CVV/CVC, SSN, card number, cookie, session and recovery-code conventions are recognized, including joined names such as `accesstoken`. Short abbreviations such as `pin` and `otp` match only whole words, so `spinner` is not redacted. Nested JSON objects and arrays use the same rule. `record.Redact` hides a particular field's values; `record.Exclude` removes the field entirely. Neither policy invokes that field's codec to capture the hidden value.

Every row stores the redaction policy that wrote it (`record.Redaction`; new rows use `record.CurrentRedaction`). Reads validate each row under its own policy, so recognizing more names never makes older history unreadable, and a row can never claim a newer policy than the one that captured it. `Entry().Redaction()` and `Document().Redaction()` report the policy.

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

`Approved.Find(ctx, db, recorder, id)` returns optional `ActionRecord[Approval]`, restricted to the descriptor's name/version and selected area. `id` is `audit.ActionID[Approval]`; `ParseActionID[Approval]` is its transport parser. `Approved.Timeline(ctx, db, recorder, request)` lists that action newest first, and `audit.ActionTimelineFor(ctx, db, recorder, Approved, account.FoundryReference(), request)` lists it for one subject. Both use the keyset `TimelineRequest`. `Document().Decode()` returns a fresh DTO only when complete and unredacted. `Document().Payload()` exposes the sanitized audit representation when a sensitive JSON key was redacted. Explicit domain DTOs should omit fields that must not appear even as markers. Custom JSON callbacks run under the framework's existing fault isolation and must remain pure and deterministic.

## Origin, areas and retention

`attribution.WithContext` supplies immutable human/system and request provenance. Audit does not store authentication credentials or depend on an Actor wrapper. `audit.WithArea(ctx, area)` selects an area for subsequent history reads and writes; the recorder's configured area is the default. Areas are metadata/scoping, not an authorization mechanism. Applications must authorize access to audit history.

Inside a routed HTTP request, the router attaches `attribution.Route` (method and declared route name) to the request context, and audit rows store it; `Route()` returns it. Raw URLs, query strings and path values are never stored because they can carry credentials or personal data. The route is request-local and does not travel with queued work.

Each row also stores a `Correlation`. `audit.WithCorrelation(ctx, id)` groups the rows of one logical operation, such as a bulk change or import batch; `audit.NewCorrelation()` generates a UUIDv7 identifier. Without it, rows use the attribution request ID, so everything one request wrote can be read together.

Heterogeneous timelines return payload-free `audit.Activity` summaries, newest first, through indexed keyset reads:

- `audit.SubjectActivity(ctx, db, recorder, subject, request)` — model changes and domain actions for one subject.
- `audit.ActorActivity(ctx, db, recorder, actorReference, request)` — rows attributed to one model actor.
- `audit.SystemActivity(ctx, db, recorder, systemID, request)` — rows attributed to one system identity.
- `audit.CorrelationActivity(ctx, db, recorder, correlation, request)` — rows written under one correlation.

`Activity` exposes the operation or action name/version, subject identity and the shared metadata. `audit.ModelActivityID(activity, template)` and `audit.ActionActivityID(activity, action)` convert a row into its typed ID when it belongs to that model or action; load details with `FindModel` or `Find`. None of these reads authorizes the caller.

`Recorder.PruneBefore(ctx, tx, cutoff, limit)` removes at most the requested number of old rows from the selected area. `PruneRetention(ctx, tx, now, limit)` derives its cutoff from configured retention days, each 24 hours. Both use the shared typed query compiler, preserve transaction rollback, avoid loading payloads and perform one bounded batch. The maximum batch size is `query.MaxPageSize`; concurrent pruning may remove fewer rows than requested.

`Scope.PruneRetention(ctx, now, batch)` and `Scope.PruneBefore(ctx, cutoff, batch)` loop over bounded batches, each in its own transaction in the configured schema, until a batch removes fewer rows than requested. They return the rows removed by committed batches, including when a later batch fails or the context ends; rerunning is safe. The cutoff is derived once, before the first batch.

`application.AuditCommand()` declares the operator command `audit prune [--before RFC3339] [--area name] [--batch 500] [--format text|json] [--apply]`. Register it in the application's CLI registry. Without `--before` it uses the configured retention and fails when retention is disabled; a `--before` cutoff later than the current time is rejected. By default it only counts the matching entries (`matching=N`, a dry run like the extension `rescope` commands); `--apply` removes them in bounded transactions and prints `removed=N`. It prints only the area and counts, never payloads. `Scope.CountBefore` and `Scope.RetentionCutoff` expose the same count and cutoff to application code.

No pruning runs automatically unless a configured application enables the [housekeeping schedule](production-operations.md#housekeeping-schedule), which applies `Scope.PruneRetention` to the configured area when retention is set. Zero `RetentionDays` disables the configured helpers; `DefaultConfig()` uses that setting. Otherwise a maintenance command or scheduler must explicitly invoke retention. These APIs remove audit history deliberately; they do not reset tables or databases.
