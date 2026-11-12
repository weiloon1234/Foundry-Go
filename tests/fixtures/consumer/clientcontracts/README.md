# Client contract consumer

Page handlers use `orm.PageResult`, `orm.SimplePage` and `orm.CursorPage`, with the
existing `httppagination.MemberResponse` DTO and explicit `PresentMember` accessor
mapping. Reporting contracts reuse the published `reporting` rows schemas and
datatable sources; they do not redeclare projections or persistence models.

The native cursor descriptor publishes the ORM owner's type ID. Endpoint and DTO
identities, token bytes and datatable reporting contracts remain covered by the
current consumer tests. `TestDatatableContractBytes` compares generated artifacts
with `testdata/current-contracts`. `TestCurrentContractProvenance` verifies its
recorded artifact hashes and ownership manifests.

Run `make publish-client-contracts` to refresh the current baseline and provenance
after a reviewed contract change. Run `make datatable-contracts` to check byte
agreement, reporting coverage and current provenance. Git tags retain release
history; no per-release copies or cursor migration publisher are needed.
The native TypeScript
gate retains strict negative contracts, HTTP/WebSocket behavior, React/Vue form
adapters and bundle assertions.


`members.create` returns a typed 201 or one of two declared 409s: `member_exists`
has generated nested details with a model-owned user ID, while
`registration_closed` is plain. The native client gate executes the compiled
guide example, full and members-portal narrowing, form submissions, and a
client predating the server's `ready` enum case. The Go boundary test captures
server logs and proves an undeclared detailed error stays redacted.
