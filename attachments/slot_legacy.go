package attachments

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// OneSlot preserves the generated extension API. Example: slot.From(manager).
type OneSlot[M any, K comparable] struct {
	query.ExtensionSlot[M]
	slotDefinition[M, K, One[M]]
}

// DefineOne preserves generated slot declarations. Example: DefineOne(owner,name,policy,binding).
func DefineOne[M any, K comparable](owner extensions.Owner[M, K], name Name, policy Policy, binding extensions.SlotBinding[M, K, One[M]]) OneSlot[M, K] {
	s := defineOne(owner, name, policy, binding)
	return OneSlot[M, K]{query.FoundryExtensionSlot(s.loader, sqlowner.Queries()), s.slotDefinition}
}

// From borrows the manager. Example: slot.From(manager).
func (s OneSlot[M, K]) From(m *Manager) OneSlot[M, K] {
	state := (oneSlot[M, K]{slotDefinition: s.slotDefinition}).From(m)
	return OneSlot[M, K]{query.FoundryExtensionSlot(state.loader, sqlowner.Queries()), state.slotDefinition}
}

// Format redacts slot state. Example: fmt.Sprint(slot).
func (OneSlot[M, K]) Format(s fmt.State, verb rune) { (oneSlot[M, K]{}).Format(s, verb) }

// PublicURL delegates to the shared slot state. Example: slot.PublicURL(ctx,owner).
func (s OneSlot[M, K]) PublicURL(ctx context.Context, owner M) (value.Optional[string], error) {
	return (oneSlot[M, K]{slotDefinition: s.slotDefinition}).PublicURL(ctx, owner)
}

// ManySlot preserves the generated extension API. Example: slot.From(manager).
type ManySlot[M any, K comparable] struct {
	query.ExtensionSlot[M]
	slotDefinition[M, K, Many[M]]
}

// DefineMany preserves generated slot declarations. Example: DefineMany(owner,name,policy,binding).
func DefineMany[M any, K comparable](owner extensions.Owner[M, K], name Name, policy Policy, binding extensions.SlotBinding[M, K, Many[M]]) ManySlot[M, K] {
	s := defineMany(owner, name, policy, binding)
	return ManySlot[M, K]{query.FoundryExtensionSlot(s.loader, sqlowner.Queries()), s.slotDefinition}
}

// From borrows the manager. Example: slot.From(manager).
func (s ManySlot[M, K]) From(m *Manager) ManySlot[M, K] {
	state := (manySlot[M, K]{slotDefinition: s.slotDefinition}).From(m)
	return ManySlot[M, K]{query.FoundryExtensionSlot(state.loader, sqlowner.Queries()), state.slotDefinition}
}

// Format redacts slot state. Example: fmt.Sprint(slot).
func (ManySlot[M, K]) Format(s fmt.State, verb rune) { (manySlot[M, K]{}).Format(s, verb) }

// PublicURLs delegates to the shared slot state. Example: slot.PublicURLs(ctx,owner).
func (s ManySlot[M, K]) PublicURLs(ctx context.Context, owner M) ([]string, error) {
	return (manySlot[M, K]{slotDefinition: s.slotDefinition}).PublicURLs(ctx, owner)
}

// AddFile delegates to the shared slot state. Example: slot.AddFile(ctx,owner,file).
func (s ManySlot[M, K]) AddFile(ctx context.Context, owner M, file FileSource) (Result[M, K], error) {
	return (manySlot[M, K]{slotDefinition: s.slotDefinition}).AddFile(ctx, owner, file)
}

// Add delegates to the shared slot state. Example: slot.Add(ctx,owner,upload).
func (s ManySlot[M, K]) Add(ctx context.Context, owner M, upload Upload) (Result[M, K], error) {
	return (manySlot[M, K]{slotDefinition: s.slotDefinition}).Add(ctx, owner, upload)
}

// AddIn delegates to the shared slot state. Example: slot.AddIn(ctx,tx,owner,prepared).
func (s ManySlot[M, K]) AddIn(ctx context.Context, tx *database.Tx, owner M, prepared Prepared[M, K]) (Result[M, K], error) {
	return (manySlot[M, K]{slotDefinition: s.slotDefinition}).AddIn(ctx, tx, owner, prepared)
}

// Reorder delegates to the shared slot state. Example: slot.Reorder(ctx,owner,order).
func (s ManySlot[M, K]) Reorder(ctx context.Context, owner M, order []ID[M]) ([]Attachment[M, K], error) {
	return (manySlot[M, K]{slotDefinition: s.slotDefinition}).Reorder(ctx, owner, order)
}
