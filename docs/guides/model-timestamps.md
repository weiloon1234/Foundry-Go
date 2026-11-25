# Managed model timestamps

Tag creation and update fields with `foundry:"role=created-at"` and `foundry:"role=updated-at"`. Their carriers retain ordinary stored-time predicates, ordering and expression support. Both fields must be non-null, non-primary `time.Time` or `temporal.DateTime` values. The [typed declaration example](model-declarations.md) shows the complete table.

`hooks.Member` is the application's model in its `hooks` package, and `hooks.Members` its generated table. `updateMember` updates one member:

<!-- compiled-source: tests/fixtures/goway/migration/guide_examples.go#writes-after -->
```go
func updateMember(ctx context.Context, tx *database.Tx, id int64) (hooks.Member, error) {
	return orm.Update(ctx, tx, hooks.Members.Table.Query(), hooks.Members.Table.Key(), id, orm.Set(hooks.Members.Name, "Ada"), orm.SetNull(hooks.Members.Note))
}
```

The member model has both roles. Insert supplies creation time when omitted and always supplies update time. Update always assigns update time; it preserves creation time unless explicitly assigned. An otherwise empty update can therefore become a timestamp-only write. Caller assignments remain unchanged.

Both roles must form a valid pair. An active conventional pair requires both managed roles, or `timestamps=none` on the directive with timestamp role tags removed. The opt-out conflicts with explicit managed roles. Managed fields reject mutators and database defaults; put business transformations in hooks or ordinary input fields.

For a lone ordinary `CreatedAt` or `UpdatedAt`, use `role=time` and explicit `Set` calls. If `timestamps=none` is already declared, omit the redundant role tag. `role=time` cannot opt an active conventional pair out of timestamp management. See [declaration diagnostics](model-declarations.md#diagnose-a-wrong-declaration).

The lone ordinary timestamp below can use a database default. It has no managed
update partner and does not disable timestamp conventions for other models.

<!-- compiled-source: tests/fixtures/goway/modelguide/event.go -->
```go
package modelguide

import "time"

//foundry:model table=guide_events var=Events key=ID
type Event struct {
	ID        int64
	CreatedAt time.Time `foundry:"role=time,default=database"`
}
```

## Clock and precision

Database modules inherit `foundation.WithClock`, including `testkit.Clock`. Direct pool construction defaults to `clock.System`; `database.WithClock` supplies an override. Sessions and savepoints preserve their pool's clock, while the actual transaction owner supplies the write clock.

Each executed write attempt samples the clock once after before hooks. Framework time is converted to UTC and truncated to PostgreSQL microseconds. Creation and update conventions share the instant. Clocks must support concurrent use. Time is not promised to be globally monotonic, and retries are never automatic.

Explicit creation time retains normal codec validation, including rejection of unsupported precision. Before hooks can inspect explicit update time, but the convention replaces it. Invalid clock instants fail the write. Context deadlines and connection cleanup still use real cancellation/deadline mechanisms.

## Lifecycle and batches

Conventions run after before hooks and before field preparation and SQL. Captured changes include automatic assignment presence and complete returned stored values, including trigger changes. Physical deletion does not update timestamps. Soft deletion shares a clock sample between deletion and update times; restoration clears deletion time and advances update time.

Empty batches do not sample time. Set insert/upsert batches share one sample while retaining their explicit callback-skipping contract. On conflict update, managed update time copies the proposed `EXCLUDED` value, including `BEFORE INSERT` trigger effects. Creation time changes on conflict only if selected explicitly. An otherwise invalid empty update policy does not become valid because timestamps exist.

Insert-select may map creation time. It cannot map managed update time, which comes from the operation's clock. See [source insertion](model-insert-from-query.md).
