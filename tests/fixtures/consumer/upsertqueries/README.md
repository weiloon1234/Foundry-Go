# Conflict policies

`ExcludedRecords` publishes the ORM declaration for the physical table `excluded`.
Its migrated cases keep database defaults, incoming-row disambiguation and stored
versus proposed calculations through `rows.ConflictRowsOf`.

The remaining scenarios depend on lane C's `models.User`, `models.WriteRecord` and
`internal/queryfixture` cutover. Their legacy assertions are retained until those
shared declarations are integrated; this package does not duplicate their tables.
