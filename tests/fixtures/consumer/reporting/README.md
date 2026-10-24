# Reporting declarations

Use `MemberModels`, `OrderModels` and `OperatorModels` for model reads and writes.
`MemberModelRelations.Orders` and `OrderModelRelations.Member` retain the local
relation owners and soft-delete scopes. `OperatorIdentity` owns authentication
references and queued export actor resolution, including the persisted
`report_operators` table name and model-owned UUID codec.

The client-contract exports are ready for dependent consumers:

| DTO (unchanged identity) | Handwritten rows schema | Datatable |
| --- | --- | --- |
| `reporting.MemberRow` | `MemberRows` | `Members` |
| `reporting.OrderRow` | `OrderRows` | `Orders` |
| `reporting.MemberTotal` | `MemberTotals` | `Totals` |

The `ExportMembers` job DTO and the DTO JSON/validation descriptors retain their
existing package paths and wire fields. Reporting queries, exports and authority
reloads use ORM declarations. Schema setup and deliberately corrupt driver inputs
remain explicit SQL in integration fixtures.

Generated model helpers and their relation declarations coexist only until the
lead's generation barrier. The source goldens and operator identity golden keep
legacy calls as independent compatibility oracles; they are not runtime fallbacks.
