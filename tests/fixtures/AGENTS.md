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
- For retained DTO/contract generation, change handwritten declarations and regenerate
  owned output/manifests together. ORM declarations follow [database rules](../../database/AGENTS.md).
  Public documentation examples should agree with these executable consumers.

## Prove the advertised behavior

- Test the actual API, transport and persisted effect. Use production middleware,
  authentication and codecs; a fake that bypasses these cannot prove integration.
- Add compile-fail cases for promised owner/value/scope restrictions using the
  [existing catalog and harness](consumer/compiler_batch_test.go). Verify the intended
  type failure, not any unrelated compiler error or an invented success stub.
- Keep the [Go-way catalogue mapping](../../blueprint/goway-data-layer/compilefail-mapping.tsv)
  tied to executable new-API negatives and validate it with `python3 tools/goway_catalogue.py`.
  Edit only owned rows/cases; explain merges or obsolescence with replacement evidence.
  Never replace a compile-time rejection with a runtime check or a missing generated symbol.
- Preserve [frozen legacy evidence](goway/testdata/oracles/README.md): stable assertion
  IDs, transcripts, source provenance and publication receipts. Ordinary tests must
  not rewrite expectations or substitute ORM output for a legacy observation.
  Freeze/refreeze publication belongs to the lead's explicit recorder run with
  required PostgreSQL coverage; an offline check is not publication.
- Review changed test sources before updating owned `comparisons.json` census entries;
  preserve paired assertions. Keep ordinary behavior tests separate from legacy
  recording files. Retain frozen legacy packages until their removal is authorized;
  generator retirement alone does not authorize deleting evidence or skipping tests.
- Keep existing [footprint ceilings](goway/footprint_test.go) unchanged. New probes
  use `ceil(measured * 1.15)` and retain source/toolchain evidence. Historical oracle
  counts do not replace current measurements.
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
