package modelbinding

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// KeyQuery is satisfied by generated model queries. Both model M and key K
// remain concrete; a query for one model cannot resolve another model's ID.
// Find retains the query's filters, eager loads, soft-delete scope and retrieval
// hooks. Transaction-only locked queries have a different executor contract.
type KeyQuery[M, K any] interface {
	Validate() error
	Find(context.Context, database.Executor, K) (value.Optional[M], error)
}

// ByKey binds a decoded path key to a generated primary-key query. The executor
// is supplied by application assembly, not retrieved from a global container.
// For a stored alternate key use ByField; Through composes declared parent/child
// relationships. Define remains available for custom application scopes.
func ByKey[P, M, K any](executor database.Executor, source KeyQuery[M, K], key func(P) K) Resolver[P, M] {
	if nilValue(executor) || nilValue(source) || key == nil {
		return Resolver[P, M]{err: fault.New(fault.Invalid, "model key binding requires an executor, query and key selector")}
	}
	return Resolver[P, M]{
		check: source.Validate,
		lookup: func(ctx context.Context, path P) (value.Optional[M], error) {
			return source.Find(ctx, executor, key(path))
		},
	}
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	ref := reflect.ValueOf(v)
	switch ref.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return ref.IsNil()
	}
	return false
}
