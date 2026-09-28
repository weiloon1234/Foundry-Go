# Typed password hashing

Typed Argon2id passwords and hashes integrate with generated model fields, model-first login, lockout and checked credential issuance. See [recovery](account-recovery.md) and [credential changes](credential-changes.md) for related flows.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Hash and check

```go
hasher, err := password.New(password.DefaultConfig())
// Check err; share this hasher across login/reset operations.
plain, err := password.NewPlaintext(secret.New(submittedPassword))
// Check err and apply the application's password-strength validation.
stored, err := hasher.Hash(ctx, plain)
// Check err. stored is password.Hash, never a plaintext string.
matched, err := hasher.Check(ctx, plain, stored)
// Check err and matched before constructing any authenticated proof.
```

`password.Plaintext` and `password.Hash` are distinct types. Formatting and JSON
redact both. `Hash.Encoded()` explicitly returns a `secret.String` for a persistence
or migration adapter; `ParseHash(secret.String)` restores a bounded canonical PHC
value. The stored hash is preserved; custom getters do not replace it. Generated
models use the sensitive `password.Codec()` automatically.

Hashing uses the Go project's approved `golang.org/x/crypto/argon2` implementation.
Default Argon2id policy is 64 MiB, three passes, four lanes, a random 16-byte salt
and a 32-byte derived key. This matches the constrained-memory parameter choice
in the [official package guidance](https://pkg.go.dev/golang.org/x/crypto/argon2).
Passwords are never trimmed, case-folded, Unicode-normalized or silently truncated.
Inputs must be nonempty and at most 4,096 bytes; domain strength validation remains
explicit through `Plaintext.Secret()` when needed. A mismatch returns `false, nil`; invalid hashes, invalid input, overload
and canceled work return errors. Login transport must avoid revealing whether the
account, hash or supplied password caused rejection.

## Generated login inputs

```go
//foundry:dto
type LoginRequest struct {
    Email    string             `json:"email"`
    Password password.Plaintext `json:"password"`
}
```

Generation discovers the password's string wire contract through its native
`JSONContract`. Runtime decoding constructs bounded plaintext, while later logging
and JSON encoding remain redacted. Hash deliberately has no public JSON contract.
The [consumer example](../../tests/fixtures/consumer/passwords/passwords.go) keeps
hashing and checking typed without converting either value into a public model DTO.
This declaration does not register a login route or decide model eligibility.

## Rehash and resource policy

`NeedsRehash(stored)` compares the stored parameters, salt length and key length
with the configured issuance policy. It does not authenticate or write anything.
Only update after a successful check, using a compare-and-swap against the original
stored hash so concurrent password changes cannot be overwritten. Parameter policy
changes can raise or lower costs; choose deployment policy deliberately.
`auth.PasswordLogin` makes this decision after verification and calls the model's
typed conditional replacement, as described below.

Verification ceilings are separate from issuance parameters. Defaults permit
existing hashes up to 128 MiB, six passes and eight lanes, with at most two active
KDF operations per shared hasher. Parameters and salt/key lengths are bounded before
any Argon2 allocation. A hash above this instance's ceiling is rejected; configure
an explicit ceiling suitable for supported legacy hashes rather than letting stored
parameters choose unbounded memory/CPU work. New hashing requires at least 19 MiB
and two passes. Only version 19 Argon2id PHC values are supported in this implementation.

The timeout controls admission and result publication. Argon2 itself cannot stop
mid-computation, so cancellation waits for actual callback exit, keeps the capacity
slot occupied, and discards a late result. Overload is a classified conflict rather
than an unbounded work queue. Each call owns its temporary byte buffers; the framework
does not promise that passwords or Argon2 working memory can be securely erased from
Go's process memory. Rate limiting and model login policy remain separate controls.

## Typed stored model fields

```go
//foundry:model table=accounts
type Account struct {
    ID      model.ID[Account]
    Email   string
    Digest  password.Hash
    Backup  value.Nullable[password.Hash]
    Enabled bool
}
```

Generation supplies `AccountDraft{}.SetDigest(hash)`, typed hash predicates,
hydration and nullable `SetBackup`/`ClearBackup` operations. Hash fields accept
stored `password.Hash` values, never plaintext or arbitrary strings. They expose
scalar equality rather than string `LIKE` operators. Use equality for conditional
replacement; verification of submitted plaintext always belongs to the hasher.

`password.Codec` stores canonical PHC text and marks its values sensitive. The
same metadata redacts automatic audit capture even for a field named `Digest`,
and rejects model identity and cursor keys containing a hash. Nullable and
validated codecs retain the flag. Ordinary model JSON redacts the hash; it is
still not a public DTO contract. Explicit SQL binding and `Hash.Encoded()` are
intentional persistence boundaries that reveal stored bytes to the database.

The generator adds an owned comment to the handwritten model field and generated
field/setter declarations. It tells completion, hover and source readers that this
is a sensitive stored hash and where verification belongs. No separate annotation
or configuration is needed. Existing getter/setter notices are preserved alongside
this notice. The [model consumer](../../tests/fixtures/consumer/passwords/account.go)
and [integration test source](../../tests/fixtures/consumer/passwords/account_postgres_test.go)
exercise typed CRUD, projections, conditional updates and audit/cursor protection.

## Model-first login

`auth.NewPasswordLogin(provider, hasher, binding, auth.DefaultConfig())` connects
a model provider to `auth.PasswordModel[Account, LoginKey]`. Its five callbacks
are domain behavior: look up a model by the typed login key, lock its current model
at issuance, read its stored hash, conditionally replace a hash, and evaluate
whether MFA is required. The existing
provider still owns current model eligibility. All callbacks are required; no
implicit MFA policy is assumed.

```go
result, err := login.Authenticate(ctx, email, request.Password)
if err != nil {
    return err
}
account := result.Subject() // Already resolved concrete model.
proof := result.Proof()     // Model/key types retained.
// Pass proof to session/token issuance using its matching assurance mode.
```

The [complete consumer binding](../../tests/fixtures/consumer/passwords/login.go)
uses generated `Where(...).First(...)` and a typed conditional `Update`.
Foundry performs one login lookup, verifies the password, checks provider
eligibility, rehashes when needed, and evaluates MFA policy. It does not invoke the
provider's identity lookup again. A normal successful result contains an
`Authenticated` proof; a required second factor produces only `PendingMFA`.
Ordinary guards reject pending proofs. Reading `Subject()` is not a shortcut
around second-factor authorization. The result's formatting and JSON omit its
model and proof; map an explicit response DTO.

The replacement callback compares the original hash inside its write transaction
and returns the complete updated model. A lost comparison rejects the attempt.
Foundry verifies the returned identity/hash and checks eligibility again after a
replacement; it never retries an uncertain write. Model hooks can roll back the
normal update. Credentials are issued subsequently, using an automatic proof
check that locks and revalidates the post-rehash model inside the issuance
transaction. A reset that wins that lock prevents issuance from the old proof;
a reset that follows issuance revokes its credentials. HTTP delivery remains
outside the transaction. See [credential changes](credential-changes.md) for the
typed revocation group and lock/rollback contracts. These additions passed the milestone gate.

Missing accounts, disabled models, unusable hashes and mismatched passwords all
produce `auth.Unauthenticated`. Missing or unusable credentials run bounded dummy
verification using the same hasher admission policy. This removes a cheap rejection
branch, but legacy hash costs and database latency still differ; it is not a
constant-time account-discovery guarantee. Operational errors retain their cause
behind safe formatting. Every failure or cancellation returns a zero result.
Lockout classification inspects at most 256 error nodes and 64 wrapping levels.
An unreached authentication marker remains an operational failure: it grants no
authority and does not count as a confirmed failed password. Error methods and
callbacks must still return; these bounds do not interrupt application code.

The login callback limit uses `auth.Config`; share a hasher to bound total Argon2
work. Callbacks that ignore cancellation retain capacity until their actual exit.
Apply request-rate limiting before verification and use `login.WithLockout` for
shared failed-password protection. See [typed login lockout](login-lockout.md).
The callback capacity bound is not a request-rate limit; successful passwords do
not clear IP quotas. Lockout/HTTP integration passed the milestone verification phase.

`provider.RecheckPassword(ctx, tx, result)` reuses that issuance validator and
returns the current locked model for a sensitive password-authorized change.
It requires the same provider declaration as the original login and performs
no second provider lookup. Keep the result request-local; the recheck verifies
current state, not a persisted ticket's age. Pending MFA still requires its
second factor. See [typed MFA factors](mfa.md). This addition passed the milestone gate.
