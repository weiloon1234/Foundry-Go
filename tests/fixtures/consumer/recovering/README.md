# recovering consumer

`MemberRecords` declares the existing recovery table, password codec and typed email revision. `MemberIdentity` preserves its stored identity. Before-write hooks rotate revisions only when email changes; otherwise `orm.WriteUnset` removes any submitted revision. The `UPDATE OF email_revision` trigger regression distinguishes omission from assigning the old value.

Password reset, verification, login rehash and revision tests use these ORM declarations. The supplied transaction still owns model-first locks, challenge consumption and stable credential revocations. Freshness, redaction, single-use and uncertain-commit assertions remain unchanged. Generated callbacks temporarily share the same revision policy; ordinary consumers use only the ORM callback factory.

PostgreSQL sources remain opt-in; the lead runs the live service gate.
