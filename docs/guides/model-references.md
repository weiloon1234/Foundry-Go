# Stored model references and attribution

The [master roadmap](../../blueprint/00-master-architecture-and-parity.md) records completed runtime, PostgreSQL, compiler-rejection, real-gopls and full-repository acceptance.

Declare an identity from the handwritten table and its own primary-key handle:

<!-- compiled-source: tests/fixtures/goway/guideadapters/examples_test.go -->
```go
var UserIdentity = orm.IdentityOf(models.Users.Table, models.Users.Key())

func restoreUser(user models.User) (model.Reference[models.User, model.ID[models.User]], error) {
	reference := UserIdentity.ReferenceOf(user)
	identity, err := reference.Identity()
	if err != nil {
		return model.Reference[models.User, model.ID[models.User]]{}, err
	}
	return UserIdentity.Parse(identity)
}
```

For a model-owned UUID, `reference.Key()` retains `model.ID[models.User]`. For a natural key it
retains the declared key type. `Reference[Member, Key]` preserves both owners in
Go signatures, so another model's reference cannot be assigned accidentally.
The identity reuses the table declaration and primary-key codec.

The reference reads the stored primary field and reuses its database codec.
It does not invoke `AccessID`, run a setter, serialize the full model, or load a
record. Document getter behavior on the handwritten model and handles. Use getters
explicitly for presentation; identity remains a persistence boundary.

## Capturing provenance

<!-- compiled-source: tests/fixtures/goway/guideadapters/examples_test.go -->
```go
func withOrigin(ctx context.Context, account Account) (context.Context, error) {
	origin, err := (attribution.Origin{}).WithModel(account)
	if err != nil {
		return nil, err
	}
	origin, err = origin.WithGuard("session")
	if err != nil {
		return nil, err
	}
	return attribution.WithContext(ctx, origin)
}
```

`Origin` is an immutable snapshot of the stored model identity and optional
request metadata. The zero value is valid anonymous provenance. `WithSystem`
replaces a model subject with a semantic system identifier, and clears the guard.
`WithRequest` records a typed request ID, parsed `netip.Addr` and user agent.
The HTTP adapter determines trusted client addresses before capture. Request IDs
are limited to 128 bytes and user agents to 4096 bytes; control characters and
local IP interface zones are rejected. Transport adapters capture untrusted user
agents through `attribution.SanitizeUserAgent`, which always yields valid metadata.

Changing the original model or creating another context later cannot change a
captured origin. No credentials, permissions, full authenticated model or database
handle are retained. A parsed origin is metadata, never authentication proof.
Authentication adapters still own concrete subject resolution and authorization.

## Explicit serialization boundaries

`model.Identity` deliberately erases the model/key type only where audit and
other polymorphic transports need it. It records the declared table namespace
and the exact SQL key representation, including its driver kind. Exact integers
remain decimal text; bytes use base64; instants retain precise UTC text. Natural
strings are not trimmed or formatted. Decimal and JSON keys reuse their existing
persistence codecs.

`json.Marshal(identity)` exports this metadata. `identity.KeyJSON()` explicitly
exports only the tagged key representation. SQL NULL and malformed or oversized
keys fail; the complete encoded key is limited to 4096 bytes. Ordinary formatting
of a reference, identity or origin omits its captured data.

At a transport boundary, restore the concrete model and key types:

Restore an exported identity with `UserIdentity.Parse(identity)`.

Restoration checks the expected model namespace and database codec. A value must
round trip without changing its persisted representation. It performs no query
and makes no promise that the record exists or the caller may access it.
Malformed JSON, unknown or duplicate fields and invalid key values fail without
replacing an existing destination.

Custom database codecs own their work and must be pure, concurrency-safe and
return owned decoded values. Identity capture/restoration contains codec panics
and `runtime.Goexit` without exposing their payloads, while preserving ordinary
error identity. This does not make an uncooperative custom codec cancellable.

For the old-to-new API mapping, see [ORM migration](orm-migration.md).

The ORM examples are checked against the [compiled guide examples](../../tests/fixtures/goway/guideadapters) and the linked consumer sources.
