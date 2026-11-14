# v0.33.0 public listener bindings: lane T merge handoff

Integration in lane T now uses the shared `FEATURE_DELIVERY_GENERATION` floor for
both public listener bindings and secret templates. Request, archive, retained
target, and recovery gates reject older clients or hosts before setup. Selection
and recovery preserve the floor across rollback. The acceptance scenario includes
both features. The notes below retain the original lane P handoff context.

Lane P adds `public_listener` declarations and runtime delivery. Lane T owns the
shared generation-14 gate in `upgrades.py` and `protocol.json`. Those two files are
unchanged in lane P. This lane alone does not enforce the generation-14 floor.

## Add public listener bindings to the shared generation

Detect `topology.PublicListenerReference` in each role's `common.bindings` after
parsing, or the `public_listener` tag where the gate inspects raw declarations.
Use the same generation-14 constant and admission path as secret config templates.
Do not introduce a separate generation or raise the floor for unchanged declarations.

The gate must cover requested and retained target declarations, including rollback
and recovery. Reject clients or hosts below generation 14 before mutation when
this binding kind is used. Persist the generation-14 selection floor so older
clients cannot manage an instance after it adopts the capability. Preserve that
floor across rollback. Add these cases to lane T's generation tests.

The existing `FEATURE_CAPABILITIES` set is validated as an exact historical set by
`DeploymentSelection.parse`. Do not append a capability to that set without a
compatible reader for existing selections.

## Declaration and descriptor contracts

A consuming role declares:

```json
{"bindings":{"relay_public":{"public_listener":{"role":"relay","listener":"ingress"}}}}
```

`contracts.ResolvedPublicListenerBinding` carries `name`, `role`, `listener`,
`hostname`, `port`, `tls`, `transport`, and `public_addresses`. Role-binding schema
1 contains these records in optional `public_listeners`. The serializer omits an
empty collection at every nesting level, retaining the old wire shape and digests.
The outer runtime descriptor remains schema 2.

`feature_lifecycle.resolve` resolves the selected profile and topology, including
replacement-host profiles. The Go API is `Bindings.PublicListener(BindingName)`.
See the [consumer guide](../../../docs/guides/host-capabilities.md#recipe-bindings).

## Verification at integration

Lane P's `test_public_listener_bindings.py` covers declaration rejection, typed
records, legacy serialization, descriptor delivery, and changed-profile consumer
replacement. `acceptance_feature.py` declares the binding to `relay.control`, the
plain public listener, and asserts the resolved values. The fixture binary reads
it through `deployment.LoadBindings` during readiness. Container acceptance is
left to the integration owner, as requested.
