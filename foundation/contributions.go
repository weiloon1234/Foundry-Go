package foundation

import (
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Collection groups typed feature contributions within one explicit service
// scope. A collection does not discover plugins or construct unrelated scopes.
type Collection[T any] struct{ name string }

func NewCollection[T any](name string) Collection[T] { return Collection[T]{name} }
func (c Collection[T]) Name() string                 { return c.name }

type collectionRef struct {
	name string
	typ  reflect.Type
}

// Contribute registers an ordinary typed factory in a named collection. The
// semantic ID and scope form an unambiguous key for duplicate/override checks.
func Contribute[T any](r *Registrar, collection Collection[T], id string, create func(Resolver) (T, error)) error {
	return ContributeAs[T](r, collection, id, create)
}

// ContributeAs preserves schema S at an erased feature boundary T, for example
// a job payload and jobs.Declaration. An explicit override must retain both T
// and S. Feature helpers infer S from their existing typed declarations.
func ContributeAs[S, T any](r *Registrar, collection Collection[T], id string, create func(Resolver) (T, error)) error {
	if !validName(collection.name) || len(collection.name) > 1024 || !validName(id) || len(id) > 1024 || create == nil || r == nil || r.registry == nil {
		return fault.New(fault.Invalid, "invalid typed feature contribution")
	}
	key := NewKey[T](fmt.Sprintf("foundry.contribution.%q.%q", collection.name, id))
	return r.registry.add(serviceDefinition{
		ref: ref(key), owner: r.owner, schema: reflect.TypeFor[S](),
		collection: collectionRef{collection.name, reflect.TypeFor[T]()},
		create: func(resolver Resolver) (any, error) {
			value, err := create(resolver)
			if err != nil {
				return nil, err
			}
			if nilValue(value) {
				return nil, fault.New(fault.Invalid, "nil feature contribution")
			}
			return value, nil
		},
	}, r.override)
}

// Contributions constructs only members of this exact collection in dependency
// and registration order. Results are independent slices; object ownership
// follows the same rules as Resolve. Constructors must remain pure.
func Contributions[T any](r Resolver, collection Collection[T]) ([]T, error) {
	if nilValue(r) || !validName(collection.name) {
		return nil, fault.New(fault.Invalid, "invalid contribution collection")
	}
	values, err := r.resolveCollection(collectionRef{collection.name, reflect.TypeFor[T]()})
	if err != nil {
		return nil, err
	}
	result := make([]T, len(values))
	for i, value := range values {
		item, ok := value.(T)
		if !ok {
			return nil, fault.New(fault.Internal, "invalid contribution value type")
		}
		result[i] = item
	}
	return result, nil
}

func (s *Services) resolveCollection(collection collectionRef) ([]any, error) {
	var result []any
	for _, key := range s.order {
		if s.collections[key.name] != collection {
			continue
		}
		value, err := s.resolve(key)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (r *constructionResolver) resolveCollection(collection collectionRef) ([]any, error) {
	release, err := r.beginResolve()
	if err != nil {
		return nil, err
	}
	defer release()
	var result []any
	for _, key := range r.build.order {
		if r.build.definitions[key.name].collection != collection {
			continue
		}
		value, err := r.resolveLocked(key)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}
