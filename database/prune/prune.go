// Package prune removes obsolete models in bounded batches, like Laravel's
// Prunable and MassPrunable models. Applications declare each prunable model
// with a typed selection; the registry runs them explicitly (for example from
// a scheduled command). Nothing prunes during boot.
package prune

import (
	"context"
	"sort"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Name identifies one prunable declaration, such as app.sessions.
type Name string

// Mode selects how selected models are removed.
type Mode uint8

const (
	// Lifecycle removes each model through its ordinary (force) delete
	// lifecycle, running hooks and observers.
	Lifecycle Mode = iota + 1
	// Mass removes each batch with one DELETE and no per-model hooks.
	Mass
)

// Target is one prunable model declaration; construct it with Model.
type Target struct {
	name Name
	mode Mode
	run  func(context.Context, database.Transactor, int) (int64, error)
}

// Model declares that the models selected by query are obsolete. Keep the
// selection's filters in the query (for example CreatedAt older than a
// cutoff computed from TransactionTime); soft-delete models are force-deleted
// within the query's visibility.
func Model[M any](name Name, selection query.Query[M], mode Mode) Target {
	return Target{name: name, mode: mode, run: func(ctx context.Context, db database.Transactor, size int) (int64, error) {
		return selection.PruneBatch(ctx, db, size, mode == Mass)
	}}
}

// Registry holds validated declarations in name order.
type Registry struct {
	targets []Target
	byName  map[Name]int
}

// New validates unique semantic names and known modes.
func New(targets ...Target) (*Registry, error) {
	r := &Registry{byName: make(map[Name]int, len(targets))}
	for _, target := range targets {
		if !identifier.Semantic(string(target.name)) || target.run == nil || (target.mode != Lifecycle && target.mode != Mass) {
			return nil, fault.New(fault.Invalid, "invalid prunable declaration")
		}
		if _, exists := r.byName[target.name]; exists {
			return nil, fault.New(fault.Duplicate, "duplicate prunable "+string(target.name))
		}
		r.byName[target.name] = len(r.targets)
		r.targets = append(r.targets, target)
	}
	sort.Slice(r.targets, func(i, j int) bool { return r.targets[i].name < r.targets[j].name })
	for i, target := range r.targets {
		r.byName[target.name] = i
	}
	return r, nil
}

// Names lists declarations in name order.
func (r *Registry) Names() []Name {
	names := make([]Name, len(r.targets))
	for i, target := range r.targets {
		names[i] = target.name
	}
	return names
}

// Options bounds one run. Each batch is its own transaction.
type Options struct {
	BatchSize  int
	MaxBatches int
}

// DefaultOptions removes up to 100 batches of 500 models per declaration.
func DefaultOptions() Options { return Options{BatchSize: 500, MaxBatches: 100} }

// Count reports one declaration's committed removals.
type Count struct {
	Name    Name  `json:"name"`
	Removed int64 `json:"removed"`
	// Remaining is true when MaxBatches stopped the run before a short batch.
	Remaining bool `json:"remaining,omitempty"`
}

// Result lists committed removals. StoppedAt names the declaration whose
// batch failed; earlier batches, including its own, remain committed.
type Result struct {
	Counts    []Count `json:"counts"`
	StoppedAt *Name   `json:"stopped_at,omitempty"`
}

// Run prunes the selected declarations (all when none are named) in name
// order, batch by batch, until a batch removes fewer than BatchSize models or
// MaxBatches is reached. A failure stops the run and reports confirmed work.
func (r *Registry) Run(ctx context.Context, db database.Transactor, options Options, selected ...Name) (Result, error) {
	if ctx == nil || db == nil {
		return Result{}, fault.New(fault.Invalid, "pruning requires a context and database")
	}
	if options.BatchSize < 1 || options.BatchSize > query.MaxPerModelWriteRows || options.MaxBatches < 1 {
		return Result{}, fault.New(fault.Invalid, "invalid prune bounds")
	}
	targets := r.targets
	if len(selected) != 0 {
		targets = make([]Target, 0, len(selected))
		seen := make(map[Name]bool, len(selected))
		for _, name := range selected {
			index, exists := r.byName[name]
			if !exists {
				return Result{}, fault.New(fault.Missing, "prunable is not registered: "+string(name))
			}
			if seen[name] {
				return Result{}, fault.New(fault.Duplicate, "prunable selection is duplicated")
			}
			seen[name] = true
			targets = append(targets, r.targets[index])
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	}
	var result Result
	for _, target := range targets {
		count := Count{Name: target.name}
		for batch := 0; ; batch++ {
			if batch == options.MaxBatches {
				count.Remaining = true
				break
			}
			if err := ctx.Err(); err != nil {
				result.Counts = append(result.Counts, count)
				return result, err
			}
			removed, err := target.run(ctx, db, options.BatchSize)
			if err != nil {
				name := target.name
				result.Counts, result.StoppedAt = append(result.Counts, count), &name
				return result, err
			}
			count.Removed += removed
			if removed < int64(options.BatchSize) {
				break
			}
		}
		result.Counts = append(result.Counts, count)
	}
	return result, nil
}
