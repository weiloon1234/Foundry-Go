package foundation

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ResolveAll collects services registered with exactly the declared type T, in
// provider/registration order. Use it during construction to assemble explicit
// feature contributions, then inject the constructed feature into domain code.
// It does not discover assignable concrete types or autowire dependencies.
// An empty collection is valid. The returned slice is independent; its service
// values retain their usual application lifetime and concurrency ownership.
func ResolveAll[T any](r Resolver) ([]T, error) {
	if nilValue(r) {
		return nil, fault.New(fault.Invalid, "invalid service resolver")
	}
	values, err := r.resolveAll(reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	result := make([]T, len(values))
	for i, value := range values {
		item, ok := value.(T)
		if !ok {
			return nil, fault.New(fault.Internal, "service value does not match its declared type")
		}
		result[i] = item
	}
	return result, nil
}

func (s *Services) resolveAll(typ reflect.Type) ([]any, error) {
	var result []any
	for _, key := range s.order {
		if key.typ != typ {
			continue
		}
		item, err := s.resolve(key)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *constructionResolver) resolveAll(typ reflect.Type) ([]any, error) {
	release, err := r.beginResolve()
	if err != nil {
		return nil, err
	}
	defer release()
	var result []any
	for _, key := range r.build.order {
		if key.typ != typ {
			continue
		}
		item, err := r.resolveLocked(key)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
