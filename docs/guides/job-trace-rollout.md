# Job trace envelope rollout

Trace propagation is opt-in and requires compatible readers before producers
enable it. The [master roadmap](../../blueprint/00-master-architecture-and-parity.md)
records acceptance and verification evidence.

Job payload `Version` and `EnvelopeVersion` are different types and concerns.
The existing envelope has no `envelope_version` or `trace` fields;
`WireVersion()` identifies it as `LegacyEnvelope`. Default capture/dispatch retains
that exact shape, even when the caller has a trace. Older strict decoders can
continue consuming it.

`Options[P].PropagateTrace` opts a producer into capturing a nonzero current
`tracing.Context`. Such an envelope carries `envelope_version: 2` and a required
immutable trace snapshot. `WireVersion()` returns `TracedEnvelope`; `Trace()`
returns `value.Optional[tracing.Context]`. With no current trace, capture retains
the legacy envelope. Origin/attribution fields, payload versions, execution IDs,
uniqueness policy and queue names keep their existing meanings.

The new decoder accepts both legacy and traced envelopes. It rejects unsupported
transport versions, trace fields without format 2, format 2 without a trace,
explicit null traces and malformed trace snapshots. Unknown *payload* versions
remain structurally valid and are classified through ordinary worker registration
policy. Unknown *envelope* formats cannot safely reach the worker handler.

Use this rollout sequence:

1. Deploy compatible readers everywhere while producers keep propagation off.
   Include workers, durable outbox publishers, workflow readers and adapter code
   that calls `DecodeEnvelope` or persists encoded envelopes.
2. Confirm that no legacy reader can reserve/restore messages from the destination
   queues or outbox rows. Old strict readers reject the new fields.
3. Enable `PropagateTrace` on selected producers. New readers can consume mixed
   legacy/traced queues and workflows. Typed payload registration is unchanged.
4. To stop propagation, turn off the producer option. Continue running compatible
   readers until all retained traced messages, retries, workflows and outbox rows
   are gone according to their retention policies. Only then consider a reader
   rollback. Disabling the producer does not rewrite existing durable data.

Capture a `Pending` once and reuse it after ambiguous acceptance. Re-capturing the
same ID under a different active trace produces a different envelope and can
conflict with the already accepted identity, just like changed payload/attribution.
Outbox retries preserve the originally captured snapshot through the actual
transaction and publisher route. No automatic migration or queue rewrite occurs.

With an application recorder, a worker restores the captured parent and creates
its own child span. Legacy messages get fresh trace roots while retaining their
ordinary request attribution. Vendor tracestate is persisted only by the explicit
propagation option; it stays out of ordinary formatting, observations and metrics.

Source coverage includes the frozen legacy strict reader, mixed workflow decoding,
worker parent restoration and PostgreSQL outbox rollback/ambiguous publication.
These checks passed [milestone 24 acceptance](../production-acceptance.md).
