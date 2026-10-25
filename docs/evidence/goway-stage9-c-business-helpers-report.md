# Stage 9 lane C business assignment helpers

Added two handwritten methods alongside the retained generated APIs:

- `func (u User) CurrentEmailAssignment() orm.Assignment[User]` in `models/business_assignments.go`: returns `Users.Email.Set(u.Email)`, including an explicitly supplied empty email.
- `func (p Product) CreatorAssignment() orm.Assignment[models.User]` in `catalog/business_assignments.go`: returns `models.Users.ID.Set(p.CreatorID)` with the creator's exact typed User key.

The shared `internal/queryfixture` seed path now uses `CurrentEmailAssignment`. No C-owned caller of either old draft helper remains to migrate; the old helper callers are lane D's root tests. Both `CurrentEmailDraft` and `CreatorDraft` are retained unchanged until those callers move at the final barrier.

`business_assignments_test.go` adds external consumer tests using the existing `inspectInsertAssignments` harness. They assert assignment presence through `WriteIsSet`/`WriteGet`, exact nonempty and empty email values, and two exact User IDs. Changing the originating model after constructing an assignment cannot change the captured value. The harness observes the real Creating callback, stops before SQL and checks transaction rollback; no separate test driver or inspection implementation was added.

Shared addition: `tests/fixtures/consumer/business_assignments_test.go` is a new root-package test file so it can reuse that existing harness. Lane D's existing root/editor test files remain unchanged. The only consumer method additions are the two signatures above; no ORM surface, catalogue case, footprint ceiling, model/projection directive or generated contract changes.

Targeted acceptance uses `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r13 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`. Build, vet, unit tests and races for models/catalog/queryfixture plus the root consumer fixture passed. The root run includes the new assignment tests, retained draft-helper tests and unchanged footprint ceilings. `make generate-check`, catalogue, docs-check, gofmt and `git diff --check` also passed. Commands and final results are recorded in `goway-stage9-c-business-helpers.json`; logs are under `/private/tmp/business-helper-accept/`. Live PostgreSQL and editor integration are not claimed. No commit.

## Docs notes

The models README documents both assignment helpers. Lane D should replace its business draft assertions with these helper calls, preserving presence and exact-value checks, before removing the old draft helpers in the final barrier.
