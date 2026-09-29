package lease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// ErrExported marks a guard whose ownership was handed to another process.
var ErrExported = fault.New(fault.Closed, "lease ownership exported")

const tokenPrefix = "foundry-lease-v1."

// Token is a transferable handle to one held lease of family K: its complete
// address, opaque owner and duration. It carries the owner secret, so treat it as
// a credential: formatting redacts it; only MarshalText exposes it. The type
// parameter keeps a token from being restored into another family's handle.
type Token[K any] struct {
	key   Key
	owner Owner
	ttl   time.Duration
}

// Export hands a live explicit guard's ownership out of this process. The guard
// stops renewing and ends with ErrExported WITHOUT releasing the authority key;
// the importing process must Restore the token before the lease's TTL expires
// and becomes responsible for releasing it. The token is single-use (see
// Restore). The guard must belong to l's family.
func (l Leases[K]) Export(g *Guard) (Token[K], error) {
	if l.definition == nil || g == nil || g.manager == nil {
		return Token[K]{}, fault.New(fault.Invalid, "lease export requires a bound handle and guard")
	}
	if !l.owns(g.key) {
		return Token[K]{}, fault.New(fault.Invalid, "lease guard belongs to another family")
	}
	if err := g.Err(); err != nil {
		return Token[K]{}, err
	}
	if g.exported.Swap(true) {
		return Token[K]{}, ErrExported
	}
	token := Token[K]{key: g.key, owner: g.owner, ttl: g.ttl}
	g.cancel(ErrExported)
	return token, nil
}

// TransferBackend optionally hands a live lease to a new owner atomically:
// while from still owns key, the owner becomes to and the lease is renewed for
// ttl; otherwise nothing changes and it returns false. Restore requires it so
// every exported token is single-use. Memory and Redis implement it.
type TransferBackend interface {
	LeaseTransfer(ctx context.Context, key Key, from, to Owner, ttl time.Duration) (bool, error)
}

// Restore resumes ownership from an exported token. Tokens are single-use: the
// authority atomically replaces the token's owner secret with a fresh one and
// renews the lease, so restoring the same token again (a retried job or a
// redelivered message) returns ErrLost instead of creating a second holder. It
// returns an explicit guard (no automatic heartbeat) under the same rules as
// Acquire. A token whose lease expired, was released, was replaced or was
// already restored returns ErrLost. The backend must implement TransferBackend.
func (l Leases[K]) Restore(ctx context.Context, token Token[K]) (*Guard, error) {
	if l.definition == nil {
		return nil, fault.New(fault.Invalid, "lease handle is not initialized")
	}
	if err := token.validate(); err != nil {
		return nil, err
	}
	if !l.owns(token.key) {
		return nil, fault.New(fault.Invalid, "lease token belongs to another family")
	}
	backend, ok := l.manager.backend.(TransferBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "lease backend does not support restoring exported tokens")
	}
	owner, err := NewOwner()
	if err != nil {
		return nil, err
	}
	if err := l.begin(ctx, token.ttl, 0); err != nil {
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			l.manager.end()
		}
	}()
	parent, unlink := contextlink.Link(ctx, l.manager.ctx)
	until := time.Now().Add(validity(token.ttl))
	command, stop := context.WithDeadline(parent, until)
	transferred, err := l.manager.command(command, func(ctx context.Context) (bool, error) {
		return backend.LeaseTransfer(ctx, token.key, token.owner, owner, token.ttl)
	})
	stop()
	uncertain := err != nil || transferred
	if err == nil && (!transferred || parent.Err() != nil || !time.Now().Before(until)) {
		err = errors.Join(ErrLost, parent.Err())
	}
	if err != nil {
		if uncertain {
			// A lost or late reply may hide an applied transfer. Release the
			// fresh owner once; the consumed token cannot be restored again.
			err = errors.Join(err, l.manager.releaseOwner(token.key, owner))
		}
		unlink()
		return nil, err
	}
	handedOff = true
	return newGuard(l.manager, parent, unlink, token.key, owner, token.ttl, until, false, l.manager.end), nil
}

func (l Leases[K]) owns(key Key) bool {
	return key.Validate() == nil && key.address.Namespace == l.manager.config.Namespace && Name(key.address.Name) == l.definition.name
}
func (t Token[K]) validate() error {
	if err := t.key.Validate(); err != nil {
		return err
	}
	if err := t.owner.Validate(); err != nil {
		return err
	}
	return ValidateDuration(t.ttl)
}

// MarshalText encodes the token, including its owner secret, for transfer.
func (t Token[K]) MarshalText() ([]byte, error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	var raw bytes.Buffer
	a := t.key.address
	for _, part := range []string{a.Namespace.Application, a.Namespace.Environment, a.Name} {
		raw.WriteByte(byte(len(part)))
		raw.WriteString(part)
	}
	raw.Write(a.Hash[:])
	raw.Write(t.owner.value[:])
	var ttl [8]byte
	binary.BigEndian.PutUint64(ttl[:], uint64(t.ttl))
	raw.Write(ttl[:])
	return []byte(tokenPrefix + base64.RawURLEncoding.EncodeToString(raw.Bytes())), nil
}

// UnmarshalText decodes and validates a token produced by MarshalText.
func (t *Token[K]) UnmarshalText(data []byte) error {
	invalid := fault.New(fault.Invalid, "invalid lease token")
	text, ok := strings.CutPrefix(string(data), tokenPrefix)
	if t == nil || !ok || len(text) > 1024 {
		return invalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return invalid
	}
	parts := make([]string, 3)
	for i := range parts {
		if len(raw) < 1 || len(raw) < 1+int(raw[0]) {
			return invalid
		}
		parts[i], raw = string(raw[1:1+int(raw[0])]), raw[1+int(raw[0]):]
	}
	if len(raw) != sha256.Size+OwnerBytes+8 {
		return invalid
	}
	var hash [sha256.Size]byte
	copy(hash[:], raw[:sha256.Size])
	address, err := keyaddress.FromHash(keyspace.Namespace{Application: parts[0], Environment: parts[1]}, parts[2], hash)
	if err != nil {
		return invalid
	}
	owner, err := ParseOwner(raw[sha256.Size : sha256.Size+OwnerBytes])
	if err != nil {
		return invalid
	}
	restored := Token[K]{key: Key{address: address}, owner: owner, ttl: time.Duration(binary.BigEndian.Uint64(raw[sha256.Size+OwnerBytes:]))}
	if restored.validate() != nil {
		return invalid
	}
	*t = restored
	return nil
}
func (Token[K]) String() string               { return "[lease token]" }
func (t Token[K]) GoString() string           { return t.String() }
func (t Token[K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte(t.String())) }
