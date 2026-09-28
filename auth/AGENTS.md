# Authentication and credential changes

These instructions add to the [repository rules](../AGENTS.md). Read the relevant
[authentication](../docs/guides/authentication.md),
[credential-change](../docs/guides/credential-changes.md) or
[MFA](../docs/guides/mfa.md) contract and its compiling consumer before changing a flow.

## Identity and current authority

- Preserve concrete model/key types and exact provider/guard/policy declaration
  identity. Matching a name or raw ID cannot substitute another declaration/model.
  A proof constructor is a trusted adapter boundary, not credential verification;
  never turn a submitted identity into a proof. Pending MFA cannot authorize ordinary
  handlers or policies.
- Keep credential verification and model resolution scoped to an explicit request
  or authorization boundary. A new scope observes revocation and current eligibility;
  the current scope retains its model snapshot. Do not promise continuous refresh or
  cache an authenticated scope for an entire WebSocket connection.
- Evaluate registered permissions/resource policies on every check, preserving
  tenant/resource ownership. Credential scopes restrict grants; they do not replace
  these decisions. Never convert an authority failure into an allow decision.
- Treat reference-valued fields in resolved models as read-only; clone before
  mutation. Preserve owned callback results and the existing once-per-guard lookup,
  without adding a second actor representation or redundant model hydration.

## Sensitive transitions

- Keep current-model locking, password-proof revalidation, challenge/factor
  consumption and required credential revocations in the supplied transaction.
  Preserve model-first and stable revocation lock ordering. Register every applicable
  session/token guard in the model's existing revocation group.
- Preserve single-use challenges/recovery codes, TOTP replay tracking, refresh-family
  reuse detection and atomic rotation. Keep the documented absolute deadlines,
  scope ceilings and assurance transitions. Do not add a replay grace period or
  automatically retry ambiguous credential mutations.
- Reuse the existing [password](../docs/guides/password-hashing.md),
  [encryption](../docs/guides/encryption.md), random-token and secret owners.
  Preserve bounded verification/lockout admission and trusted encryption purpose/
  record bindings. Do not invent cryptography or regenerate durable keys at startup;
  rotation must retain keys needed by stored records.
- Disclose newly issued secrets only through the existing explicit delivery boundary
  after confirmed commit. Public IDs and safe metadata never authenticate a caller.
  Database commit and HTTP delivery remain separate outcomes. For browser/HTTP
  changes also read [http/AGENTS.md](../http/AGENTS.md), preserving CSRF, cookie and
  session-fixation protections.

## Evidence to select for the completed batch

Exercise denial as well as success: wrong model/guard/provider, tenant/resource
mismatch, pending assurance, expiry, revocation and replay. Use real PostgreSQL
coverage for changed lock/transaction behavior, including concurrent consumption,
rollback and persisted outcomes. Reuse injectable clocks for deadline cases and
independent consumers/compile-fail checks for type changes. Select relevant races
and transport tests after the completed implementation batch, as the root requires.
