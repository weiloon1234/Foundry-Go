package auth

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// PermissionName identifies a current model capability, distinct from a token's
// persisted AccessScopeName. Permissions and resource policies share one registry
// namespace and execution pipeline; duplicate names cannot replace declarations.
type PermissionName string

// Permission is a model-owned policy without a resource argument. Its callback
// decides from the authenticated model and current domain state, not serialized
// credential claims. It reuses Policy's registration, bounds and failure handling.
type Permission[M any] struct {
	policy Policy[M, struct{}]
	label  i18n.MessageKey
	err    error
}

// DefinePermission declares a named model permission evaluated through the policy
// machinery.
func DefinePermission[M any](name PermissionName, evaluate func(context.Context, M) (bool, error)) Permission[M] {
	var check func(context.Context, M, struct{}) (bool, error)
	if evaluate != nil {
		check = func(ctx context.Context, subject M, _ struct{}) (bool, error) { return evaluate(ctx, subject) }
	}
	return Permission[M]{policy: DefinePolicy(PolicyName(name), check)}
}

// Name returns the declared permission name.
func (p Permission[M]) Name() PermissionName { return PermissionName(p.policy.Name()) }

// Validate checks retained declaration errors and the underlying policy.
func (p Permission[M]) Validate() error {
	if p.err != nil {
		return p.err
	}
	return p.policy.Validate()
}

// ValidateIn checks that this exact permission declaration belongs to registry.
func (p Permission[M]) ValidateIn(registry *Registry) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return p.policy.ValidateIn(registry)
}

// Registration returns the underlying policy registration with permission validation.
func (p Permission[M]) Registration() Registration {
	r := p.policy.Registration()
	r.validate = p.Validate
	return r
}

// Allows evaluates the registered permission for guard's current subject and returns
// whether access is allowed.
func (p Permission[M]) Allows(ctx context.Context, guard Guard[M]) (bool, error) {
	if err := p.Validate(); err != nil {
		return false, err
	}
	return p.policy.Allows(ctx, guard, struct{}{})
}

// Inspect returns the decision, including a typed Denial when one was given.
func (p Permission[M]) Inspect(ctx context.Context, guard Guard[M]) (Decision, error) {
	if err := p.Validate(); err != nil {
		return Decision{}, err
	}
	return p.policy.Inspect(ctx, guard, struct{}{})
}

// Authorize evaluates the permission and returns a denial error when access is refused.
func (p Permission[M]) Authorize(ctx context.Context, guard Guard[M]) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return p.policy.Authorize(ctx, guard, struct{}{})
}

// WithLabelKey copies only presentation metadata. The exact registered policy
// identity, model ownership and fresh authorization evaluator remain unchanged.
func (p Permission[M]) WithLabelKey(key i18n.MessageKey) Permission[M] {
	if err := key.Validate(); err != nil {
		p.err = err
		return p
	}
	p.label = key
	return p
}

// LabelKey returns the permission's display-label translation key.
func (p Permission[M]) LabelKey() i18n.MessageKey { return p.label }

// PermissionDescription carries a permission's stable name and optional display-label
// translation key.
type PermissionDescription struct {
	Name     PermissionName  `json:"name"`
	LabelKey i18n.MessageKey `json:"label_key,omitempty"`
}

// Description validates the permission and returns its name and translation key.
func (p Permission[M]) Description() (PermissionDescription, error) {
	if err := p.Validate(); err != nil {
		return PermissionDescription{}, err
	}
	return PermissionDescription{Name: p.Name(), LabelKey: p.label}, nil
}
