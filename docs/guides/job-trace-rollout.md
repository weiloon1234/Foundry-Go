# Job trace envelopes

Job payload `Version` and transport `EnvelopeVersion` are independent. A capture
writes the smallest envelope format that carries what it needs:

- Format 1 (`jobs.EnvelopeV1`) has no `envelope_version` or `trace` field. It is the
  default.
- Format 2 (`jobs.TracedEnvelope`) adds a required immutable trace snapshot. A
  capture uses it when `Options[P].PropagateTrace` is set and the context carries a
  nonzero `tracing.Context`.
- Format 3 (`jobs.ExtendedEnvelope`) carries `Policy.MaxExceptions`,
  `Policy.RetryUntil`, until-processing uniqueness or an encrypted payload, with an
  optional trace.

`WireVersion()` reports the format and `Trace()` returns
`value.Optional[tracing.Context]`. Readers accept all three formats, including
format 3 without extended fields. They reject unsupported versions, trace fields in
format 1, format 2 without a trace, explicit null traces and malformed snapshots.
Unknown payload versions stay structurally valid and go through ordinary worker
registration policy.

Capture a `Pending` once and reuse it after ambiguous acceptance. Re-capturing the
same ID under a different active trace produces a different envelope and can
conflict with an accepted identity, just like changed payload or attribution.
Outbox retries preserve the original snapshot through the transaction and publisher
route. Turning propagation off does not rewrite retained queues, workflows, outbox
rows or failed-job archives.

With an application recorder, a worker restores the captured parent and creates
its own child span. Messages without a trace get fresh trace roots and retain their
ordinary request attribution. Vendor tracestate is persisted only by the explicit
propagation option; it stays out of ordinary formatting, observations and metrics.
