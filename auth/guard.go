package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type GuardName string

// Guard retains the concrete authenticated model while its strategy/provider
// preserve the key type at construction. A model may have multiple guards.
type Guard[M any] struct{ definition *guardDefinition[M] }
type guardResult[M any] struct {
	identity   model.Identity
	subject    value.Optional[M]
	grants     *accessGrant
	credential *attachedCredential
}
type guardDefinition[M any] struct {
	id           *declarationID
	name         GuardName
	providerName ProviderName
	source       CredentialName
	providerID   *declarationID
	validate     func() error
	resolve      func(context.Context, Credentials) (guardResult[M], error)
}

func DefineGuard[M model.Identifiable, K any](name GuardName, provider Provider[M, K], strategy Strategy[M, K]) Guard[M] {
	d := &guardDefinition[M]{id: &declarationID{}, name: name, providerName: provider.Name(), source: strategy.Source()}
	if provider.definition != nil {
		d.providerID = provider.definition.id
	}
	d.validate = func() error {
		if err := provider.Validate(); err != nil {
			return err
		}
		return strategy.Validate()
	}
	d.resolve = func(ctx context.Context, inputs Credentials) (guardResult[M], error) {
		credential, bound := inputs.Get(strategy.source), inputs.bound[strategy.source]
		if credential.IsZero() && bound.IsZero() {
			return guardResult[M]{}, nil
		}
		var result value.Optional[Proof[M, K]]
		var err error
		switch {
		case bound.IsZero():
			result, err = strategy.verify(ctx, credential)
		case strategy.bound != nil:
			result, err = strategy.bound(ctx, bound)
		default:
			// Only a strategy's own binder creates its bound credentials.
			err = Unauthenticated
		}
		if err != nil {
			return guardResult[M]{}, err
		}
		if err := ctx.Err(); err != nil {
			return guardResult[M]{}, err
		}
		proof, present := result.Get()
		if !present {
			return guardResult[M]{}, Unauthenticated
		}
		if !proof.assurance.valid() {
			return guardResult[M]{}, fault.New(fault.Invalid, "strategy returned an invalid authentication proof")
		}
		if proof.assurance == PendingMFA {
			if _, err := provider.Parse(proof.identity); err != nil {
				return guardResult[M]{}, Unauthenticated.WithCause(err)
			}
			return guardResult[M]{}, MFARequired
		}
		subject, err := provider.resolve(ctx, proof.identity)
		if err != nil {
			return guardResult[M]{}, err
		}
		return guardResult[M]{identity: proof.identity, subject: value.Set(subject), grants: proof.grants, credential: proof.credential}, nil
	}
	return Guard[M]{definition: d}
}
func (g Guard[M]) Validate() error {
	if g.definition == nil || !identifier.Semantic(string(g.definition.name)) {
		return fault.New(fault.Invalid, "authentication guard requires a semantic name")
	}
	return g.definition.validate()
}
func (g Guard[M]) Name() GuardName {
	if g.definition == nil {
		return ""
	}
	return g.definition.name
}
func (g Guard[M]) ProviderName() ProviderName {
	if g.definition == nil {
		return ""
	}
	return g.definition.providerName
}
func (g Guard[M]) Registration() Registration {
	if g.definition == nil {
		return Registration{}
	}
	return Registration{kind: guardRegistration, id: g.definition.id, name: string(g.Name()), providerID: g.definition.providerID, providerName: g.ProviderName(), validate: g.Validate}
}

// Optional returns an omitted model only when this guard's credential is absent.
// Invalid, revoked, missing-model and disabled-account credentials remain errors.
// Verification and model lookup coalesce once per guard declaration per scope;
// failures are cached too. A canceled initiating call cannot cause an implicit
// retry. Other waiters may cancel without canceling the initiating operation.
func (g Guard[M]) Optional(ctx context.Context) (value.Optional[M], error) {
	result, err := g.resolve(ctx)
	if err != nil {
		return value.Optional[M]{}, err
	}
	return result.subject, nil
}

func (g Guard[M]) resolve(ctx context.Context) (guardResult[M], error) {
	if err := g.Validate(); err != nil {
		return guardResult[M]{}, err
	}
	scope, err := currentScope(ctx)
	if err != nil {
		return guardResult[M]{}, err
	}
	result, err := scope.resolve(ctx, g.definition.id, func(op context.Context, inputs Credentials) (any, error) { return g.definition.resolve(op, inputs) })
	if err != nil {
		return guardResult[M]{}, err
	}
	typed, ok := result.(guardResult[M])
	if !ok {
		return guardResult[M]{}, fault.New(fault.Internal, "invalid authentication scope result")
	}
	return typed, nil
}

// Require returns the concrete model after current credential and provider
// checks. It never returns a partial model on error and never accepts MFA-pending
// credentials. Reference-valued model fields remain read-only shared scope data.
func (g Guard[M]) Require(ctx context.Context) (M, error) {
	found, err := g.Optional(ctx)
	if err != nil {
		return *new(M), err
	}
	subject, present := found.Get()
	if !present {
		return *new(M), Unauthenticated
	}
	return subject, nil
}

// ValidateIn checks assembly registration without verifying credentials or loading
// a model. Matching names alone never substitute for the registered declaration.
func (g Guard[M]) ValidateIn(registry *Registry) error {
	if err := g.Validate(); err != nil {
		return err
	}
	if err := registry.Validate(); err != nil {
		return err
	}
	if !registry.guards[g.definition.id] {
		return fault.New(fault.Missing, "authentication guard is not registered")
	}
	return nil
}

// Source is the named credential input verified by this guard's strategy.
func (g Guard[M]) Source() CredentialName {
	if g.definition == nil {
		return ""
	}
	return g.definition.source
}
