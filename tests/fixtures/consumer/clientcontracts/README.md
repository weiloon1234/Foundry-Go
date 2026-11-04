# Client contract consumer

Page handlers use `orm.PageResult`, `orm.SimplePage` and `orm.CursorPage`, with the
existing `httppagination.MemberResponse` DTO and explicit `PresentMember` accessor
mapping. Reporting contracts reuse the published `reporting` rows schemas and
datatable sources; they do not redeclare projections or persistence models.

The native cursor descriptor publishes the ORM owner's type ID. Endpoint and DTO
identities, token bytes and datatable reporting contracts are unchanged.
`TestDatatableContractBytes` compares generated artifacts with
`testdata/current-contracts`. Historical cohorts `testdata/datatable-baseline`
and `testdata/native-cursor-contracts` remain immutable; `TestCursorContractPublication`
restricts changes between them to the cursor ID and resulting ownership hashes.
Released cohorts under `testdata/contracts-<version>/` are also immutable;
`TestReleasedContractCohorts` verifies that each released cohort's recorded
provenance hashes match its files and ownership manifests.

Publish the current unreleased cohort using `make publish-client-contracts`,
which updates `testdata/current-contracts` and its provenance. Republishing is
permitted while unreleased. Historical cursor-ID publication (`make publish-cursor-contracts`)
remains locked to its original bytes. Run `make datatable-contracts` to check byte
agreement, reporting coverage, and historical publication. The native TypeScript
gate retains strict negative contracts, HTTP/WebSocket behavior, React/Vue form
adapters and bundle assertions.


`members.create` returns a typed 201 or one of two declared 409s: `member_exists`
has generated nested details with a model-owned user ID, while
`registration_closed` is plain. The native client gate executes the compiled
guide example, full and members-portal narrowing, form submissions, and a
client predating the server's `ready` enum case. The Go boundary test captures
server logs and proves an undeclared detailed error stays redacted.
