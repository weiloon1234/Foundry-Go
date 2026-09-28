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
