# Authenticated encryption

The `encryption` package is the shared authenticated-encryption boundary for Foundry features. MFA uses it for stored factors; later features reuse the same keyring and cipher contracts.

Milestone 10 passed native acceptance. See the [master roadmap](../../blueprint/00-master-architecture-and-parity.md#milestone-10-acceptance) for verification evidence and later integration boundaries.

## Keys and record ownership

Load an explicitly configured key using `encryption.ParseKey(id, secretValue)`,
then construct an immutable `encryption.NewKeyring(activeID, keys...)`. A key is
32 random bytes encoded as canonical unpadded base64url. `GenerateKey` supports
explicit setup: persist its `Secret()` in the application's secret store before
using it for durable records. Generating a replacement at startup would make
existing records unreadable. Construction performs no I/O.

Every operation requires an `encryption.Context`: a nominal `Purpose` and exact
record binding. Construct the binding from trusted tenant/model/record identity,
including an enrollment generation when replacing a factor. Read it from the
owning record's identity, not a submitted ciphertext or untrusted owner field.
The binding is authenticated but is not embedded in the envelope.

```go
owner, err := encryption.NewContext("auth.mfa.totp.v1", recordBinding)
if err != nil {
    return err
}
encrypted, err := keys.Encrypt(ctx, owner, factor.Secret())
if err != nil {
    return err
}
// Persist encrypted.Encoded() at the explicit storage boundary.
```

The [independent consumer](../../tests/fixtures/consumer/multifactor/primitives.go)
provides complete typed functions for encryption, decryption and rotation. This
fixture demonstrates primitives; it does not implement factor enrollment or grant
authentication authority.

## Envelope and limits

`fg1:<keyID>:<payload>` stores a version, key ID and canonical base64url payload.
The payload contains the nonce, ciphertext and authentication tag. Version, key ID,
purpose and exact binding are authenticated with unambiguous length prefixes.
A parsed `Ciphertext` has valid structure; only `Decrypt` authenticates it.
Unknown keys, altered ciphertext and wrong bindings return no plaintext. There is
no key fallback, implicit format upgrade or unauthenticated legacy decoding.

Encryption uses Go's standard AES-256-GCM with randomly generated 96-bit nonces.
Go requires no more than 2^32 encryptions with one key's material. Copies of a
Foundry key share a local counter, including across keyrings. Loading the key
again, restarting or using another process creates another counter: **the local
counter is not an aggregate usage guarantee**. Operators must rotate keys before
that total across all instances and restarts. Duplicate IDs or material within
one keyring are rejected. See the [Go cipher documentation](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce).

A keyring accepts at most 32 keys; plaintext is limited to 1 MiB, context bindings
to 8 KiB. Operations check cancellation at entry and before returning output; the
bounded local cipher call is not interruptible. An admitted encryption consumes
its local budget even if cancellation prevents publication. This API does not
provide stream encryption or global concurrency admission.

## Rotation and disclosure

Install the next key alongside retained keys and make it active. `Reencrypt`
authenticates the old envelope with its retained key and emits a new envelope under
the active key. Callers atomically replace the persisted value using the same
record binding. Retire old keys only after all relevant records have migrated.
Key IDs must never be reassigned to different material.

Keys, contexts and ciphertext redact ordinary formatting, JSON and structured
logging. `Key.Secret()`, `Ciphertext.Encoded()` and decrypted `secret.String.Reveal()`
are explicit disclosure boundaries. Do not include those outputs in diagnostics.
Redaction does not promise memory erasure, key custody, or protection after an
application deliberately reveals a value.

Written tests cover tampering across every envelope byte, owner/purpose/key-ID
substitution, binary context preservation, retained-key rotation, missing keys,
input/resource bounds, cancellation, concurrent local budget exhaustion and
redaction. They passed milestone 10 acceptance.
