# Oracle prerequisites after batch 7g

Base: `dbe6b1b789f2a23b8324f53935cd753e9141a9b4`.

The blockers below describe the initial rebase gate. The subsequent lead decision
and its resolution are recorded in [the follow-up report](goway-stage8c-base-dependencies-report.md).
All conflict markers are resolved. Twenty-three previously integrated files are
byte-identical to HEAD, including the public surface, compiler and footprint
registries, framework implementations, consumer migrations and historical evidence.
Only the prerequisite migrations, handwritten legacy declarations, strict import
guard, separated behavior tests and their census/documentation remain in the delta.

The PostgreSQL sensitive and mutator tests use ORM declarations. The actual legacy
mutator hydration comparison lives in its own recording file. The handwritten
redaction test retains both API branches, cause identity, four formatting checks
and rejection before SQL. The guard parses escaped imports and rejects ordinary
tests hidden in recorded files. The census contains 539 test sources and 103 paired
entry points; the integrated census entries remain intact.

## Acceptance on r13

Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r13`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`.

- Passed: all 34 selected fixture tests under race, including redaction and the
  neighboring old/new parity assertions.
- Passed: shared oracle replay tests under race, and the recorder's mixed-file,
  escaped-import, inventory and isolated-refreeze regressions.
- Passed: catalogue validation, conflict-marker check, formatting and diff checks.
- Passed: both handwritten legacy owner-conversion compile-fail cases, pgsmoke
  compilation without PostgreSQL execution, and vet for the fixture, pgsmoke and
  recorder packages, plus shared oracle vet and documentation checks. Exact commands
  and logs are in the accompanying evidence JSON.
- The recorder suite and offline capture/replay command fail at the strict import
  boundary on newly integrated base dependencies, before recording or publishing.

## Integrated base blockers

The following files are unchanged from HEAD:

| File under `tests/fixtures/goway/` | Why the strict boundary rejects it |
| --- | --- |
| `relation_order_golden_test.go` | Contains ordinary `TestComputedRelationOrdersFailClosed` alongside the recorded parity test. |
| `migration/guide_test.go` | Contains ordinary `TestGuideSnippets` alongside the recorded declaration/read test. |
| `migration/examples_test.go` | Generated-model guide snippets; not a recorded entry-point file. One observer helper is used by the recorded test, but the other snippets are compile-only. |
| `migration/sql_test.go` | Generated-model and oldreporting guide snippets; no recorded entry points. |

The first two need behavior/recording separation; the latter two need a decision
about preserving the guide examples independently of generated models or expanding
actual recording coverage. No broad allowlist exception was added. These integrated
files were left unchanged under the explicit base-wins instruction; scope expansion
was requested from the lead. Full replay remains blocked until they are resolved.

No PostgreSQL execution, final freeze, generated-output change, public-surface
addition, footprint-ceiling change or commit. Earlier prerequisite reports describe
the previous base; this report and its JSON are the current-base evidence.

## Docs notes

The stricter recording boundary exposes four additional guide/relation-order files
introduced by integration. Generator retirement must address them before freeze.
Lead-owned documentation remains unchanged.
