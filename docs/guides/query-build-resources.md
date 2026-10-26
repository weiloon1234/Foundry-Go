# Query build resources

Use `database/orm` declarations, `orm/expr` computations and `orm/rows` result builders.
The typed facades validate model, field, key, result and nullability ownership before
lowering through private shared `internal/ormcore` and `internal/querycore` owners.
Models carry domain values; table handles carry declaration metadata. Keep engine
state out of model method signatures and stored fields.

The [footprint fixtures](../../tests/fixtures/goway/footprint_test.go) measure compiled
function symbols before linker elimination. Every surviving ceiling stays fixed.
Measure the affected consumer archive with the same toolchain and build settings;
a smaller linked binary alone does not prove a smaller generic compile footprint.

See [typed field computations](typed-field-computations.md), the
[ORM migration guide](orm-migration.md), and the
[historical measurements](../evidence/goway-query-build-resources-historical.md).
The historical source names describe their measured revisions, not current APIs.
