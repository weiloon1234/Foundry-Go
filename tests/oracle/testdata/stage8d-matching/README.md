# Extension matching classifications

Twelve legacy observations cover translation, metadata and attachment matching in
found, empty, corrupt and overflow modes. Each records success plus Invalid,
Conflict and Canceled classifications from the legacy error. Native manager SQL
and scoped predicates continue to replay the Stage 8c fixture records.

Provenance records the actual fixture command, build environment and source hashes.
`sources/` preserves changed capture inputs as text; remaining source hashes refer
to commit `e7b4d8a4`. This independent bundle avoids rewriting the earlier framework
capture's manifest. No current ORM result supplied an expected value.

Publication from `tests/oracle`, authorized by Stage 8d decision 4:

```sh
go run ./cmd/freeze-oracles --freeze --directory testdata/stage8d-matching --input /private/tmp/goway-8d2-matching/bundle.json
```

The capture uses the shared format, validator and publication receipt. Ordinary
fixture tests only replay it. Capture and replay retain no application credentials.
