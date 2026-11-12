# Job trace envelopes

Job payload `Version` and transport `EnvelopeVersion` are independent. Every
capture and redispatch writes `envelope_version: 3`
(`jobs.CurrentEnvelopeVersion`), with or without trace, retry policy extensions,
until-processing uniqueness or encryption.

`WireVersion()` reports the explicit format and `Trace()` returns
`value.Optional[tracing.Context]`. Trace propagation remains opt-in through
`Options[P].PropagateTrace` and requires a nonzero `tracing.Context`. Readers
accept only format 3. Omitted versions, formats 1/2, unsupported versions,
explicit null traces and malformed snapshots are invalid input. Unknown payload
versions stay structurally valid and go through ordinary worker registration policy.

Before activating v0.23.0, convert data written by v0.22.x in envelope formats 1/2
to format 3 across queues, workflows, outbox rows and failed-job archives. Stop
old writers during conversion and prevent older snapshots from reintroducing
those formats. Semaklah's one-off conversion is performed by the lead during the
deploy window, before activation; no public migration command is provided.

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
