# Client contract consumer

Page handlers use `orm.PageResult`, `orm.SimplePage` and `orm.CursorPage`, with the
existing `httppagination.MemberResponse` DTO and explicit `PresentMember` accessor
mapping. Reporting contracts reuse the published `reporting` rows schemas and
datatable sources; they do not redeclare projections or persistence models.

The native cursor descriptor publishes the ORM owner's type ID. Endpoint and DTO
identities, token bytes and datatable reporting contracts are unchanged.
`TestDatatableContractBytes` compares generated artifacts with
`testdata/native-cursor-contracts`. `TestCursorContractPublication` restricts changes
from the immutable `testdata/datatable-baseline` to the cursor ID and resulting
ownership hashes. Its provenance lists old/new hashes for every protected artifact.

Publish only through `make publish-cursor-contracts`; reruns require identical
bytes. Run `make datatable-contracts` to check the publication. The native
TypeScript gate retains strict negative contracts, HTTP/WebSocket behavior,
React/Vue form adapters and bundle assertions.
