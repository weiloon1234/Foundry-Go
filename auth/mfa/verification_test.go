package mfa

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	lockoutmemory "github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type cyclicFactorError struct{ visits atomic.Int32 }

func (*cyclicFactorError) Error() string { panic("private factor error must not be formatted") }
func (e *cyclicFactorError) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

type failingFactorLockout struct {
	lockout.Backend
	site    string
	failure error
}

func (b *failingFactorLockout) LockoutBegin(ctx context.Context, key lockout.Key, policy lockout.Policy, generation lockout.Generation) (lockout.Admission, error) {
	if b.site == "begin" && b.failure != nil {
		return lockout.Admission{}, b.failure
	}
	return b.Backend.LockoutBegin(ctx, key, policy, generation)
}
func (b *failingFactorLockout) LockoutFinish(ctx context.Context, key lockout.Key, policy lockout.Policy, snapshot lockout.Snapshot, outcome lockout.Outcome) (lockout.Decision, error) {
	if b.site == "finish" && b.failure != nil {
		return lockout.Decision{}, b.failure
	}
	return b.Backend.LockoutFinish(ctx, key, policy, snapshot, outcome)
}

func TestCyclicLockoutFailureReleasesMFAMutationWithoutConsumingRecovery(t *testing.T) {
	for _, site := range []string{"begin", "finish"} {
		t.Run(site, func(t *testing.T) {
			source := testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
			now, err := temporal.NewDateTime(source.Now())
			if err != nil {
				t.Fatal(err)
			}
			namespace := keyspace.Namespace{Application: "mfa-error-graph", Environment: "test"}
			memory, err := lockoutmemory.New(16, source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { memory.Close() })
			cycle := new(cyclicFactorError)
			backend := &failingFactorLockout{Backend: memory, site: site, failure: cycle}
			limits := lockout.DefaultConfig(namespace)
			limits.MaxConcurrent = 1
			attempts, err := lockout.NewStore(backend, limits)
			if err != nil {
				t.Fatal(err)
			}
			throttle, err := lockout.Define("mfa.subjects", keyspace.SignedKeys[int64](), lockout.DefaultPolicy()).Bind(attempts)
			if err != nil {
				t.Fatal(err)
			}
			key, err := encryption.GenerateKey("test")
			if err != nil {
				t.Fatal(err)
			}
			keys, err := encryption.NewKeyring(key.ID(), key)
			if err != nil {
				t.Fatal(err)
			}
			config := DefaultConfig(namespace, "Foundry")
			config.MaxConcurrent = 1
			protocol := &protocolBackend{now: now}
			store, err := NewStore(protocol, keys, config)
			if err != nil {
				t.Fatal(err)
			}
			subject := factorSubject{ID: 7}
			provider := auth.DefineProvider("factor.subjects", subject.reference(), func(context.Context, int64) (value.Optional[factorSubject], error) {
				panic("unexpected provider lookup")
			}, func(context.Context, factorSubject) (bool, error) { return true, nil })
			identity, err := subject.FoundryIdentity()
			if err != nil {
				t.Fatal(err)
			}
			codes, hashes, err := newRecoveryCodes(1)
			if err != nil {
				t.Fatal(err)
			}
			original := slices.Clone(hashes)
			record := Record{Subject: identity, RecoveryHashes: hashes}
			response, err := RecoveryResponse(codes[0])
			if err != nil {
				t.Fatal(err)
			}
			rejected := 0
			// Exercise verification inside its real mutation owner. The protocol
			// backend avoids database I/O and accepts an empty removal only after
			// verification; persistent rollback is covered by PostgreSQL tests.
			factors := &Factors[factorSubject, int64]{
				store: store, provider: provider, attempts: throttle,
				address:  Address{Namespace: namespace, Provider: provider.Name(), Model: provider.ModelName()},
				observer: &Observer[factorSubject, int64]{Rejected: func(context.Context, Notice[factorSubject, int64]) error { rejected++; return nil }},
			}
			var selected Record
			mutate := func() error {
				return factors.mutate(t.Context(), identity, protocol.Within, func(context.Context, *database.Tx) (factorSubject, error) { return subject, nil }, func(ctx context.Context, _ *database.Tx, _ factorSubject, _ value.Optional[Record], now temporal.DateTime) (Change, error) {
					next, err := factors.verify(ctx, record, response, now)
					if err != nil {
						return Change{}, err
					}
					selected = next
					return Remove(), nil
				})
			}
			if err := mutate(); err == nil || cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
				t.Fatal("cyclic lockout inspection did not finish safely")
			}
			if rejected != 0 || selected.Subject == identity || len(selected.RecoveryHashes) != 0 || !slices.Equal(record.RecoveryHashes, original) {
				t.Fatal("unknown lockout failure published verification or changed recovery state")
			}
			backend.failure = nil
			if err := mutate(); err != nil {
				t.Fatal("failed mutation retained capacity", err)
			}
			if selected.Subject != identity || len(selected.RecoveryHashes) != 0 || rejected != 0 || !slices.Equal(record.RecoveryHashes, original) {
				t.Fatal("healthy verification did not consume only its owned recovery snapshot")
			}
		})
	}
}
