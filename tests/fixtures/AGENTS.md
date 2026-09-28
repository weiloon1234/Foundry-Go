# Independent consumer and plugin fixtures

These instructions add to the [repository rules](../../AGENTS.md). Fixtures prove
what an application can use; they are not starter products or internal unit tests.
See the [consumer guide](consumer/README.md) for existing scenario families.

## Preserve independence

- Use only public Foundry-Go imports. Consumer-owned `internal` helpers are fine;
  importing the framework's private implementation or unsafe access defeats the proof.
- Keep model/key/actor/DTO types concrete in usage examples. Do not add casts,
  reflection or untyped adapters just to hide a framework capability gap.
- Share fixture setup/definitions at their existing owner; keep each scenario's
  business behavior clear. Base/dependent plugins and consumers must use matching
  framework/Go requirements. Use [the root Makefile](../../Makefile) for module order.
- Change handwritten declarations and regenerate owned output/manifests together.
  Public documentation examples should agree with these executable consumers.

## Prove the advertised behavior

- Test the actual API, transport and persisted effect. Use production middleware,
  authentication and codecs; a fake that bypasses these cannot prove integration.
- Add compile-fail cases for promised owner/value/scope restrictions using the
  [existing catalog and harness](consumer/compiler_batch_test.go). Verify the intended
  type failure, not any unrelated compiler error or an invented success stub.
- Extend [real editor probes](../../internal/agent) for changed public completion
  contracts. Client changes use the actual strict TypeScript/runtime transport gate.
- Use existing isolated PostgreSQL schema/application helpers and scoped Redis data.
  Retain test schemas/data as their contract requires; never reset/drop databases,
  flush shared stores or start duplicate services to get a clean test.
- Register cleanup before fallible startup; close clients before their providers.
  After cancellation/failure, verify cleanup and reusable capacity where ownership
  changed, not only the returned error.
- Required integration/tool checks must fail when unavailable, not silently skip.
  Reuse `testkit.TrackExternalInputs` for external compiler/editor/client inputs so
  cached parent tests cannot hide changed child inputs.

Run compiler/editor/generation checks in the root batch cadence. Private packaged
consumers are separate evidence from local `replace` fixtures; use the existing
release tools when the requested acceptance includes package consumption.
