package mfa

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Action identifies an MFA security observation, never a credential or authority.
type Action string

const (
	Enrolled            Action = "enrolled"
	Disabled            Action = "disabled"
	Verified            Action = "verified"
	RecoveryRegenerated Action = "recovery_regenerated"
	Reencrypted         Action = "reencrypted"
	Retired             Action = "retired"
	Rejected            Action = "rejected"
)

// Notice exposes only the affected typed reference and action. It contains no
// model snapshot, password, factor secret, submitted code or credential. The
// initiator remains attribution.FromContext(ctx); affected subject is not actor.
type Notice[M any, K any] struct {
	subject model.Reference[M, K]
	action  Action
}

func (n Notice[M, K]) Subject() model.Reference[M, K] { return n.subject }
func (n Notice[M, K]) Action() Action                 { return n.action }
func (Notice[M, K]) Format(s fmt.State, _ rune)       { _, _ = s.Write([]byte("MFA notice")) }

// Observer maps MFA observations to application-owned event DTOs. Changed runs
// inside the mutation transaction: use events.Enqueue/AfterCommit or audit with
// this tx, never external I/O. Its failure rolls back factor/model/credentials.
// Rejected is an immediate, process-local observation of a nonmatching factor
// or confirmed lockout. The enclosing mutation rolls back; do not persist a
// rejection through that transaction. No callback runs for malformed input,
// absent/inactive factors, missing models or infrastructure failures. Rejected
// errors remain causes of the denial and cannot turn it into success.
type Observer[M any, K any] struct {
	Changed  func(context.Context, *database.Tx, Notice[M, K]) error
	Rejected func(context.Context, Notice[M, K]) error
}

// WithObserver returns an immutable view. Register at most once; callbacks must
// respect cancellation and shared-state concurrency. Notices are not delivery
// receipts: outbox durability begins only when the enclosing transaction commits.
func (f *Factors[M, K]) WithObserver(observer Observer[M, K]) (*Factors[M, K], error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if observer.Changed == nil && observer.Rejected == nil {
		return nil, fault.New(fault.Invalid, "MFA observer is empty")
	}
	if f.observer != nil {
		return nil, fault.New(fault.Duplicate, "MFA observer already configured")
	}
	next := *f
	next.observer = &observer
	return &next, nil
}
func (f *Factors[M, K]) changed(ctx context.Context, tx *database.Tx, identity model.Identity, action Action) error {
	if f.observer == nil || f.observer.Changed == nil {
		return nil
	}
	reference, err := f.provider.Parse(identity)
	if err != nil {
		return err
	}
	return callback.Isolated("MFA change observer", func() error { return f.observer.Changed(ctx, tx, Notice[M, K]{subject: reference, action: action}) })
}
func (f *Factors[M, K]) rejected(ctx context.Context, reference model.Reference[M, K]) error {
	if f.observer == nil || f.observer.Rejected == nil {
		return nil
	}
	return callback.Isolated("MFA rejection observer", func() error { return f.observer.Rejected(ctx, Notice[M, K]{subject: reference, action: Rejected}) })
}
