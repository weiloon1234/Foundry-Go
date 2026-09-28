package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensionvalue"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Record's explicit dynamic export prevents accidental serialization of a
// private setting when an administrative record is logged or returned by HTTP.
type Record struct {
	name             Name
	version          Version
	presentation     Presentation
	data             value.JSON[json.RawMessage]
	created, updated temporal.DateTime
}

func (r Record) Name() Name                             { return r.name }
func (r Record) Version() Version                       { return r.version }
func (r Record) Presentation() Presentation             { return r.presentation }
func (r Record) CreatedAt() temporal.DateTime           { return r.created }
func (r Record) UpdatedAt() temporal.DateTime           { return r.updated }
func (r Record) DynamicValue() (json.RawMessage, error) { return r.data.Decode() }
func (Record) Format(s fmt.State, _ rune)               { _, _ = s.Write([]byte("setting record")) }
func (Record) MarshalJSON() ([]byte, error)             { return nil, invalid() }
func (k Key[V]) Decode(ctx context.Context, r Record) (V, error) {
	if err := k.Validate(); err != nil {
		return *new(V), err
	}
	if r.name != k.Name() || r.version != k.Version() {
		return *new(V), invalid()
	}
	return extensionvalue.Decode(ctx, k.definition.codec, r.data)
}
func (m *Manager) record(ctx context.Context, row store.Setting) (Record, error) {
	registration, ok := m.keys[Name(row.Name)]
	if !ok {
		return Record{}, invalid()
	}
	if row.Version != uint32(registration.version) {
		return Record{}, fault.New(fault.Conflict, "stored setting version differs from its descriptor")
	}
	p := Presentation{Kind: Kind(row.Kind), Group: Group(row.GroupName), Label: row.Label, Description: row.Description, Order: row.SortOrder, Public: row.IsPublic, Parameters: row.Parameters}
	if err := p.Validate(); err != nil {
		return Record{}, err
	}
	if err := registration.decode(ctx, row.Value); err != nil {
		return Record{}, err
	}
	return Record{name: Name(row.Name), version: Version(row.Version), presentation: p, data: row.Value, created: row.CreatedAt, updated: row.UpdatedAt}, nil
}
func (k Key[V]) Find(ctx context.Context, m *Manager) (value.Optional[Record], error) {
	if err := k.check(m); err != nil {
		return value.Optional[Record]{}, err
	}
	var result value.Optional[Record]
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		f := store.SettingFields()
		row, err := store.QueryFoundrySettings().Where(f.Name.Eq(string(k.Name()))).First(ctx, tx)
		if err != nil {
			return err
		}
		if r, ok := row.Get(); ok {
			record, err := m.record(ctx, r)
			if err != nil {
				return err
			}
			result = value.Set(record)
		}
		return nil
	})
	if err != nil {
		return value.Optional[Record]{}, err
	}
	return result, nil
}

type Filter struct {
	Group      Group
	Prefix     string
	PublicOnly bool
}

// List returns this manager's registered settings ordered by group, sort order
// and name. It performs one bounded query. Prefix is a literal string, not SQL
// LIKE syntax. PublicOnly is explicit; callers still own endpoint authorization.
func List(ctx context.Context, m *Manager, filter Filter) ([]Record, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if filter.Group != "" && !identifier.Semantic(string(filter.Group)) {
		return nil, invalid()
	}
	if !validText(filter.Prefix, 128) {
		return nil, invalid()
	}
	names := make([]string, 0, len(m.keys))
	for name := range m.keys {
		if strings.HasPrefix(string(name), filter.Prefix) {
			names = append(names, string(name))
		}
	}
	slices.Sort(names)
	var result []Record
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		result = make([]Record, 0)
		if len(names) == 0 {
			return nil
		}
		f := store.SettingFields()
		q := store.QueryFoundrySettings().Where(f.Name.In(names...))
		if filter.Group != "" {
			q = q.Where(f.GroupName.Eq(string(filter.Group)))
		}
		if filter.PublicOnly {
			q = q.Where(f.IsPublic.Eq(true))
		}
		var budget extensionvalue.BatchBudget
		return q.OrderBy(f.GroupName.Asc(), f.SortOrder.Asc(), f.Name.Asc()).Limit(MaxKeys+1).Each(ctx, tx, func(row store.Setting) error {
			if len(result) >= MaxKeys {
				return invalid()
			}
			if err := budget.Add(row.Value); err != nil {
				return err
			}
			if err := budget.Add(row.Parameters); err != nil {
				return err
			}
			record, err := m.record(ctx, row)
			if err != nil {
				return err
			}
			result = append(result, record)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func Groups(ctx context.Context, m *Manager) ([]Group, error) {
	records, err := List(ctx, m, Filter{})
	if err != nil {
		return nil, err
	}
	groups := make([]Group, 0)
	for _, r := range records {
		g := r.presentation.Group
		if len(groups) == 0 || groups[len(groups)-1] != g {
			groups = append(groups, g)
		}
	}
	return groups, nil
}
