package extensions

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Declaration erases model types only at application assembly. Names and model
// namespaces must be unique; constructing a registry performs no database I/O.
type Declaration struct {
	name         OwnerName
	model, scope string
	id           *declarationID
	validate     func() error
	retained     func(context.Context, database.Executor, []model.Identity) (map[string]bool, error)
	subject      func(model.Identity) (Subject, error)
}

func (o Owner[M, K]) Registration() Declaration {
	if o.definition == nil {
		return Declaration{}
	}
	return Declaration{name: o.Name(), model: o.ModelName(), scope: o.Scope(), id: o.definition.id, validate: o.Validate, subject: func(identity model.Identity) (Subject, error) {
		ref, err := o.Parse(identity)
		if err != nil {
			return Subject{}, err
		}
		return o.Subject(ref)
	}, retained: func(ctx context.Context, executor database.Executor, identities []model.Identity) (map[string]bool, error) {
		if len(identities) > query.MaxIdentityBatch {
			return nil, invalid("owner batch exceeds its limit")
		}
		keys := make([]K, len(identities))
		for i, identity := range identities {
			reference, err := o.Parse(identity)
			if err != nil {
				return nil, err
			}
			keys[i] = reference.Key()
		}
		found, err := o.definition.source.RetainedKeys(ctx, executor, keys)
		if err != nil {
			return nil, err
		}
		result := make(map[string]bool, len(found))
		for _, key := range found {
			subject, err := o.Subject(o.Reference(key))
			if err != nil {
				return nil, err
			}
			result[subject.Key] = true
		}
		return result, nil
	}}
}

type Registry struct {
	owners map[OwnerName]Declaration
	names  []OwnerName
}

func NewRegistry(owners ...Declaration) (*Registry, error) {
	if len(owners) > 256 {
		return nil, invalid("too many model extension owners")
	}
	r := &Registry{owners: make(map[OwnerName]Declaration, len(owners))}
	models := make(map[string]bool, len(owners))
	for _, owner := range owners {
		if owner.validate == nil || owner.id == nil || owner.retained == nil || owner.subject == nil {
			return nil, invalid("invalid model extension owner registration")
		}
		if err := owner.validate(); err != nil {
			return nil, err
		}
		if _, exists := r.owners[owner.name]; exists || models[owner.model] {
			return nil, fault.New(fault.Duplicate, "model extension owner already registered")
		}
		r.owners[owner.name] = owner
		r.names = append(r.names, owner.name)
		models[owner.model] = true
	}
	slices.Sort(r.names)
	return r, nil
}
func (r *Registry) Validate() error {
	if r == nil || r.owners == nil {
		return invalid("invalid model extension owner registry")
	}
	return nil
}
func (r *Registry) Owners() []OwnerName {
	if r == nil {
		return nil
	}
	return slices.Clone(r.names)
}

// Subject restores a registered owner's identity at an explicit heterogeneous
// maintenance boundary. Ordinary model APIs retain Owner[M,K].
func (r *Registry) Subject(owner OwnerName, identity model.Identity) (Subject, error) {
	if err := r.Validate(); err != nil {
		return Subject{}, err
	}
	entry, ok := r.owners[owner]
	if !ok {
		return Subject{}, invalid("unknown model extension owner")
	}
	return entry.subject(identity)
}
func (r *Registry) Scope(owner OwnerName) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	entry, ok := r.owners[owner]
	if !ok {
		return "", invalid("unknown model extension owner")
	}
	return entry.scope, nil
}
func (o Owner[M, K]) Check(r *Registry) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return err
	}
	owner, ok := r.owners[o.Name()]
	if !ok || owner.id != o.definition.id {
		return invalid("model extension owner declaration is not registered")
	}
	return nil
}

// RetainedSubjects is the explicit maintenance boundary. It includes soft-
// deleted owners, preventing them from being misclassified as orphans. Unknown
// owner declarations fail closed; maintenance never guesses a table name.
func (r *Registry) RetainedSubjects(ctx context.Context, executor database.Executor, owner OwnerName, identities []model.Identity) (map[string]bool, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	entry, ok := r.owners[owner]
	if !ok {
		return nil, invalid("unknown model extension owner")
	}
	return entry.retained(ctx, executor, identities)
}
