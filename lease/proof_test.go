package lease_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func TestProofLifetimeAndOwnedScope(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, l := manager(t, nil, nil)
		if err := (lease.Proof{}).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if (lease.Proof{}).Key().Validate() == nil || (lease.Proof{}).Owner().Validate() == nil {
			t.Fatal("zero proof has authority")
		}
		var retained lease.Proof
		ran, err := l.WithProof(t.Context(), "a", time.Second, 0, func(ctx context.Context, proof lease.Proof) error {
			retained = proof
			if err := proof.Validate(); err != nil {
				return err
			}
			if proof.Key().Namespace() != m.Namespace() || proof.Owner().Validate() != nil {
				t.Fatal("proof lost scope")
			}
			return nil
		})
		if !ran || err != nil {
			t.Fatal(ran, err)
		}
		if err := retained.Validate(); !errors.Is(err, lease.ErrReleased) {
			t.Fatal("proof survived scope", err)
		}
		g := acquire(t, l, "b")
		proof, err := g.Proof()
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if err := proof.Validate(); !errors.Is(err, lease.ErrLost) {
			t.Fatal(err)
		}
		<-g.Done()
		if ran, err := l.WithProof(t.Context(), "a", time.Second, 0, nil); ran || !errors.Is(err, fault.Invalid) {
			t.Fatal(ran, err)
		}
		if m.Stats().Active != 0 {
			t.Fatal(m.Stats())
		}
	})
}
