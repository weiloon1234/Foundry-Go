# Client contract consumer

Page handlers use `orm.PageResult`, `orm.SimplePage` and `orm.CursorPage`, with the
existing `httppagination.MemberResponse` DTO and explicit `PresentMember` accessor
mapping. Reporting contracts reuse the published `reporting` rows schemas and
datatable sources; they do not redeclare projections or persistence models.

The endpoint declarations, DTO identities, TypeScript sources and frozen contract
artifacts remain unchanged. `TestDatatableContractBytes` compares every generated
manifest, OpenAPI and client artifact to its frozen baseline. The native TypeScript
gate retains strict negative contracts, real HTTP/WebSocket behavior, optional
React/Vue form adapters and bundle assertions.

The accepted `httppagination` ORM migration still needs integration in this base;
its public result aliases remain compatible. No generated output is retired here.
