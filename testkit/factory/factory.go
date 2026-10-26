// Package factory builds typed ORM drafts and persists them through the
// ordinary model lifecycle. It never resets data or substitutes raw inserts.
package factory

import (
	"context"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type Sequence uint64

const MaxStates = 64
const MaxCount = querycore.MaxPerModelWriteRows

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
// Define call has independent state. Callbacks must themselves support concurrent
// calls when the factory is shared; each invocation returns its own draft.
type Factory[M, D any] struct {
	prepare  func([]D, bool) (func(context.Context, database.Transactor) ([]M, error), error)
	build    Build[D]
	states   []State[D]
	after    []AfterCreate[M]
	sequence *sequence
}

func (f *Factory[M, D]) valid() error {
	if f == nil || f.build == nil || f.sequence == nil || f.prepare == nil {
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

// Create uses normal defaults/UUID preparation, field transformations,
// model observers, timestamps and the caller's actual transaction owner. With
// AfterCreating hooks, the insert and every hook share one transaction (a
// savepoint inside the caller's transaction), so a failing hook leaves no row.
func (f *Factory[M, D]) Create(ctx context.Context, writer database.Transactor) (M, error) {
	draft, err := f.Draft(ctx)
	if err != nil {
		return *new(M), err
	}
	run, err := f.prepare([]D{draft}, true)
	if err != nil {
		return *new(M), err
	}
	if len(f.after) == 0 {
		rows, err := run(ctx, writer)
		if err != nil {
			return *new(M), err
		}
		return rows[0], nil
	}
	if writer == nil {
		return *new(M), fault.New(fault.Invalid, "factory requires a database writer")
	}
	var created M
	err = writer.Transaction(ctx, func(tx *database.Tx) error {
		rows, err := run(ctx, tx)
		if err != nil {
			return err
		}
		created, err = f.afterCreate(ctx, tx, rows[0])
		return err
	})
	if err != nil {
		return *new(M), err
	}
	return created, nil
}

// CreateMany is atomic, bounded and ordered. Every input executes its normal
// model hooks through InsertEach, and AfterCreating hooks run in the same
// transaction (a savepoint inside the caller's transaction). A later failure,
// including a failing hook, rolls back the whole batch and its after-commit
// work; factories never switch to the hook-free bulk API.
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
	drafts := make([]D, count)
	for i := range drafts {
		draft, err := f.Draft(ctx)
		if err != nil {
			return nil, err
		}
		drafts[i] = draft
	}
	run, err := f.prepare(drafts, false)
	if err != nil {
		return nil, err
	}
	if len(f.after) == 0 {
		return run(ctx, writer)
	}
	if writer == nil {
		return nil, fault.New(fault.Invalid, "factory requires a database writer")
	}
	var created []M
	err = writer.Transaction(ctx, func(tx *database.Tx) error {
		inserted, err := run(ctx, tx)
		if err != nil {
			return err
		}
		for i := range inserted {
			if inserted[i], err = f.afterCreate(ctx, tx, inserted[i]); err != nil {
				return err
			}
		}
		created = inserted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
