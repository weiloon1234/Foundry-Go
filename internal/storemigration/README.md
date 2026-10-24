# Framework store migration transcripts

`stores_3b_test.go` runs the declared stores against 59 transcripts captured from
their generated predecessors before migration. Each transcript pins SQL, ordered
bindings, complete hydrated-result fingerprints, savepoints, transaction outcomes
and exact statement counts. These synthetic transcripts do not replace PostgreSQL
integration coverage. Do not regenerate them from the migrated implementation.

| Store | Migrated behavior |
| --- | --- |
| webhookstore | Endpoint, secret and delivery CRUD, expiry filters and ordered UUID seeks. Network dispatch, replay and outcome reconciliation remain in outbound. |
| notificationstore | Capture conflicts, delivery claims, recipient-scoped inbox writes, bounded lifecycle batches and source-subquery pruning. Running deliveries retain their explicit reconciliation rule. |
| attachmentstore | File/variant declarations, UUID maintenance seeks and narrow locked index projections. Bounded write loops retain their per-row lifecycle/savepoints; storage effects remain owned by the existing managers. |
| extensionstore | Metadata, setting and translation declarations; narrow projection codecs; conditional setting conflicts and deletion batches. Owner scopes and persisted identities retain their existing definitions. |
| auditstore | Typed append assignments, payload-free activity projection and bounded DeleteUsing pruning ordered by database insertion sequence. |

Translation deletion uses `rows.DeleteUsing` after the existing owner lock. It
selects keys and deletes in one set-based data statement without reading text.
Audit pruning uses the same source-write compiler with its existing cutoff,
ordering and limit. Neither path introduces per-row lifecycle callbacks.

The store declarations bind the original model fields and retain their tags and
codecs. Timestamp carriers follow the current declarations: paired creation/update
fields are managed; lone timestamps remain caller supplied; database defaults stay
database owned. Public field handles document these distinctions. Generated model
and projection outputs are retired through `make generate`, retaining each package's
ownership manifest. Enum and transport generation remains separate.

The streaming setting-state and translation-index consumers use `rows.Each`, a
package function over the ordinary projection reader. Callback failures stop before
decoding later rows. Regression sources cover row closure, decode failure, nil
callbacks, cancellation, scoped execution and panic/Goexit containment; no Query
or Table methods are added. Lane C owns the shared `rows.Each` implementation;
this lane's self-contained `each.go` is removed at integration.
