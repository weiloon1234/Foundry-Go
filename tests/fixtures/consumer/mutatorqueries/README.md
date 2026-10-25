# Mutator query fixture

`Members` is the published ORM table. All mutated fields use Input carriers,
including same-type input/storage pairs: Email and Nickname use string, Attempts
uses int, and State uses the existing generated enum. `Nickname.SetNull()` preserves
SQL NULL; omission is the absence of an assignment. Stored-value predicates do not
invoke a mutator.

`MemberIdentity` preserves the stored `model.ID[Member]`. `Member.AccessID`,
`AccessEmail` and `AccessNickname` remain explicit presentation calls with the
existing `DisplayEmail` and `DisplayNickname` result types. Dependent adapters can
migrate without changing model, enum, key or response identities.

The generated APIs and directives remain for downstream cutovers. The stored-change
PostgreSQL test now uses ORM comparison and persisted field changes; remaining
legacy operation tests remain executable until their package cutover.

CRUD, reusable conflict literals, proposed values, stored change snapshots and
reference/provenance assertions now exercise ORM. SQL calculations cannot bypass a
Go mutator; explicit accessors still produce presentation values without altering
stored identity. Generated declarations remain for the shared retirement barrier.
