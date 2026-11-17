# v0.33.0 public listener bindings: lane T merge handoff

Public listener bindings and secret templates are available with generation-19
hosts and clients. New selections carry forward the higher of the stored floor
and the protocol client floor. The acceptance scenario includes both features.

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
