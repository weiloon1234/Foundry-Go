package auth

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ProviderName identifies one authoritative model resolver.
type ProviderName string

// Provider preserves the model and its generated stored primary-key type.
// Reuse the same declaration when session and token guards share a provider.
type Provider[M model.Identifiable, K any] struct{ definition *providerDefinition[M, K] }
type providerDefinition[M model.Identifiable, K any] struct {
	id        *declarationID
	name      ProviderName
	reference model.Reference[M, K]
	lookup    func(context.Context, K) (value.Optional[M], error)
	eligible  func(context.Context, M) (bool, error)
}

// DefineProvider uses generated reference metadata, avoiding repeated table/key
// codecs. Lookup returns an omitted Optional for missing/deleted models. Eligible
// checks current account state (for example disabled status); it is mandatory.
// Callbacks must honor cancellation, be concurrency-safe, and return owned
// hydrated values. M must be a model struct. Treat reference-valued fields in
// returned models as read-only for the scope, or clone them before modification.
func DefineProvider[M model.Identifiable, K any](name ProviderName, reference model.Reference[M, K], lookup func(context.Context, K) (value.Optional[M], error), eligible func(context.Context, M) (bool, error)) Provider[M, K] {
	return Provider[M, K]{definition: &providerDefinition[M, K]{id: &declarationID{}, name: name, reference: reference, lookup: lookup, eligible: eligible}}
}
func (p Provider[M, K]) Validate() error {
	if p.definition == nil || !identifier.Semantic(string(p.definition.name)) || p.definition.lookup == nil || p.definition.eligible == nil || reflect.TypeFor[M]().Kind() != reflect.Struct {
		return fault.New(fault.Invalid, "authentication provider requires a name, concrete model struct, lookup and eligibility check")
	}
	return p.definition.reference.Validate()
}
func (p Provider[M, K]) Name() ProviderName {
	if p.definition == nil {
		return ""
	}
	return p.definition.name
}
func (p Provider[M, K]) ModelName() string {
	if p.definition == nil {
		return ""
	}
	return p.definition.reference.ModelName()
}

// Parse restores a verified credential's stored model reference through the
// generated codec. Parsing alone grants no authority and performs no lookup.
func (p Provider[M, K]) Parse(identity model.Identity) (model.Reference[M, K], error) {
	if err := p.Validate(); err != nil {
		return model.Reference[M, K]{}, err
	}
	return p.definition.reference.Parse(identity)
}
func (p Provider[M, K]) resolve(ctx context.Context, identity model.Identity) (M, error) {
	reference, err := p.Parse(identity)
	if err != nil {
		return *new(M), Unauthenticated.WithCause(err)
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	found, err := p.definition.lookup(ctx, reference.Key())
	if err != nil {
		return *new(M), err
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	subject, present := found.Get()
	if !present {
		return *new(M), Unauthenticated
	}
	actual, err := subject.FoundryIdentity()
	if err != nil {
		return *new(M), err
	}
	if actual != identity {
		return *new(M), fault.New(fault.Invalid, "authentication provider returned a different model identity")
	}
	if err := p.checkEligibility(ctx, subject); err != nil {
		return *new(M), err
	}
	return subject, nil
}

// checkEligibility is shared by credential resolution and model-first login.
// Its caller owns callback isolation and has already validated the identity.
func (p Provider[M, K]) checkEligibility(ctx context.Context, subject M) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	allowed, err := p.definition.eligible(ctx, subject)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !allowed {
		return Unauthenticated
	}
	return nil
}
