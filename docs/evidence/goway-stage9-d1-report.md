# Stage 9 D1

Base: `6d80111`; D8 is preserved as a separate delta. No commit.

## Published foundations

- **profiles:** `Profiles`, `ProfileIdentity`, `Tables()`, an ORM-backed extension
  owner, service identity calls and ORM cleanup hooks. The existing PostgreSQL
  lifecycle test now uses ORM insert, soft delete, restore and force delete,
  retaining persisted-state, rollback and file-cleanup assertions. It is compiled
  and vetted here; live PostgreSQL remains the lead's gate.
- **articles:** author/article tables, both relations, identities, pinned owner,
  thin `ArticleSlots` and `ORMExtensionDeclaration()`. Policies and stored slot
  names come from the existing policy factory. Register only one cleanup observer
  family per application. Existing workflows retain generated callers until D3.
- **passwords, multifactor:** typed account tables, sensitive password carriers,
  nullable backup hash, identities and `Tables()`. Authentication workflows remain
  for D3. No plaintext/hash conversion or claim-freshness behavior changed.
- **Retained without edits:** genericdto, unions, localization, httpuploads,
  requestflow, internal/authfixture and internal/clientfixture. These are supported
  DTO/enum/config/transport contracts, or already handwritten identities/helpers;
  converting their ordinary structs into persistence models would be incorrect.

New tests check `orm.Check`, old/new SQL and arguments, exact serialized identity,
old-reference parsing, extension storage subjects, extension declaration metadata,
soft-delete visibility, unbound runtime rejection and direct relation validation.
A generator-free Go-way auth fixture proves an explicit `FoundryIdentity` adapter
works with providers and proofs.

## Compiler rows and footprint

24 consumer cases are covered: the eight `extension_*` rows; attachment ID/owner;
metadata owner/value; translation owner/locale; auth provider key, assurance and
scope-proof owner; MFA retirement owner; recovery/revocation reference owner;
country key; and password setter/predicate/LIKE. Country's consumer source was
already valid against the migrated store; its pending row is reconciled here.
Thirteen previously pending rows become ported, with matching Go-way programs.
Eleven existing ports now have migrated consumer programs. Intended diagnostics
remain unchanged; neither compiler harness is weakened.

No framework API or public-method snapshot additions. No footprint budgets or
probe workloads changed. The existing consumer probes passed:

| Probe | Functions / ceiling | Symbols / ceiling | Bytes / ceiling |
| --- | ---: | ---: | ---: |
| model_type | 1,159 / 1,449 | 7,267 / 9,084 | 4,769,542 / 5,959,555 |
| model_isolated | 139 / 174 | 1,051 / 1,314 | 630,100 / 786,148 |
| model_draft | 225 / 282 | 1,566 / 1,958 | 992,806 / 1,239,298 |
| endpoint_plain | 1,065 / 1,332 | 7,314 / 9,143 | 4,851,184 / 6,059,643 |

## Dependencies and requests

- **Lane B / recovering:** `Saving` in `recovering/hooks.go` removes a pending
  EmailRevision assignment when email is unchanged. `database/orm/write_draft.go`
  provides typed reads and replacement, but no typed unset. Preserve omission;
  assigning the old value is not equivalent. Recovery's declaration remains
  blocked pending an assignment-removal capability.
- **Extension owner / matching:** attachments, translations and metadata expose
  `Matching` only through `matching_legacy.go`, returning `query.Predicate[M]`.
  They need a public, bounded, owner-typed ORM matching predicate using the
  existing manager matching logic. `attachment_query_owner` remains pending;
  constructing a predicate from unrelated keys would not preserve its assertion.
- **Lane C:** authenticating and nested binding cutovers depend on the integrated
  models/queryfixture foundations. **Lane B:** D2 adapters and reference-key
  negatives await mutatorqueries/linkqueries stored-key foundations.

## Ownership and docs notes

Shared edits: only D-owned catalogue rows in
`blueprint/goway-data-layer/compilefail-mapping.tsv`, own entries in
`tests/fixtures/goway/compiler_cases_test.go`, and the consumer README. Root/editor
harnesses, public-method snapshots and budgets are unchanged.

No model directive, generated file, ownership manifest, module pin, Makefile,
OpenAPI or TypeScript artifact changed. The lead owns Makefile generation at the
barrier. Lead-owned docs note: D1 publishes four model foundations; profiles'
service/lifecycle consumer has cut over, while generated APIs remain for
coexistence and parity oracles. DTO and transport generation stays supported.

Exact acceptance commands, results, limitations and source hashes are recorded in
[the evidence JSON](goway-stage9-d1.json). Logs are in
`/private/tmp/goway-stage9-d1/`.
