// Package factory builds typed generated drafts and persists them through the
// ordinary model lifecycle. It never resets data or substitutes raw inserts.
package factory

import (
	"context"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type Sequence uint64

const MaxStates = 64
const MaxCount = query.MaxPerModelWriteRows

// Model and query.CreateDraft are the existing generated integration contracts.
// The draft must describe this exact model; another model's draft cannot bind.
type Model[M any] interface{ FoundryQuery() query.Query[M] }
type Build[D any] func(context.Context, Sequence) (D, error)
type State[D any] func(context.Context, D) (D, error)

type sequence struct {
	mu   sync.Mutex
	last Sequence
}

func (s *sequence) next() (Sequence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == ^Sequence(0) {
		return 0, fault.New(fault.Invalid, "factory sequence is exhausted")
	}
	s.last++
	return s.last, nil
}

// Factory owns a sequence and immutable state callbacks. Derived states share
// their parent's sequence, preventing accidental reuse across variants. Each
// New call has independent state. Callbacks must themselves support concurrent
// calls when the factory is shared; each invocation returns its own draft.
type Factory[M Model[M], D query.CreateDraft[M]] struct {
	query    query.Query[M]
	build    Build[D]
	states   []State[D]
	sequence *sequence
}

func New[M Model[M], D query.CreateDraft[M]](build Build[D]) (*Factory[M, D], error) {
	if build == nil {
		return nil, fault.New(fault.Invalid, "factory requires a typed draft builder")
	}
	var model M
	var definition query.Query[M]
	err := callback.Isolated("prepare factory model", func() error {
		definition = model.FoundryQuery()
		_, err := definition.Compile()
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Factory[M, D]{query: definition, build: build, sequence: &sequence{}}, nil
}

func (f *Factory[M, D]) valid() error {
	if f == nil || f.build == nil || f.sequence == nil {
		return fault.New(fault.Invalid, "uninitialized test factory")
	}
	return nil
}

// WithStates appends immutable draft transformations in declaration order.
func (f *Factory[M, D]) WithStates(states ...State[D]) (*Factory[M, D], error) {
	if err := f.valid(); err != nil {
		return nil, err
	}
	if len(states) > MaxStates-len(f.states) {
		return nil, fault.New(fault.Invalid, "factory state count exceeds its bound")
	}
	for _, state := range states {
		if state == nil {
			return nil, fault.New(fault.Invalid, "nil factory state")
		}
	}
	result := *f
	result.states = append(slices.Clone(f.states), states...)
	return &result, nil
}

// Draft performs no database work. An attempted build consumes its sequence
// number even on failure; numbers are never reset or silently reused.
func (f *Factory[M, D]) Draft(ctx context.Context) (D, error) {
	var draft D
	if err := f.valid(); err != nil {
		return draft, err
	}
	if ctx == nil {
		return draft, fault.New(fault.Invalid, "factory requires a context")
	}
	if err := ctx.Err(); err != nil {
		return draft, err
	}
	number, err := f.sequence.next()
	if err != nil {
		return draft, err
	}
	err = callback.Isolated("build factory draft", func() error {
		var err error
		draft, err = f.build(ctx, number)
		if err != nil {
			return err
		}
		for _, state := range f.states {
			if err := ctx.Err(); err != nil {
				return err
			}
			draft, err = state(ctx, draft)
			if err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	if err != nil {
		return *new(D), err
	}
	return draft, nil
}

func (f *Factory[M, D]) mutation(ctx context.Context) (query.Mutation[M], error) {
	draft, err := f.Draft(ctx)
	if err != nil {
		return query.Mutation[M]{}, err
	}
	var mutation query.Mutation[M]
	err = callback.Isolated("prepare factory mutation", func() error {
		var err error
		mutation, err = draft.FoundryCreateMutation(query.Mutation[M]{})
		return err
	})
	return mutation, err
}

// Create uses normal generated defaults/UUID preparation, field transformations,
// model observers, timestamps and the caller's actual transaction owner.
func (f *Factory[M, D]) Create(ctx context.Context, writer database.Transactor) (M, error) {
	mutation, err := f.mutation(ctx)
	if err != nil {
		return *new(M), err
	}
	return f.query.Insert(ctx, writer, mutation)
}

// CreateMany is atomic, bounded and ordered. Every input executes its normal
// model hooks through InsertEach. A later failure rolls back the whole batch and
// its after-commit work; factories never switch to the hook-free bulk API.
func (f *Factory[M, D]) CreateMany(ctx context.Context, writer database.Transactor, count int) ([]M, error) {
	if err := f.valid(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "factory requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count < 0 || count > MaxCount {
		return nil, fault.New(fault.Invalid, "factory count exceeds its bound")
	}
	mutations := make([]query.Mutation[M], count)
	for i := range mutations {
		mutation, err := f.mutation(ctx)
		if err != nil {
			return nil, err
		}
		mutations[i] = mutation
	}
	return f.query.InsertEach(ctx, writer, mutations)
}
