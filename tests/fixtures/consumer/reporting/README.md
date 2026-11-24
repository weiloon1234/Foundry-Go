# Reporting declarations

Use `MemberModels`, `OrderModels` and `OperatorModels` for model reads and writes.
`MemberModelRelations.Orders` and `OrderModelRelations.Member` retain the local
relation owners and soft-delete scopes. `OperatorIdentity` owns authentication
references and queued export actor resolution, including the persisted
`report_operators` table name and model-owned UUID codec.

The client-contract exports are ready for dependent consumers:

| Row DTO (`role=row`) | Generated column bindings | Datatable |
| --- | --- | --- |
| `reporting.MemberRow` | `MemberRowColumns()` | `Members` |
| `reporting.OrderRow` | `OrderRowColumns()` | `Orders` |
| `reporting.MemberTotal` | `MemberTotalColumns()` | `Totals` |
| `reporting.NumericLevelRow` | `NumericLevelRowColumns()` | `NumericLevels` |

`numeric_levels.go` owns the separate `reports.numeric_levels` report. Its
`AgentLevel` and `WideLevel` columns cover numeric enum cases and labels in the
TypeScript client, including integers above 2^53. The members report retains its
frozen SQL and columns.

The `ExportMembers` job DTO and the DTO JSON/validation descriptors retain their
existing package paths and wire fields. Reporting queries, exports and authority
reloads use ORM declarations. Schema setup and deliberately corrupt driver inputs
remain explicit SQL in integration fixtures.

Model structs and relation declarations are handwritten; model directives generate the typed tables. Each table declares its row
selections through its columns and returns only its base source. The source goldens
compile each table's real Query statements with `datatable.Compile`
(`source_golden_test.go`), and the operator identity golden keeps legacy calls as
an independent compatibility oracle; neither is a runtime fallback.

`examples_columns.go` holds the guide's compiled examples for a request-dependent
column, `ScanWith`, `HiddenSelected` on a joined expression and a `DistinctSource`
table (`OrderLineRow`, `MemberNameRow`); `examples_columns_test.go` asserts their SQL.
