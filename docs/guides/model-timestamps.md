# Managed model timestamps

Declare creation and update fields with `orm.CreatedAtOf` and `orm.UpdatedAtOf`. Their carriers retain ordinary stored-time predicates, ordering and expression support. Both fields must be non-null, non-primary `time.Time` or `temporal.DateTime` values. The [typed declaration example](model-declarations.md) shows the complete table.

<!-- orm-example: writes-after -->
```go
func write(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := hooks.Members.Update(ctx, tx, id,
		hooks.Members.Name.Set("Ada"), hooks.Members.Note.SetNull())
	return err
}
```

The [member table](../../tests/fixtures/goway/models/hooks/models.go) used here has both roles. Insert supplies creation time when omitted and always supplies update time. Update always assigns update time; it preserves creation time unless explicitly assigned. An otherwise empty update can therefore become a timestamp-only write. Caller assignments remain unchanged.

Both roles must form a valid pair. A conventional lone field remains ordinary unless an explicit role requires the pair. Binding a managed field through `TimeOf` fails declaration validation with the required carrier. For application-managed fields, use ordinary declarations and `orm.NoTimestamps()`. The opt-out conflicts with explicit managed roles. Role constructors do not accept a Go mutator; put business transformations in hooks or ordinary input fields.

## Clock and precision

Database modules inherit `foundation.WithClock`, including `testkit.Clock`. Direct pool construction defaults to `clock.System`; `database.WithClock` supplies an override. Sessions and savepoints preserve their pool's clock, while the actual transaction owner supplies the write clock.

Each executed write attempt samples the clock once after before hooks. Framework time is converted to UTC and truncated to PostgreSQL microseconds. Creation and update conventions share the instant. Clocks must support concurrent use. Time is not promised to be globally monotonic, and retries are never automatic.

Explicit creation time retains normal codec validation, including rejection of unsupported precision. Before hooks can inspect explicit update time, but the convention replaces it. Invalid clock instants fail the write. Context deadlines and connection cleanup still use real cancellation/deadline mechanisms.

## Lifecycle and batches

Conventions run after before hooks and before field preparation and SQL. Captured changes include automatic assignment presence and complete returned stored values, including trigger changes. Physical deletion does not update timestamps. Soft deletion shares a clock sample between deletion and update times; restoration clears deletion time and advances update time.

Empty batches do not sample time. Set insert/upsert batches share one sample while retaining their explicit callback-skipping contract. On conflict update, managed update time copies the proposed `EXCLUDED` value, including `BEFORE INSERT` trigger effects. Creation time changes on conflict only if selected explicitly. An otherwise invalid empty update policy does not become valid because timestamps exist.

Insert-select may map creation time. It cannot map managed update time, which comes from the operation's clock. See [source insertion](model-insert-from-query.md).

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
