// Package seed runs explicitly registered data seeders through normal Foundry
// transactions. It never resets tables or treats repeated execution as a wipe.
package seed

import (
	"context"
	"errors"
	"sort"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/dependency"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// ID names a seeder once at registration. Use namespaces such as app.countries
// to distinguish application and plugin contributions.
type ID string

// Definition declares normal transactional domain work and its prerequisites.
// Run must use the supplied Tx. Idempotence is its own business contract; no
// history table silently suppresses later explicit invocations.
type Definition struct {
	ID       ID
	Requires []ID
	Run      func(context.Context, *database.Tx) error
}

// Registry snapshots declarations and validates dependencies before execution.
// Shared callbacks must be concurrency-safe if applications invoke Run concurrently.
type Registry struct {
	byID    map[ID]Definition
	ordered []ID
}

func New(definitions ...Definition) (*Registry, error) {
	r := &Registry{byID: make(map[ID]Definition, len(definitions))}
	for _, definition := range definitions {
		if !identifier.Semantic(string(definition.ID)) || definition.Run == nil {
			return nil, fault.New(fault.Invalid, "invalid seeder definition")
		}
		if _, exists := r.byID[definition.ID]; exists {
			return nil, fault.New(fault.Duplicate, "duplicate seeder "+string(definition.ID))
		}
		definition.Requires = append([]ID(nil), definition.Requires...)
		sort.Slice(definition.Requires, func(i, j int) bool { return definition.Requires[i] < definition.Requires[j] })
		for i, id := range definition.Requires {
			if !identifier.Semantic(string(id)) || id == definition.ID {
				return nil, fault.New(fault.Invalid, "invalid seeder dependency")
			}
			if i > 0 && id == definition.Requires[i-1] {
				return nil, fault.New(fault.Duplicate, "duplicate seeder dependency")
			}
		}
		r.byID[definition.ID] = definition
		r.ordered = append(r.ordered, definition.ID)
	}
	sort.Slice(r.ordered, func(i, j int) bool { return r.ordered[i] < r.ordered[j] })
	ordered, err := r.order(r.ordered)
	if err != nil {
		return nil, err
	}
	r.ordered = ordered
	return r, nil
}

func (r *Registry) order(ids []ID) ([]ID, error) {
	ordered, failure := dependency.Order(ids, func(id ID) ([]ID, bool) { definition, exists := r.byID[id]; return definition.Requires, exists })
	if failure != nil {
		if failure.Kind == dependency.Cycle {
			return nil, fault.New(fault.Cycle, "seeder dependency cycle at "+string(failure.Key))
		}
		return nil, fault.New(fault.Missing, "seeder is not registered: "+string(failure.Key))
	}
	return ordered, nil
}

func (r *Registry) IDs() []ID { return append([]ID(nil), r.ordered...) }

// Result lists seeders whose database transactions are confirmed committed.
// StoppedAt identifies the invocation that returned an error. It can also appear
// in Committed when only an after-commit callback failed. Inspect database error
// outcomes before retrying; the framework never retries arbitrary domain work.
type Result struct {
	Committed []ID `json:"committed"`
	StoppedAt *ID  `json:"stopped_at,omitempty"`
}

// Run executes selected seeders and their dependencies once in deterministic
// order. Omit selected to run the whole registry. Selection errors fail before
// database I/O. Each seeder uses a separate transaction; earlier commits remain
// if a later seeder fails. The same seeder may run again in a later explicit call.
func (r *Registry) Run(ctx context.Context, db *database.DB, selected ...ID) (Result, error) {
	if db == nil {
		return Result{}, fault.New(fault.Invalid, "seeding needs a database")
	}
	roots := append([]ID(nil), selected...)
	if len(roots) == 0 {
		roots = r.IDs()
	} else {
		sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
		for i, id := range roots {
			if i > 0 && id == roots[i-1] {
				return Result{}, fault.New(fault.Duplicate, "seeder selection is duplicated")
			}
		}
	}
	ordered, err := r.order(roots)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, id := range ordered {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		definition := r.byID[id]
		err := db.Transaction(ctx, func(tx *database.Tx) error { return definition.Run(ctx, tx) })
		if err != nil {
			result.StoppedAt = &id
			var detail *database.Error
			if errors.As(err, &detail) && detail.Outcome() == database.Committed {
				result.Committed = append(result.Committed, id)
			}
			return result, err
		}
		result.Committed = append(result.Committed, id)
	}
	return result, nil
}
