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
	"github.com/weiloon1234/Foundry-Go/value"
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
	Device        auth.Device
	// ConfirmedAt is when the holder last re-entered its password (see
	// Sessions.ConfirmCurrent). It never outlives the session.
	ConfirmedAt value.Optional[temporal.DateTime] `json:",omitzero"`
	// Impersonator marks an impersonation session with its original actor.
	Impersonator value.Optional[Impersonator] `json:",omitzero"`
}

// Impersonator is the original actor of an impersonation session: its stored
// identity, the guard that authenticated it and that actor's own session, which
// Impersonation.Resume and Stop use to return. It is metadata, never authority.
type Impersonator struct {
	Subject model.Identity
	Guard   auth.GuardName
	Session model.ID[Record]
}

func (i Impersonator) Validate() error {
	if err := i.Subject.Validate(); err != nil {
		return err
	}
	if i.Guard == "" || i.Session.IsZero() {
		return fault.New(fault.Invalid, "invalid session impersonator")
	}
	return nil
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
	if confirmed, present := r.ConfirmedAt.Get(); present && (confirmed.IsZero() || confirmed.UTC().Before(created) || r.Assurance != auth.Authenticated) {
		return fault.New(fault.Invalid, "invalid stored session confirmation")
	}
	if impersonator, present := r.Impersonator.Get(); present {
		if err := impersonator.Validate(); err != nil {
			return err
		}
		if r.Assurance != auth.Authenticated || r.Remember || r.ConfirmedAt.IsSet() {
			return fault.New(fault.Invalid, "invalid stored impersonation session")
		}
	}
	return r.Device.Validate()
}
func (r Record) Live(now time.Time) bool {
	return now.Before(r.IdleExpiresAt.UTC()) && now.Before(r.ExpiresAt.UTC())
}

// TouchInterval is the minimum age of LastSeenAt before a sliding session's
// activity is written again: max(1 minute, Idle/20), but never more than half
// the idle lifetime, so a short idle window still slides for an active user.
// Skipping more frequent writes avoids a row update per request; idle expiry
// may therefore arrive up to this interval earlier than Idle after the last
// request.
func (l Lifetime) TouchInterval() time.Duration {
	return min(max(time.Minute, l.Idle/20), l.Idle/2).Truncate(time.Microsecond)
}

// NeedsTouch reports whether a live sliding session should record activity now.
func (r Record) NeedsTouch(now time.Time) bool {
	return r.Lifetime.Sliding && r.Live(now) && now.UTC().Sub(r.LastSeenAt.UTC()) >= r.Lifetime.TouchInterval()
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
// Maximum is the live full-session bound and PendingMaximum the live pending-MFA
// bound, both enforced under the subject's write lock for the credential's own
// kind. Limit selects eviction or rejection for full sessions; pending sessions
// always evict their oldest. Expired rows never count.
type Creation struct {
	ID             model.ID[Record]
	Subject        model.Identity
	Hash           Digest
	Assurance      auth.Assurance
	Remember       bool
	Lifetime       Lifetime
	Maximum        int
	PendingMaximum int
	Limit          auth.LimitPolicy
	Device         auth.Device
	// Impersonator marks an impersonation session. Impersonation sessions count
	// only against PendingMaximum, like pending MFA, so they never evict or are
	// rejected by the subject's own sessions.
	Impersonator value.Optional[Impersonator]
}

func (c Creation) Validate(address Address) error {
	if _, err := address.SubjectKey(c.Subject); err != nil {
		return err
	}
	if c.ID.IsZero() || c.Hash.IsZero() || c.Maximum < 1 || c.PendingMaximum < 1 || c.Maximum+2*c.PendingMaximum > MaxSessions {
		return fault.New(fault.Invalid, "invalid session creation")
	}
	if err := c.Limit.Validate(); err != nil {
		return err
	}
	if err := c.Device.Validate(); err != nil {
		return err
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
	if impersonator, present := c.Impersonator.Get(); present {
		if err := impersonator.Validate(); err != nil {
			return err
		}
		if c.Assurance != auth.Authenticated || c.Remember {
			return fault.New(fault.Invalid, "impersonation sessions must be fully authenticated and not remembered")
		}
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
	r := Record{ID: c.ID, Address: address, Subject: c.Subject, Hash: c.Hash, Assurance: c.Assurance, Remember: c.Remember, Lifetime: c.Lifetime, CreatedAt: created, LastSeenAt: created, IdleExpiresAt: idle, ExpiresAt: expires, Device: c.Device, Impersonator: c.Impersonator}
	return r, r.Validate(address)
}
