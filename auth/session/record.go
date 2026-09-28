package session

import (
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Address binds persisted credentials to one application/environment, guard,
// provider and stored model namespace. It is the explicit adapter boundary.
type Address struct {
	Namespace keyspace.Namespace
	Guard     auth.GuardName
	Provider  auth.ProviderName
	Model     string
}

func (a Address) Validate() error {
	return credential.ValidateAddress(a.Namespace, string(a.Guard), string(a.Provider), a.Model)
}
func (a Address) Key() (string, error) {
	return credential.AddressKey(a.Namespace, string(a.Guard), string(a.Provider), a.Model)
}
func (a Address) SubjectKey(identity model.Identity) (string, error) {
	scope, err := a.Key()
	if err != nil {
		return "", err
	}
	return credential.SubjectKey(scope, a.Model, identity)
}

// Record is a bounded, immutable-value adapter result. Adapters never return a
// raw secret. Public typed consumers receive Info or Issued, not these records.
type Record struct {
	ID            model.ID[Record]
	Address       Address
	Subject       model.Identity
	Hash          Digest
	Assurance     auth.Assurance
	Remember      bool
	Lifetime      Lifetime
	CreatedAt     temporal.DateTime
	LastSeenAt    temporal.DateTime
	IdleExpiresAt temporal.DateTime
	ExpiresAt     temporal.DateTime
}

func (Record) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("session record")) }
func (r Record) Validate(address Address) error {
	if err := address.Validate(); err != nil {
		return err
	}
	if r.Address != address || r.ID.IsZero() || r.Hash.IsZero() {
		return fault.New(fault.Invalid, "invalid session record scope or identifier")
	}
	if _, err := address.SubjectKey(r.Subject); err != nil {
		return err
	}
	if err := r.Assurance.Validate(); err != nil {
		return err
	}
	if err := r.Lifetime.Validate(); err != nil {
		return err
	}
	if r.Assurance == auth.PendingMFA && (r.Remember || r.Lifetime.Sliding) {
		return fault.New(fault.Invalid, "pending MFA session cannot persist or slide")
	}
	created, seen, idle, expires := r.CreatedAt.UTC(), r.LastSeenAt.UTC(), r.IdleExpiresAt.UTC(), r.ExpiresAt.UTC()
	if r.CreatedAt.IsZero() || r.LastSeenAt.IsZero() || r.IdleExpiresAt.IsZero() || r.ExpiresAt.IsZero() || seen.Before(created) || !expires.After(created) || !idle.After(seen) || idle.After(expires) || !expires.Equal(created.Add(r.Lifetime.Absolute)) || idle.After(seen.Add(r.Lifetime.Idle)) {
		return fault.New(fault.Invalid, "invalid stored session lifetime")
	}
	return nil
}
func (r Record) Live(now time.Time) bool {
	return now.Before(r.IdleExpiresAt.UTC()) && now.Before(r.ExpiresAt.UTC())
}

// Touch keeps monotonic activity across small server clock regressions. It never
// revives an expired record and never extends beyond its original absolute limit.
func (r Record) Touch(now time.Time) (Record, bool, error) {
	if err := r.Validate(r.Address); err != nil {
		return Record{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !r.Live(now) {
		return Record{}, false, nil
	}
	if !r.Lifetime.Sliding || !now.After(r.LastSeenAt.UTC()) {
		return r, true, nil
	}
	seen, err := temporal.NewDateTime(now)
	if err != nil {
		return Record{}, false, err
	}
	idle := now.Add(r.Lifetime.Idle)
	if idle.After(r.ExpiresAt.UTC()) {
		idle = r.ExpiresAt.UTC()
	}
	expires, err := temporal.NewDateTime(idle)
	if err != nil {
		return Record{}, false, err
	}
	r.LastSeenAt = seen
	r.IdleExpiresAt = expires
	return r, true, nil
}

// Creation contains verified identity metadata and a newly generated hash.
// Maximum is the active-session bound enforced under the subject's write lock.
type Creation struct {
	ID        model.ID[Record]
	Subject   model.Identity
	Hash      Digest
	Assurance auth.Assurance
	Remember  bool
	Lifetime  Lifetime
	Maximum   int
}

func (c Creation) Validate(address Address) error {
	if _, err := address.SubjectKey(c.Subject); err != nil {
		return err
	}
	if c.ID.IsZero() || c.Hash.IsZero() || c.Maximum < 1 || c.Maximum > MaxSessions {
		return fault.New(fault.Invalid, "invalid session creation")
	}
	if err := c.Assurance.Validate(); err != nil {
		return err
	}
	if err := c.Lifetime.Validate(); err != nil {
		return err
	}
	if c.Assurance == auth.PendingMFA && (c.Remember || c.Lifetime.Sliding) {
		return fault.New(fault.Invalid, "pending MFA session cannot persist or slide")
	}
	return nil
}
func (c Creation) At(address Address, now time.Time) (Record, error) {
	if err := c.Validate(address); err != nil {
		return Record{}, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	created, err := temporal.NewDateTime(now)
	if err != nil {
		return Record{}, err
	}
	idle, err := created.Add(c.Lifetime.Idle)
	if err != nil {
		return Record{}, err
	}
	expires, err := created.Add(c.Lifetime.Absolute)
	if err != nil {
		return Record{}, err
	}
	r := Record{ID: c.ID, Address: address, Subject: c.Subject, Hash: c.Hash, Assurance: c.Assurance, Remember: c.Remember, Lifetime: c.Lifetime, CreatedAt: created, LastSeenAt: created, IdleExpiresAt: idle, ExpiresAt: expires}
	return r, r.Validate(address)
}
