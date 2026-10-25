# Oracle prerequisites: integrated dependency decisions

Base: `dbe6b1b789f2a23b8324f53935cd753e9141a9b4`.

- Edited `tests/fixtures/goway/relation_order_golden_test.go`: kept its shared
  oracle comparator and stable site `goway/relation_order_golden/1`, and separated
  `TestComputedRelationOrdersFailClosed` plus its helpers into
  `relation_order_behavior_test.go`. The four direct/target/pivot/mixed subtests
  record actual legacy SQL and bindings and check the ORM transcripts against the
  staged frozen observations. Assertions and test/helper bodies are preserved.
  The paired entry point was already in the integrated census; it is now eligible
  for the strict recording guard. A recorder regression requires its site in both
  offline and PostgreSQL capture plans.
- Added the explicit **migration-before examples** exception, matching only
  `tests/fixtures/goway/migration/{guide,examples,sql}_test.go`. Its regression
  permits those three paths and rejects other migration files, nested paths and
  identical filenames elsewhere. The migration files themselves are unchanged.
- Updated the 8c plan: before blocks become documentation-only and uncompiled;
  after blocks stay compiled. Remove the exception during that transition.
  The census now covers 540 test sources and 103 paired entry points. The actual
  import audit reports 45 legacy importer files, including the three exceptions.

## Acceptance

Completed race checks used r13. After the requested cache rotation, every further
Go command uses `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r14`,
`GOMAXPROCS=4`, `GOFLAGS=-p=4`, `GOWORK=off`.

The strict import guard and its regressions passed under race. The first recorder
run rejected stale census hashes after the test split; after updating the reviewed
hashes, its capture-plan test passed on the focused rerun. The four relation-order modes and the separated rejection test also passed under
race. The r13 full capture was interrupted for the cache rotation, before replay or
publication. No recorder or Go child remained; its abandoned lock was removed and
the complete capture/replay restarted on r14. It passed: **907 observations across
134 assertion sites and 12 footprint packages**, including relation ordering and
the migration comparison. Nothing was published; oracle state remains pending.
Exact commands, logs, remaining importer paths and source hashes are in the
accompanying evidence JSON. Vet for the fixture and recorder packages, catalogue
validation, documentation checks, formatting and diff checks passed.

No production API, generated output, footprint ceiling or frozen record changed.
PostgreSQL capture and the final freeze remain with the lead. No commit.

## Docs notes

The migration-before exception is temporary and confined to the three approved
files. The 8c plan owns its removal; documentation before blocks remain available
without depending on retired generated model declarations. Lead-owned docs unchanged.
