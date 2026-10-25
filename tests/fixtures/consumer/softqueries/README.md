# Soft-delete query fixture

`Members`, `Groups` and `Memberships` declare soft-delete fields and expose their
`Soft` tokens. `MemberLinks` owns relation targets and pivots; `MemberCounts` binds
aggregate slots. `MemberLabels` declares the projection DTO.

The application registers ORM hooks only. Tests retain delete/restore/force-delete
ordering, visibility, rollback, cancellation, natural keys, target/pivot scopes,
outer joins and keyset reads. Force deletion passes the matching declaration token.
Factories use `factory.Define` with typed assignment slices and preserve transactional
per-model hooks. Generated output remains for the integration barrier.
