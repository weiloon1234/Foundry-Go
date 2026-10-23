package orm

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
)

// RetrievalHooks observes complete stored models after hydration and row closure.
// Example: RetrievalHooks[Order]{Retrieved: inspectOrder}.
type RetrievalHooks[M any] struct {
	// Retrieved receives the caller's executor; reads create no implicit write transaction.
	// Example: Retrieved: func(ctx context.Context, ex database.Executor, order Order) error { return nil }.
	Retrieved func(context.Context, database.Executor, M) error
}

// WithRetrievalHooks installs a factory run once per nonempty complete batch.
// Example: WithRetrievalHooks(func() RetrievalHooks[Order] { return orderReadHooks() }).
func WithRetrievalHooks[M any](factory func() RetrievalHooks[M]) Option {
	opt := Option{retrievalModel: reflect.TypeFor[M]()}
	if factory != nil {
		opt.retrieval = func() retrievalSet { return eraseRetrieval(factory()) }
	}
	return opt
}

// RetrievalObserver retains a retrieval-only model factory at registration.
// Example: NewRetrievalObserver[Order]("orders.read").
type RetrievalObserver[M any] struct {
	observer lifecycle.Observer[M, retrievalSet]
}

// Name returns the registration identifier. Example: observer.Name().
func (o RetrievalObserver[M]) Name() string { return o.observer.Name() }

// Validate checks the identifier without constructing hooks. Example: observer.Validate().
func (o RetrievalObserver[M]) Validate() error { return o.observer.Validate() }

// Declare captures the per-batch factory without running it. Example: observer.Declare(orderReadHooks).
func (o RetrievalObserver[M]) Declare(factory func() RetrievalHooks[M]) (lifecycle.Declaration, error) {
	var wrapped func() retrievalSet
	if factory != nil {
		wrapped = func() retrievalSet { return eraseRetrieval(factory()) }
	}
	return o.observer.Declare(wrapped)
}

// NewRetrievalObserver declares an instance-owned complete-read observer.
// Example: NewRetrievalObserver[Order]("orders.read").
func NewRetrievalObserver[M any](name string) RetrievalObserver[M] {
	return RetrievalObserver[M]{observer: lifecycle.NewRetrievalObserver[M, retrievalSet](name)}
}

// RegisterRetrievalObserver constructs dependencies once during application assembly.
// Example: RegisterRetrievalObserver(r, pool, NewRetrievalObserver[Order]("orders.read"), constructOrderReads).
func RegisterRetrievalObserver[M any](r *foundation.Registrar, pool foundation.Key[*database.DB], observer RetrievalObserver[M], construct func(foundation.Resolver) (func() RetrievalHooks[M], error)) error {
	if construct == nil {
		return invalid("retrieval observer needs a dependency constructor")
	}
	return database.RegisterObserver(r, pool, observer.observer, func(resolver foundation.Resolver) (func() retrievalSet, error) {
		factory, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		if factory == nil {
			return nil, invalid("retrieval observer needs a hook factory")
		}
		return func() retrievalSet { return eraseRetrieval(factory()) }, nil
	})
}

type retrievalSet struct {
	invoke func(context.Context, database.Executor, unsafe.Pointer) error
}

func eraseRetrieval[M any](hooks RetrievalHooks[M]) retrievalSet {
	if hooks.Retrieved == nil {
		return retrievalSet{}
	}
	return retrievalSet{invoke: func(ctx context.Context, ex database.Executor, p unsafe.Pointer) error {
		return hooks.Retrieved(ctx, ex, *(*M)(p))
	}}
}
func mayRetrieve(t *tableCore, ex database.Executor) bool {
	if t == nil {
		return false
	}
	set, known := ownerObservers(ex)
	return !known || t.retrieval != nil || t.readObserved(set)
}

// retrievalBatch tracks the current backing array and the window collected for
// one closed fetch. Scanners refresh it after append, including reallocations.
type retrievalBatch struct {
	table        *tableCore
	base         unsafe.Pointer
	start, count int
}

func (b *retrievalBatch) capture(base unsafe.Pointer, count int) {
	b.base = unsafe.Add(base, uintptr(b.start)*b.table.typ.Size())
	b.count = count - b.start
}
func readBatches(ex database.Executor, batches ...*retrievalBatch) *ormcore.ReadCallbacks {
	return &ormcore.ReadCallbacks{
		Needed: func(set lifecycle.Observers) bool {
			for _, b := range batches {
				if b.table.retrieval != nil || b.table.readObserved(set) {
					return true
				}
			}
			return false
		},
		Complete: func(ctx context.Context, set lifecycle.Observers) error {
			hooks := make([][]retrievalSet, len(batches))
			for i, b := range batches {
				if b.count == 0 {
					continue
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				factories, err := b.table.readFactories(set)
				if err != nil {
					return err
				}
				if b.table.retrieval != nil {
					hooks[i] = append(hooks[i], b.table.retrieval())
				}
				for _, factory := range factories {
					if err := ctx.Err(); err != nil {
						return err
					}
					hooks[i] = append(hooks[i], factory())
				}
			}
			// Joined roles construct target then pivot factories; each stored row
			// dispatches target then pivot, matching the generated API.
			count := batches[0].count
			for row := 0; row < count; row++ {
				for i, b := range batches {
					base := b.base
					for _, hook := range hooks[i] {
						if err := ctx.Err(); err != nil {
							return err
						}
						if hook.invoke != nil {
							if err := hook.invoke(ctx, ex, unsafe.Add(base, uintptr(row)*b.table.typ.Size())); err != nil {
								return err
							}
						}
					}
				}
			}
			return ctx.Err()
		},
	}
}
