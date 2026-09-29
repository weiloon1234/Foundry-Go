package token

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func validateName(name string) error {
	if len(name) > 128 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return fault.New(fault.Invalid, "invalid token display name")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fault.New(fault.Invalid, "invalid token display name")
		}
	}
	return nil
}
func restoreScopes[M any](names []auth.AccessScopeName) (auth.AccessScopes[M], error) {
	if len(names) > auth.MaxAccessScopes {
		return auth.AccessScopes[M]{}, fault.New(fault.Invalid, "stored token scope capacity exceeded")
	}
	declarations := make([]auth.AccessScope[M], len(names))
	for i, name := range names {
		declarations[i] = auth.DefineAccessScope[M](name)
	}
	scopes, err := auth.NewAccessScopes(declarations...)
	if err != nil {
		return auth.AccessScopes[M]{}, err
	}
	if !slices.Equal(scopes.Names(), names) {
		return auth.AccessScopes[M]{}, fault.New(fault.Invalid, "stored token scopes are not canonical")
	}
	return scopes, nil
}

// Record is the explicit adapter boundary for the current token generation.
// It contains no raw secret. Adapters transfer ownership of the bounded scope
// slice; typed application code receives immutable Info instead of this record.
type Record struct {
	ID               model.ID[Record]
	Address          Address
	Subject          model.Identity
	Name             string
	Scopes           []auth.AccessScopeName
	Mode             Mode
	Assurance        auth.Assurance
	AccessHash       Digest
	RefreshHash      value.Optional[Digest]
	Lifetime         Lifetime
	RotationLimit    uint32
	Generation       uint32
	CreatedAt        temporal.DateTime
	IssuedAt         temporal.DateTime
	LastSeenAt       temporal.DateTime
	AccessExpiresAt  temporal.DateTime
	RefreshExpiresAt value.Optional[temporal.DateTime]
	ExpiresAt        temporal.DateTime
	Device           auth.Device
	// SupersededAt is set only when Lookup returns the generation immediately
	// before the family's current one: the successor's issue time. Its access
	// secret authenticates only within Config.AccessGrace of that time.
	SupersededAt value.Optional[temporal.DateTime] `json:",omitzero"`
}

func (Record) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("token record")) }

// LiveAccessWithin reports access validity, allowing a superseded generation
// only until grace after its successor was issued (never past its own expiry).
func (r Record) LiveAccessWithin(now time.Time, grace time.Duration) bool {
	if !r.LiveAccess(now) {
		return false
	}
	superseded, present := r.SupersededAt.Get()
	return !present || now.Before(superseded.UTC().Add(grace))
}
func (r Record) validateMetadata(address Address) error {
	if err := address.Validate(); err != nil {
		return err
	}
	if r.Address != address || r.ID.IsZero() || r.AccessHash.IsZero() {
		return fault.New(fault.Invalid, "invalid token record scope or identity")
	}
	if _, err := address.SubjectKey(r.Subject); err != nil {
		return err
	}
	if err := validateName(r.Name); err != nil {
		return err
	}
	if _, err := restoreScopes[Record](r.Scopes); err != nil {
		return err
	}
	if err := r.Lifetime.Validate(r.Mode); err != nil {
		return err
	}
	if err := r.Assurance.Validate(); err != nil {
		return err
	}
	refreshHash, hasRefresh := r.RefreshHash.Get()
	if r.Mode == Renewable {
		if r.Assurance != auth.Authenticated || !hasRefresh || refreshHash.IsZero() || refreshHash.Equal(r.AccessHash) || r.RotationLimit < 1 || r.RotationLimit > MaxRotations || r.Generation > r.RotationLimit {
			return fault.New(fault.Invalid, "invalid renewable token metadata")
		}
	} else {
		if hasRefresh || r.Generation != 0 || r.RotationLimit != 0 {
			return fault.New(fault.Invalid, "nonrenewable token has refresh metadata")
		}
		if r.Mode == Challenge {
			if r.Assurance != auth.PendingMFA || len(r.Scopes) != 0 {
				return fault.New(fault.Invalid, "MFA challenge token cannot carry ordinary authority")
			}
		} else if r.Assurance != auth.Authenticated {
			return fault.New(fault.Invalid, "personal token requires full assurance")
		}
	}
	return nil
}
func (r Record) Validate(address Address) error {
	if err := r.validateMetadata(address); err != nil {
		return err
	}
	created, issued, seen, access, expires := r.CreatedAt.UTC(), r.IssuedAt.UTC(), r.LastSeenAt.UTC(), r.AccessExpiresAt.UTC(), r.ExpiresAt.UTC()
	if r.CreatedAt.IsZero() || r.IssuedAt.IsZero() || r.LastSeenAt.IsZero() || r.AccessExpiresAt.IsZero() || r.ExpiresAt.IsZero() || issued.Before(created) || seen.Before(issued) || !access.After(seen) || access.After(expires) || !expires.Equal(created.Add(r.Lifetime.Absolute)) || !access.Equal(boundedExpiry(issued, r.Lifetime.Access, expires)) {
		return fault.New(fault.Invalid, "invalid stored token lifetime")
	}
	refreshExpiry, hasExpiry := r.RefreshExpiresAt.Get()
	if r.Mode == Renewable {
		if !hasExpiry || refreshExpiry.IsZero() || !refreshExpiry.UTC().Equal(boundedExpiry(issued, r.Lifetime.RefreshIdle, expires)) {
			return fault.New(fault.Invalid, "invalid stored refresh expiry")
		}
	} else if hasExpiry || !issued.Equal(created) {
		return fault.New(fault.Invalid, "nonrenewable token has refresh times")
	}
	if superseded, present := r.SupersededAt.Get(); present && (r.Mode != Renewable || superseded.UTC().Before(issued)) {
		return fault.New(fault.Invalid, "invalid superseded token generation")
	}
	return r.Device.Validate()
}
func boundedExpiry(now time.Time, duration time.Duration, absolute time.Time) time.Time {
	expiry := now.Add(duration)
	if expiry.After(absolute) {
		return absolute
	}
	return expiry
}
func (r Record) LiveAccess(now time.Time) bool {
	return now.Before(r.AccessExpiresAt.UTC()) && now.Before(r.ExpiresAt.UTC())
}
func (r Record) LiveRefresh(now time.Time) bool {
	expiry, present := r.RefreshExpiresAt.Get()
	return r.Mode == Renewable && present && now.Before(expiry.UTC()) && now.Before(r.ExpiresAt.UTC())
}
func (r Record) Live(now time.Time) bool { return r.LiveAccess(now) || r.LiveRefresh(now) }

// Refreshed computes the successor of a current live generation. The adapter must
// first lock and re-read the family, detect consumed-token reuse, and ensure this
// record is current. False means no successor; it must never revive a dead family.
func (r Record) Refreshed(now time.Time, access, refresh Digest) (Record, bool, error) {
	if err := r.Validate(r.Address); err != nil {
		return Record{}, false, err
	}
	previous, _ := r.RefreshHash.Get()
	if access.IsZero() || refresh.IsZero() || access.Equal(refresh) || access.Equal(r.AccessHash) || access.Equal(previous) || refresh.Equal(previous) || refresh.Equal(r.AccessHash) {
		return Record{}, false, fault.New(fault.Invalid, "refresh requires distinct new credential hashes")
	}
	now = now.UTC().Truncate(time.Microsecond)
	if !r.LiveRefresh(now) || r.Generation >= r.RotationLimit {
		return Record{}, false, nil
	}
	if now.Before(r.LastSeenAt.UTC()) {
		now = r.LastSeenAt.UTC()
	}
	next := r
	next.Scopes = slices.Clone(r.Scopes)
	next.SupersededAt = value.Optional[temporal.DateTime]{}
	next.Generation++
	next.AccessHash = access
	next.RefreshHash = value.Set(refresh)
	issued, err := temporal.NewDateTime(now)
	if err != nil {
		return Record{}, false, err
	}
	next.IssuedAt = issued
	next.LastSeenAt = issued
	accessExpiry, err := temporal.NewDateTime(boundedExpiry(now, r.Lifetime.Access, r.ExpiresAt.UTC()))
	if err != nil {
		return Record{}, false, err
	}
	refreshExpiry, err := temporal.NewDateTime(boundedExpiry(now, r.Lifetime.RefreshIdle, r.ExpiresAt.UTC()))
	if err != nil {
		return Record{}, false, err
	}
	next.AccessExpiresAt = accessExpiry
	next.RefreshExpiresAt = value.Set(refreshExpiry)
	return next, true, next.Validate(r.Address)
}

// Creation is a trusted, bounded issuance request. Backend.Create enforces
// Maximum live personal/renewable families and PendingMaximum live challenge
// families, each counting only its own kind, while holding the stable subject
// lock. Limit selects rejection or eviction for full families; challenges
// always evict their oldest. Expired families never count.
type Creation struct {
	ID             model.ID[Record]
	Subject        model.Identity
	Name           string
	Scopes         []auth.AccessScopeName
	Mode           Mode
	Assurance      auth.Assurance
	AccessHash     Digest
	RefreshHash    value.Optional[Digest]
	Lifetime       Lifetime
	RotationLimit  uint32
	Maximum        int
	PendingMaximum int
	Limit          auth.LimitPolicy
	Device         auth.Device
}

func (c Creation) metadata(address Address) Record {
	return Record{ID: c.ID, Address: address, Subject: c.Subject, Name: c.Name, Scopes: c.Scopes, Mode: c.Mode, Assurance: c.Assurance, AccessHash: c.AccessHash, RefreshHash: c.RefreshHash, Lifetime: c.Lifetime, RotationLimit: c.RotationLimit, Device: c.Device}
}
func (c Creation) Validate(address Address) error {
	if c.Maximum < 1 || c.PendingMaximum < 1 || c.Maximum+c.PendingMaximum > MaxTokens {
		return fault.New(fault.Invalid, "invalid token subject capacity")
	}
	if err := c.Limit.Validate(); err != nil {
		return err
	}
	if err := c.Device.Validate(); err != nil {
		return err
	}
	return c.metadata(address).validateMetadata(address)
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
	expires, err := created.Add(c.Lifetime.Absolute)
	if err != nil {
		return Record{}, err
	}
	access, err := created.Add(c.Lifetime.Access)
	if err != nil {
		return Record{}, err
	}
	r := c.metadata(address)
	r.Scopes = slices.Clone(c.Scopes)
	r.CreatedAt = created
	r.IssuedAt = created
	r.LastSeenAt = created
	r.AccessExpiresAt = access
	r.ExpiresAt = expires
	if c.Mode == Renewable {
		expiry, err := created.Add(c.Lifetime.RefreshIdle)
		if err != nil {
			return Record{}, err
		}
		r.RefreshExpiresAt = value.Set(expiry)
	}
	return r, r.Validate(address)
}
