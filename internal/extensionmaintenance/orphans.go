// Package extensionmaintenance shares bounded, owner-aware orphan inspection
// and explicit pruning. Feature adapters own their ordinary generated queries.
package extensionmaintenance

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionrow"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Cursor struct {
	table string
	owner extensions.OwnerName
	after string
}

func (c Cursor) IsZero() bool { return c.after == "" }

type Page[R any] struct {
	Rows    []R
	Scanned int
	Next    Cursor
}
type Table[R any] struct {
	Name       string
	ValidKey   func(string) bool
	Scan       func(context.Context, *database.Tx, extensions.OwnerName, string, string, int) ([]R, error)
	Lock       func(context.Context, *database.Tx, extensions.OwnerName, string, []string) ([]R, error)
	Identity   func(*extensions.Registry, extensions.OwnerName, string, R) (model.Identity, error)
	Key        func(R) string
	SubjectKey func(R) string
	Delete     func(context.Context, *database.Tx, R) error
}

func (t Table[R]) validKey(key string) bool {
	if t.ValidKey != nil {
		return t.ValidKey(key)
	}
	return extensionrow.ValidKey(key)
}

func (t Table[R]) validate() error {
	if t.Name == "" || t.Scan == nil || t.Lock == nil || t.Identity == nil || t.Key == nil || t.SubjectKey == nil || t.Delete == nil {
		return invalid()
	}
	return nil
}
func Inspect[R any](ctx context.Context, store *extensions.Store, owner extensions.OwnerName, cursor Cursor, limit int, table Table[R]) (Page[R], error) {
	if err := store.Validate(); err != nil {
		return Page[R]{}, err
	}
	if err := table.validate(); err != nil {
		return Page[R]{}, err
	}
	scope, err := store.Registry().Scope(owner)
	if err != nil {
		return Page[R]{}, err
	}
	if limit < 1 || limit > 1000 || !cursor.IsZero() && (cursor.table != table.Name || cursor.owner != owner || !table.validKey(cursor.after)) {
		return Page[R]{}, invalid()
	}
	var result Page[R]
	err = store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		rows, err := table.Scan(ctx, tx, owner, scope, cursor.after, limit+1)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows = rows[:limit]
			result.Next = Cursor{table: table.Name, owner: owner, after: table.Key(rows[len(rows)-1])}
		}
		result.Scanned = len(rows)
		identities := make([]model.Identity, len(rows))
		for i, row := range rows {
			identity, err := table.Identity(store.Registry(), owner, scope, row)
			if err != nil {
				return err
			}
			identities[i] = identity
		}
		retained, err := store.Registry().RetainedSubjects(ctx, tx, owner, identities)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !retained[table.SubjectKey(row)] {
				result.Rows = append(result.Rows, row)
			}
		}
		return nil
	})
	if err != nil {
		return Page[R]{}, err
	}
	return result, nil
}
func Prune[R any](ctx context.Context, store *extensions.Store, owner extensions.OwnerName, keys []string, table Table[R]) (int, error) {
	if err := store.Validate(); err != nil {
		return 0, err
	}
	if err := table.validate(); err != nil {
		return 0, err
	}
	scope, err := store.Registry().Scope(owner)
	if err != nil {
		return 0, err
	}
	if len(keys) > 1000 {
		return 0, invalid()
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !table.validKey(key) || seen[key] {
			return 0, invalid()
		}
		seen[key] = true
	}
	if len(keys) == 0 {
		if ctx == nil {
			return 0, invalid()
		}
		return 0, ctx.Err()
	}
	count := 0
	err = store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		rows, err := table.Lock(ctx, tx, owner, scope, keys)
		if err != nil {
			return err
		}
		identities := make([]model.Identity, len(rows))
		for i, row := range rows {
			identity, err := table.Identity(store.Registry(), owner, scope, row)
			if err != nil {
				return err
			}
			identities[i] = identity
		}
		retained, err := store.Registry().RetainedSubjects(ctx, tx, owner, identities)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if retained[table.SubjectKey(row)] {
				continue
			}
			if err := table.Delete(ctx, tx, row); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
func invalid() error { return fault.New(fault.Invalid, "invalid model extension maintenance request") }

// Stale identifies one row recorded under an earlier scope of its registered
// owner, such as rows written before a table rename or a declared storage model
// change, and the key the row has in the owner's current scope.
type Stale struct {
	Key    string `json:"key"`
	Target string `json:"target"`
}

// RescopePage lists stale rows moved (or movable) into the current scope and
// the rows left in place: Conflicts already have a current-scope equivalent,
// Missing belong to a subject that no longer exists (including soft-deleted
// subjects' absence), and Undeclared were recorded under a model the owner
// does not declare in OwnerOptions.PreviousModels. Entries are opaque row keys.
type RescopePage struct {
	Rows       []Stale
	Conflicts  []string
	Missing    []string
	Undeclared []string
	Next       Cursor
}

// RescopeTable adapts one feature table. Scan selects rows whose owner column
// matches and whose scope differs from the owner's current scope, ordered by
// key after the cursor, locking them when requested. Move replaces the stale
// row with its current-scope equivalent, preserving its value and timestamps.
type RescopeTable[R any] struct {
	Name   string
	Scan   func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, current, after string, limit int, lock bool) ([]R, error)
	Row    func(R) (extensionrow.Identity, []string)
	Exists func(ctx context.Context, tx *database.Tx, key string) (bool, error)
	Move   func(ctx context.Context, tx *database.Tx, row R, subject extensions.Subject, key string) error
}

func (t RescopeTable[R]) validate() error {
	if t.Name == "" || t.Scan == nil || t.Row == nil || t.Exists == nil || t.Move == nil {
		return invalid()
	}
	return nil
}

// Rescope inspects, or with apply moves, one bounded page of stale rows for a
// registered owner. Every row is verified against its recorded scope and the
// current key codec first; malformed rows fail closed. Only rows recorded under
// a model the owner declares are adopted, and only when their subject still
// exists (soft-deleted subjects count as existing). A row whose current-scope
// key already exists is left in place and reported as a conflict for an
// explicit operator decision. Nothing is merged or overwritten.
func Rescope[R any](ctx context.Context, store *extensions.Store, owner extensions.OwnerName, cursor Cursor, limit int, apply bool, table RescopeTable[R]) (RescopePage, error) {
	if err := store.Validate(); err != nil {
		return RescopePage{}, err
	}
	if err := table.validate(); err != nil {
		return RescopePage{}, err
	}
	scope, err := store.Registry().Scope(owner)
	if err != nil {
		return RescopePage{}, err
	}
	if limit < 1 || limit > 1000 || !cursor.IsZero() && (cursor.table != table.Name || cursor.owner != owner || !extensionrow.ValidKey(cursor.after)) {
		return RescopePage{}, invalid()
	}
	var result RescopePage
	run := func(ctx context.Context, tx *database.Tx) error {
		result = RescopePage{}
		rows, err := table.Scan(ctx, tx, owner, scope, cursor.after, limit+1, apply)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows = rows[:limit]
			last, _ := table.Row(rows[len(rows)-1])
			result.Next = Cursor{table: table.Name, owner: owner, after: last.Key}
		}
		type candidate struct {
			row     R
			key     string
			parts   []string
			subject extensions.Subject
		}
		candidates := make([]candidate, 0, len(rows))
		identities := make([]model.Identity, 0, len(rows))
		for _, row := range rows {
			identity, parts := table.Row(row)
			if identity.Scope == scope {
				return invalid()
			}
			subject, declared, err := extensionrow.Adopt(store.Registry(), owner, identity, parts...)
			if err != nil {
				return err
			}
			if !declared {
				result.Undeclared = append(result.Undeclared, identity.Key)
				continue
			}
			if subject.Scope != scope {
				return invalid()
			}
			current, err := subject.Identity.Decode()
			if err != nil {
				return err
			}
			candidates = append(candidates, candidate{row: row, key: identity.Key, parts: parts, subject: subject})
			identities = append(identities, current)
		}
		retained := map[string]bool{}
		if len(identities) != 0 {
			if retained, err = store.Registry().RetainedSubjects(ctx, tx, owner, identities); err != nil {
				return err
			}
		}
		for _, item := range candidates {
			row, parts, subject := item.row, item.parts, item.subject
			if !retained[subject.Key] {
				result.Missing = append(result.Missing, item.key)
				continue
			}
			input := make([]string, 0, 2+len(parts))
			input = append(input, subject.Scope, subject.Key)
			target := extensions.Digest(append(input, parts...)...)
			exists, err := table.Exists(ctx, tx, target)
			if err != nil {
				return err
			}
			if exists {
				result.Conflicts = append(result.Conflicts, item.key)
				continue
			}
			if apply {
				if err := table.Move(ctx, tx, row, subject, target); err != nil {
					return err
				}
			}
			result.Rows = append(result.Rows, Stale{Key: item.key, Target: target})
		}
		return nil
	}
	if apply {
		err = store.Write(ctx, run)
	} else {
		err = store.Read(ctx, run)
	}
	if err != nil {
		return RescopePage{}, err
	}
	return result, nil
}
