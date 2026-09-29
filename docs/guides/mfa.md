# Typed MFA factors

Encrypted TOTP factors and hashed recovery codes integrate with model-first enrollment, management and transactional pending-session/token completion.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Model-first assembly

Create `mfa.Store` from the borrowed PostgreSQL adapter, configured encryption
keyring and `mfa.DefaultConfig(namespace, issuer)`. Register and apply
`auth/mfa/postgres.Migrations()` explicitly; construction never mutates schemas.
The adapter's schema contains both the factor table and domain model queries.

Bind `mfa.Factors[M,K]` with the existing model provider, a separate typed MFA
lockout throttle and `mfa.Model[M,K]`. Domain callbacks lock the current model by its concrete key, select the stored enabled
flag, update it through normal generated model writes, provide the account label,
enforce required-factor policy and invalidate credentials in the supplied
transaction. Pass `auth.Revocations.Invalidate` for all registered session/token
guards.

Link the factors to login with `login.WithSecondFactor(factors)`. Login and the
issuance recheck then require a second factor whenever `RequiresMFA` or the
stored enabled flag says so, so an enrolled account can never bypass MFA because
a `RequiresMFA` callback forgot the flag. `RequiresMFA` remains the place for
domain requirements, such as mandatory MFA for administrators. `mfa.Factors`
implements `auth.EnrolledFactors`, which reads only the model's enabled flag.

The [consumer model and binding](../../tests/fixtures/consumer/multifactor/account.go)
show the actual Go API. Foundry owns model rechecking, encrypted storage, factor
verification, lockout composition and transaction ordering. There is one factor
per namespace/provider/model/key, shared across that model's guards.

| Operation | Required input | Result |
|---|---|---|
| `Enroll` | Current request's password result | Model-owned enrollment ID, redacted secret/URI, expiry |
| `Confirm` | Password result, matching enrollment ID, `TOTPCode` | Updated model and one new recovery-code set |
| `Disable` | Password result, `Response`, domain permission | Model with MFA disabled |
| `RegenerateRecovery` | Password result and `Response` | Complete replacement recovery-code set |
| `Reencrypt` | Password result and `Response` | Factor encrypted under the active retained keyring |
| `Prune` | Bounded maintenance batch size | Number of expired pending enrollments removed |
| `Verifier` | Typed `Response` | Request-local second-factor action for session/token completion |

`Response` is constructed with `TOTPResponse(code)` or `RecoveryResponse(code)`.
Confirmation accepts only `TOTPCode`; passing a recovery code fails compilation.
Enrollment IDs retain their model type, including across explicit conversion.

## Transaction behavior

Each factor-management operation rechecks the password and locks the current model first. It then
locks the factor row. The model lock also serializes first enrollment when no
factor row exists. Credential revocations run afterward in their registered
order. Callbacks must use the supplied transaction and preserve model identity.
No Actor hydration or second provider lookup occurs.

`Enroll` replaces only an unconfirmed enrollment, creating a new ID, secret and
expiry. Its default lifetime is ten minutes, configurable up to one hour. An
enabled or confirmed factor cannot be overwritten by a new enrollment request.
`Confirm` checks the current ID and expiry, verifies TOTP, records the accepted
step and recovery hashes, enables the model flag and revokes registered
credentials in one transaction. The backend rechecks the expiry after protected
writes, before returning to commit. Context deadlines also bound the operation.

MFA lockout has its own declaration and model-key type. While holding the database
locks, Foundry calls the bounded throttle with verification that changes only
owned memory. Management applies protected writes after the throttle's final decision succeeds.
Login completion provisionally consumes its pending credential first, in the same
transaction; a failed throttle decision rolls that deletion back. Late rejection leaves the persisted replay step/recovery code
unchanged. Failed attempts may remain counted if a later database operation
fails; Redis lockout state is not part of the PostgreSQL transaction. Apply request
rate limiting before password/MFA work as well. Rejection inspection stops after
256 error nodes or 64 wrapping levels. An unreached lockout rejection remains an
operational failure, invokes no rejection observer and publishes no verified
factor or consumed recovery snapshot. Custom error methods must return.

Pre-commit errors, callback panic/abnormal exit and cancellation roll back the
factor, model and credential revocations together. Results expose no new secrets
on failure. After-commit callback errors can instead retain a `database.Committed`
outcome: do not retry blindly or assume the action was undone. For example, if
confirmation committed but delivery failed, authenticate the password again and
use a new TOTP step to regenerate recovery codes.

`Disable` verifies an existing factor and enforces `CanDisable` before clearing
MFA and revoking credentials. `RegenerateRecovery` consumes its submitted factor,
replaces every recovery hash and revokes credentials in the same transaction.
`Reencrypt` preserves enrollment identity and secret, while consuming the factor
used to authorize it; it does not change policy or issue credentials. Retain old
encryption keys until every factor has migrated.

`Prune` removes at most 128 expired pending records, skipping locked rows. It never
locks application models or deletes confirmed factors. Model deletion/orphan
maintenance and protected operational orchestration remain separate integration
work.

## Complete a pending login

A pending credential proves only the first authentication stage. Ordinary guards
continue to reject it. Construct a typed verifier from the submitted factor and
pass it to the original session or token binding:

```go
response, err := mfa.TOTPResponse(request.Code)
if err != nil { return err }
factor, err := factors.Verifier(response)
if err != nil { return err }
issued, err := sessions.CompleteMFA(ctx, pending, factor, session.IssueOptions{})
```

`Tokens.CompleteMFA` has the same shape with `token.IssueOptions[User]`. Choose
remember/refresh/scope options from server policy. The declared scope ceiling
still applies. `Verifier` performs no I/O and is not a proof; no reusable full
proof escapes completion. See the actual [consumer functions](../../tests/fixtures/consumer/multifactor/completion.go).

Completion locates the pending credential, then locks the current model once,
checks current eligibility and the enabled factor, consumes the pending credential
provisionally, and verifies/consumes the second factor in the full credential's
creation transaction. It returns a fresh secret for the same guard/provider.
There is no Actor and no second model-provider lookup. Concurrent attempts can
consume a pending credential only once. A losing or failed transaction retains
its unconsumed recovery code/TOTP state. Credential expiry is rechecked after
insertion, before returning to commit; this is not a guarantee about the wall-clock
instant at which a remote commit completes.

The provider must be the exact declaration used by the credential guard. The
PostgreSQL factor and credential adapters must borrow the same `*database.DB`;
independent pools cannot share the transaction. Different validated schemas may
be scoped through savepoints; model queries run in the factor adapter's schema.
Custom adapters must implement the joined transaction capabilities and preserve
callback ownership, locking and rollback. Missing capabilities fail closed.

## HTTP delivery

The [consumer routes](../../tests/fixtures/consumer/multifactor/routes.go) compose
ordinary typed endpoints with:

- `http.MFAEnrollmentResponse[User](201, clock)` to disclose the committed
  enrollment ID, base32 secret, provisioning URI and expiry.
- `http.MFARecoveryResponse[User](200)` to disclose the recovery set returned
  by `Confirm` or `RegenerateRecovery`.
- `browser.CompleteMFA(ctx, factor, options)` to consume the bound pending cookie
  and stage its authenticated replacement through the existing response lifecycle.
- `http.MFATOTPBody()` or `http.MFARecoveryBody()` for a token challenge plus
  its distinct factor type; return the issued pair through `http.TokenResponse`.

Secret responses require POST and TLS, respect configured trusted proxies, set
no-store/no-cache headers, and encode bounded JSON using generated DTO metadata.
Ordinary `Enrollment`, `RecoveryCodes`, token results and input-value serialization
keep secrets private. Enrollment delivery rejects a clock before creation or at/
after expiry. A pending token is supplied in the JSON body, never in a URL; browser
completion reads only its bound cookie. These input parsers do not authenticate.

Use `browser.Middleware()` on public enrollment/challenge routes: it protects
password submissions and pending-cookie completion with CSRF and cookie policy.
A normal guarded endpoint rejects pending credentials before its handler.
Require a freshly verified password for enrollment/management; confirmation
accepts a typed `mfa.EnrollmentID[User]` and `TOTPCode`. The ID supplies its JSON
contract automatically, preserving its model owner in generated Go metadata.
Keep ingress rate limiting before expensive password/MFA work.

Cookie publication waits for successful handler and response preparation. Database
commit and HTTP delivery cannot be atomic: a later failed response may leave the
pending credential consumed and a full credential undisclosed. Restart login;
do not replay completion or retry uncertain writes automatically. An undisclosed
full credential remains until expiry/revocation. Lost enrollment delivery can be
replaced while still unconfirmed; lost recovery delivery needs new factor-authorized
regeneration. The framework does not retain plaintext recovery codes to retrieve.

## Separate input types

`mfa.TOTPSecret` holds a canonical 160-bit base32 secret. Generate it using
`GenerateTOTPSecret`; parse existing material explicitly with `ParseTOTPSecret`.
The manager persists secrets through the shared [encryption package](encryption.md),
bound to the owning account and enrollment generation. Ordinary logs and JSON redact them.

`mfa.TOTPCode` accepts exactly six ASCII digits and preserves leading zeroes.
`mfa.RecoveryCode` accepts a distinct canonical 43-character base64url credential.
Neither performs trimming, Unicode digit conversion or automatic type guessing.
Parsers take `secret.String`; `Secret()` explicitly returns a redacted secret for
an adapter. Parsing is input validation, never authentication.

Go declarations own request contracts:

```go
//foundry:dto
type TOTPRequest struct {
    Code mfa.TOTPCode `json:"code"`
}

//foundry:dto
type RecoveryRequest struct {
    Code mfa.RecoveryCode `json:"code"`
}
```

Both code types supply `JSONContract`, so the generator discovers their wire
string shape and produces typed request fields without parallel configuration.
Their JSON decoder validates input; their JSON encoder redacts it. Do not use
these input DTOs to deliver newly issued recovery codes. See the actual
[consumer declarations](../../tests/fixtures/consumer/multifactor/primitives.go)
and generated DTO files in that directory.

## TOTP and provisioning

The implementation uses HMAC-SHA1, six digits and a 30-second period, retaining
Rust Foundry's interoperability choices. Its private matcher accepts a server
window of zero or one adjacent step in each direction. It examines each allowed
step and returns the highest matching step greater than the previously used step.
The manager persists that step under the factor lock in the same transaction
as the protected action. A primitive match in memory alone cannot ensure single use. This
follows the time-step and replay requirements of [RFC 6238](https://www.rfc-editor.org/rfc/rfc6238.html).

`ProvisioningURI(secret, issuer, account)` returns a redacted `secret.String`
containing an `otpauth` URI. Labels are bounded, colon-free UTF-8 with no control
characters. The issuer appears in both the label and query. Digits and period
come from the verification constants. Render a QR locally and disclose only
through an authorized enrollment response; the URI contains the full factor
secret. The format follows Google's [authenticator URI specification](https://github.com/google/google-authenticator/wiki/Key-Uri-Format).

## Recovery factors

Recovery generation and matching currently remain private to the MFA runtime.
Each code has 256 random bits and shares session/token credential hashing. Only
its distinct `RecoveryHash` persists. Hash sets are bounded to 16 and reject zero
or duplicate entries. Matching compares hashes in constant time; consumption
returns owned remaining state without mutating the caller's snapshot. Management
persists consumption in the protected operation's transaction. Password hashing is deliberately separate from
high-entropy recovery credential hashing.

Enrollment confirmation requires a TOTP code. Recovery codes cannot confirm
possession of an unconfirmed secret. New sets replace previous hashes atomically
with credential invalidation; `RecoveryCodes.Codes()` returns an owned slice.

## Password-authorized changes

`provider.RecheckPassword(ctx, tx, result)` accepts the concrete `PasswordResult`
from that exact provider declaration. It runs the original password binding's
locked lookup and returns the current model after checking its identity, verified
hash, eligibility and MFA policy. Credential issuance uses the same validator.
There is no Actor hydration or second provider lookup.

Recheck before acquiring factor or credential-store locks. Keep `PasswordResult`
within the server request; it is not a serialized, expiring reauthentication
ticket. This method checks account state, not elapsed time. A pending-MFA result
has verified only the password stage and cannot replace verification of an
existing second factor. The caller owns transaction lifetime, timeout, capacity
and authorization for the protected action.

The [consumer recheck](../../tests/fixtures/consumer/recovering/credentials.go)
shows the typed boundary. Zero results and different provider declarations are
rejected before a model lock; changed or disabled models, errors, panic, abnormal
callback exit and cancellation return no model.

## Key rotation

With `Features.Auth.MFA` enabled, `application.New` constructs the store from
the configured database, schema and the [application key ring](encryption.md#application-key-ring);
`Services.MFA()` returns it and `mfa.Config.Issuer` defaults to the application
name. Enabling MFA without application encryption keys fails the build.

`store.ReencryptStale(ctx, cursor, limit)` re-encrypts up to 256 stored factors
per call under the active key, across every model and provider sharing the store,
without any user's password or domain model. Each replacement is conditional on
the factor's generation and envelope, so it is safe alongside logins and factor
management; the secret, replay step and recovery hashes are unchanged and no
credential is revoked. Factors that changed concurrently are skipped and picked
up by a later run; envelopes whose key is no longer retained are reported as
failed. The PostgreSQL adapter implements the `mfa.RotationBackend` contract
(`StaleCiphertexts`, `ReplaceCiphertext`).

Register `application.MFACommand()` and run
`mfa reencrypt [--batch 128] [--max-batches 10000] [--format text|json]` after
making a new key active. It reports `reencrypted`, `changed`, `failed` and
`complete`, and fails when a factor uses a key that is no longer retained.
Remove the previous key only after a run reports complete with no failures.

## Acceptance coverage

Written primitive tests include RFC SHA1 vectors, time boundaries, accepted-step
replay, bounded drift, recovery state ownership and consumption, URI encoding,
secret redaction, input decoding, and generated DTO contracts. Written compiler
and editor cases cover separate secret/code/ciphertext types and model-preserving
password rechecks. These passed with the rest of milestone 10.

The [PostgreSQL consumer tests](../../tests/fixtures/consumer/multifactor/factors_postgres_test.go)
are written for replacement/confirmation, real session/token revocation, stale
password results, TOTP/recovery reuse, rollback across failures/cancellation/expiry,
late lockout, concurrent consumption, required-factor policy, pruning, key rotation
and committed after-commit failures. Runtime tests cover malformed callback order,
omitted/repeated calls and suppressed errors. These tests passed.

The [completion tests](../../tests/fixtures/consumer/multifactor/completion_postgres_test.go)
cover one current-model read, original guard/provider ownership, invalid scopes,
concurrent pending consumption, TOTP/recovery replay, disabled models, rollback,
expiry before commit, committed failures and foreign database pools. The
[HTTP tests](../../tests/fixtures/consumer/multifactor/http_postgres_test.go) cover
actual enrollment/confirmation wire contracts, browser CSRF/cookies, token
completion, malformed input, response failure/redaction and stale delivery.
Compiler and editor cases verify model/key/response ownership. Authentication events, attribution, recovery transport and operational cleanup are covered by the completed milestone.

## Security observations and account retirement

`WithObserver` exposes typed transactional change notices and separate immediate
rejection notices. `RetireIn` joins an authorized administrative account-deletion
transaction. See [events and maintenance](auth-operations.md) for concrete consumer
composition, rollback semantics, lock ordering and limits. These additions passed the milestone gate.
