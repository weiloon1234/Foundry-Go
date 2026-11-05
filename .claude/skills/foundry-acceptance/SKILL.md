---
name: foundry-acceptance
description: Complete Foundry-Go milestone acceptance or a requested implementation audit after its source, tests and documentation are ready. Use for batched verification/fixes, final review and evidence; not for every minor edit, routine API lookup or documentation-only maintenance.
---

# Foundry acceptance

Close the requested implementation scope with current-source evidence while
preserving typed consumer contracts and the user's batch cadence. This workflow
is repository-local. It does not authorize publication or expand a small change
into a release or framework-wide audit.

## Establish the acceptance scope

Read the [root instructions](../../../AGENTS.md), applicable scoped instructions
and the task's owning blueprint/guide. Use the [master](../../../blueprint/00-master-architecture-and-parity.md)
for milestone status; existing evidence is historical until its source matches.
For ordinary documentation-only maintenance, stop here and follow the root's
lightweight documentation checks instead.

Identify the concrete guarantees changed and the evidence needed for each: typed
consumer signatures, runtime behavior, persistence/wire compatibility, callback
and resource ownership, generated/client/editor contracts, and performance claims.
Inspect current files and existing verification results. Resume any confirmed live
run instead of launching a duplicate; a polling timeout alone does not end it.

Complete source, regression tests, consumers and docs before compiling. Reuse the
existing helpers and command targets; do not introduce a parallel verification
runner or hard-coded dependency/toolchain inventory.

## Select and run the gate

[Makefile](../../../Makefile) owns the exact commands and package batching.
For milestone acceptance, `make verify` is the full source/fixture/generation gate.
Select additional checks from the changed guarantees, not from every available tool:

| Changed guarantee | Evidence |
| --- | --- |
| SQL, commit outcomes, durable effects, scoped HTTP data | Existing PostgreSQL integration, actual persisted-state assertions, relevant races |
| Shared Redis state, queues, leases or realtime | Required existing Redis integration, isolation/failure tests, relevant races |
| Public/generated owner or value types | Independent consumers, intended compile-fail cases, current deterministic output |
| Completion/signatures/documentation | Existing real gopls probes with unsaved overlays |
| HTTP/DTO/schema/TypeScript contracts | Actual handler/client tests, exported metadata, strict TypeScript compilation/runtime |
| Parsers, protocol or concurrency ownership | Relevant bounded fuzz/fault/race tests, cleanup and subsequent healthy operation |
| Performance or resource claims | Representative measured workload with toolchain/cache/hardware conditions |

Use existing private configuration without printing credentials. When integration
or tools are required, enable their existing required modes rather than accepting
skips: `FOUNDRY_TEST_POSTGRES_REQUIRED`, `FOUNDRY_TEST_REDIS_REQUIRED` and
`FOUNDRY_TEST_TYPESCRIPT_REQUIRED` use `1`. Select existing `FOUNDRY_TEST_GOPLS`,
`FOUNDRY_TEST_NODE` and `FOUNDRY_TEST_TYPESCRIPT` executables/files as appropriate.
The [tooling guide](../../../docs/guides/developer-tooling-and-testing.md) and
[Makefile](../../../Makefile) own setup and explicit backend/tool targets.

Keep expensive runs sequential, preserve ordinary warm caches and check disk
headroom. Do not request a cold package measurement solely because a feature changed.

## Collect, correct and re-audit

Retain failures and collect independent remaining failures where safe. Batch the
corrections before the next compilation round. Diagnose fixture assumptions separately
from runtime defects; fix assertions only when the intended behavioral guarantee
remains tested. Do not weaken bounds, security, ownership or typed contracts.

After the initial gate passes, review the completed implementation against each
requirement. Trace the actual handler/consumer path and relevant failure/cleanup
path, including custom error inspection where extensions participate. Check shared
owners, defaults, generated metadata and compatibility. A requested whole-framework
review needs current module coverage, not just the files changed most recently.

Fix confirmed audit findings together; rerun affected checks and complete the final
full source gate for milestone acceptance. After only final reporting/docs edits,
run documentation checks rather than compiling unchanged runtime code again.

## Conditional package/release work

Only when the task or milestone requires private package acceptance, follow the
[release checklist](../../../docs/release-checklist.md) and
[native measurement guide](../../../docs/guides/developer-resource-measurements.md).
Use a fresh candidate, verify replacements/module integrity/generated output,
review actual scanner findings and notices, and justify provider evidence according
to the changed adapter behavior. Retain explicit limits; do not claim live provider
certification from a local fixture. Read [tools instructions](../../../tools/AGENTS.md)
for process, cache and artifact ownership.

## Completion evidence

Record exact commands, terminal results, required backend/tool conditions, source
fingerprints, failures/corrections and audit dispositions in the existing evidence
convention. Keep credentials and private payloads out. Recheck that the verified
runtime/generator/client source still matches; docs-only changes are distinguishable.

Update owning guides, changelog and master status as applicable. Every required
item needs direct evidence or must remain explicitly incomplete. Report useful
changes, verification and material limits concisely. Leave version control and
publication to the user.
