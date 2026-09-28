# Team dependency readiness audit — 2026-09-25

Scope: adopting Foundry-Go as a dependency while a team builds its own boilerplate.
**Accepted for team boilerplate development on the supported stack.** The review,
corrections and final verification are complete. The team must select a retrievable
framework version before remote installation. The [adoption guide](team-adoption.md)
supplies the development path; the [evidence record](../evidence/team-readiness-20260925.json)
retains commands, hashes, corrections and limits.

## Review method and coverage

The initial source comparison matched all 3,661 runtime/generator/test/client source hashes and 91 build,
dependency, generation and workflow input hashes retained by the
[September 24 acceptance](../evidence/security-hardening-20260924.json).
The [September 23 module review](module-review-20260923.md) covers 65 module families;
the subsequent [security continuation](security-hardening-20260924.md) covers its
additions. This audit reuses those identified results for unchanged internals and
rechecks the consumer entry points and release path. It is not a claim that every
unchanged implementation line was manually reread on September 25.

| Adoption requirement | Current-source review and acceptance path |
| --- | --- |
| Independent module consumption | Root/fixture module graph, package file policy, generated ownership and private-proxy consumers; no published version assumed |
| Laravel-inspired startup | `application.New`, configuration snapshots, named defaults, typed constructor services and the configured executable |
| Typed domain and transport contracts | Generator discovery/overlay checks, typed endpoint composition, generated models/DTOs, independent compiler/editor/client suites |
| Database and durable effects | Explicit feature migrations, immutable definitions, transaction/outbox/idempotency workflow and retained PostgreSQL acceptance |
| Authentication and safe HTTP | Cookie CSRF registration validation, typed actor/resource scopes, existing security continuation and endpoint failure tests |
| Background work and realtime | Worker/scheduler/WebSocket lifecycle and delivery contracts from the module review; current framework and consumer suites |
| Feature modules | Existing coverage for storage, cache, Redis, email, notifications, attachments, reports, localization, settings and support values; no unimplemented ordinary-runtime marker found |
| Operational ownership | Build/start/run/shutdown paths, configured cleanup and smoke failure handling, readiness/runbook boundaries |
| Team maintenance | One selected runtime/tool version, generation freshness, dependency integrity, security checks and compatibility policy |

Source searches distinguish implementation TODOs from deliberately unfinished
consumer scaffold handlers and historical roadmap entries. Job/command scaffolds
correctly fail until the application supplies domain behavior. Migration progress,
MFA pending states and queued work are runtime states, not missing modules.

## Findings and changes

1. **Missing team onboarding path.** The README led into subsystem and acceptance
   history without a concise dependency-to-boilerplate sequence. Added the
   [team guide](team-adoption.md) and linked it from the README and tooling docs.
2. **Manual CLI/runtime version coordination.** Guidance relied on a separately
   installed matching CLI. The independent consumer now declares the framework
   command as a Go tool; `go tool foundry` shares the runtime's module requirement.
   A real consumer test checks doctor and generation through this entry point.
   Re-audit moved this check beside the existing compiler harness so it reuses
   bounded output, offline execution and process-group cancellation for children.
3. **Stale pending notices.** Generic DTO, union, agent timing and job trace guides
   still described accepted milestones as pending. Replaced these claims with
   links to their delivered acceptance records.
4. **Fresh-package prerequisite omitted.** Replaying installation in an empty
   module showed that `foundry make` correctly requires an existing Go package.
   Added explicit package initialization to the adoption sequence and clarified
   the existing scaffolding contract in the tooling guide.

No additional must-have runtime module or confirmed runtime defect was identified
in this adoption review. No public runtime signature or persisted/wire format is
changed by this batch. Private packaged consumers and the corrected empty-project
installation sequence passed without local replacements.

## Verification results

| Gate | Result |
| --- | --- |
| Initial `make verify` | Passed in 509.42 seconds |
| Final `make verify` after the CLI-test ownership correction | Passed in 504.50 seconds; PostgreSQL, Redis and TypeScript required, existing real gopls selected |
| Fresh consumer races | Tooling, configured profile, bootstrap, complete typed workflow and idempotent HTTP passed; final pinned-tool test also passed with race detection |
| Security | Framework, fixtures, release tool, gopls and npm checks passed; no affected functions reported; module-only advisories retained |
| Independent private packages | All three profiles passed download/integrity, generation, build and editor checks; ordinary/configured executable smoke checks passed |
| Final runtime candidate | Selected intended versions with no replacements; complete consumer generation, module-pinned tool and configured startup passed |
| Empty-project replay | Pinned installation, doctor, initialized packages, model/DTO scaffolds, typed declarations, generation, tidy, vet, tests, races and module integrity passed |
| Linux compilation | Configured packaged consumer built statically for amd64 and arm64 with `CGO_ENABLED=0`; binaries were not run on Linux |

Native checks used macOS arm64 and the SDK declared by the module. Ordinary warm
caches were retained. The package harness used its own isolated module and cold
measurement caches; those caches were removed only after their commands finished.
The unchanged runtime retains the September 24 backend race/fuzz evidence; this
round adds fresh application-consumer races rather than claiming every backend
race/fuzz target was rerun.

All 57 external module versions and license-file hashes match the prior reviewed
inventory. Current scans retain GO-2026-5932 at module level for x/crypto and the
previous gopls module-only advisories; see [dependency review](../dependency-review.md).
This is not a vulnerability-free claim. Known private test credentials were absent
from the inspected archives and retained logs.

The measured and final runtime packages differ only in framework documentation;
the consumer test relocation was separately verified. Final onboarding corrections
and reporting were written after package preparation. These private synthetic
versions are verification artifacts, not published releases. Logs, source hashes,
archives and generated-consumer evidence remain under
`.cache/team-readiness-20260925/` and the two
`.cache/team-readiness-candidate-20260925*` directories.

## Distribution and deployment boundaries

The team needs a retrievable framework revision/version in its chosen repository
or module proxy. A successful local candidate proves packaging and dependency
consumption; it does not publish a remote version. Version-control and publication
remain operator actions. Hosted CI has not run against this local working tree.

PostgreSQL is the current database. Redis supports explicit standalone servers,
not Cluster or Sentinel routing. Deferred milestone 25 includes MySQL/SQLite,
Dart generation, durable realtime recovery and Pusher/Echo compatibility; those
features are not implied by Laravel-inspired DX. The team owns its boilerplate,
business rules, deployment configuration and workload-specific capacity testing.

Live email-provider account sends remain unverified. Previous S3/R2 provider
certification and performance results are historical evidence for their recorded
conditions; this audit does not claim fresh live-provider or throughput results.
