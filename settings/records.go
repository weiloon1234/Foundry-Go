package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	configured       bool
	data             value.JSON[json.RawMessage]
	created, updated temporal.DateTime
}

func (r Record) Name() Name                 { return r.name }
func (r Record) Version() Version           { return r.version }
func (r Record) Presentation() Presentation { return r.presentation }

// Configured reports whether Configure took ownership of the presentation, so
// declaration changes no longer replace it.
func (r Record) Configured() bool                       { return r.configured }
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

// errNoUpgrade is returned for a stored version the declaration cannot read.
func errNoUpgrade() error {
	return fault.New(fault.Conflict, "stored setting version has no declared upgrade")
}

// record validates a stored row. A value written by an earlier version with a
// declared upgrade is converted in memory and reported at the current version;
// Reconcile persists the conversion. Other version differences are conflicts.
func (m *Manager) record(ctx context.Context, row store.Setting) (Record, error) {
	registration, ok := m.keys[Name(row.Name)]
	if !ok {
		return Record{}, invalid()
	}
	data := row.Value
	if row.Version != uint32(registration.version) {
		upgrade, ok := registration.upgrades[Version(row.Version)]
		if !ok {
			return Record{}, errNoUpgrade()
		}
		converted, err := upgrade(ctx, row.Value)
		if err != nil {
			return Record{}, err
		}
		data = converted
	} else if err := registration.decode(ctx, row.Value); err != nil {
		return Record{}, err
	}
	p := Presentation{Kind: Kind(row.Kind), Group: Group(row.GroupName), Label: row.Label, Description: row.Description, Order: row.SortOrder, Public: row.IsPublic, Parameters: row.Parameters}
	if err := p.Validate(); err != nil {
		return Record{}, err
	}
	return Record{name: Name(row.Name), version: registration.version, presentation: p, configured: !row.PresentationDeclared, data: data, created: row.CreatedAt, updated: row.UpdatedAt}, nil
}

// Find reads one setting. With Options.Cache it is served from the manager's
// cache until the entry expires or a write through this manager invalidates it.
func (k Key[V]) Find(ctx context.Context, m *Manager) (value.Optional[Record], error) {
	if err := k.check(m); err != nil {
		return value.Optional[Record]{}, err
	}
	if ctx == nil {
		return value.Optional[Record]{}, invalid()
	}
	hit, ok, epoch := m.cached(k.Name())
	if ok {
		if err := ctx.Err(); err != nil {
			return value.Optional[Record]{}, err
		}
		return hit, nil
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
	m.remember(k.Name(), epoch, result)
	return result, nil
}

type Filter struct {
	Group      Group
	Prefix     string
	PublicOnly bool
}

// Cursor is the position after which ListPage continues: the group, sort order
// and name of the last record already returned. The zero Cursor starts at the
// beginning. It is a position, not a capability or a consistent snapshot.
type Cursor struct {
	Group Group
	Order int32
	Name  Name
}

func (c Cursor) IsZero() bool { return c == Cursor{} }

// After returns the cursor that continues after record r.
func After(r Record) Cursor {
	return Cursor{Group: r.presentation.Group, Order: r.presentation.Order, Name: r.name}
}

// Page is one bounded ListPage result. Next is zero after the last page.
type Page struct {
	Records []Record
	Next    Cursor
}

const (
	DefaultPageSize = 100
	// MaxLoadBytes bounds the stored JSON retained by one List, Load or LoadGroup.
	// ListPage never fails on aggregate size: it ends a page early instead.
	MaxLoadBytes = 64 << 20
)

var errPageFull = errors.New("settings page is full")

func (m *Manager) listNames(filter Filter) ([]string, error) {
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
	return names, nil
}
func (m *Manager) listQuery(names []string, filter Filter) store.SettingQuery {
	f := store.SettingFields()
	q := store.QueryFoundrySettings().Where(f.Name.In(names...))
	if filter.Group != "" {
		q = q.Where(f.GroupName.Eq(string(filter.Group)))
	}
	if filter.PublicOnly {
		q = q.Where(f.IsPublic.Eq(true))
	}
	return q.OrderBy(f.GroupName.Asc(), f.SortOrder.Asc(), f.Name.Asc())
}

// ListPage returns up to limit registered settings (DefaultPageSize when zero,
// at most MaxKeys) ordered by group, sort order and name, after the cursor. A
// page also ends early rather than retain more than MaxBatchBytes of JSON, so
// large settings never make the listing fail; continue with Page.Next. Each page
// is one bounded query in its own read snapshot.
func ListPage(ctx context.Context, m *Manager, filter Filter, after Cursor, limit int) (Page, error) {
	if err := m.Validate(); err != nil {
		return Page{}, err
	}
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxKeys || !after.IsZero() && (!identifier.Semantic(string(after.Group)) || !identifier.Semantic(string(after.Name))) {
		return Page{}, invalid()
	}
	names, err := m.listNames(filter)
	if err != nil {
		return Page{}, err
	}
	var result Page
	err = m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		result = Page{Records: make([]Record, 0)}
		if len(names) == 0 {
			return nil
		}
		f := store.SettingFields()
		q := m.listQuery(names, filter)
		if !after.IsZero() {
			g, o, n := string(after.Group), after.Order, string(after.Name)
			q = q.Where(query.Or(f.GroupName.Gt(g), query.And(f.GroupName.Eq(g), query.Or(f.SortOrder.Gt(o), query.And(f.SortOrder.Eq(o), f.Name.Gt(n))))))
		}
		var budget extensionvalue.BatchBudget
		err := q.Limit(limit+1).Each(ctx, tx, func(row store.Setting) error {
			if len(result.Records) == limit {
				result.Next = After(result.Records[len(result.Records)-1])
				return errPageFull
			}
			if err := budget.Add(row.Value); err != nil {
				return pageFull(&result, err)
			}
			if err := budget.Add(row.Parameters); err != nil {
				return pageFull(&result, err)
			}
			record, err := m.record(ctx, row)
			if err != nil {
				return err
			}
			result.Records = append(result.Records, record)
			return nil
		})
		if errors.Is(err, errPageFull) {
			return nil
		}
		return err
	})
	if err != nil {
		return Page{}, err
	}
	return result, nil
}

// pageFull ends a non-empty page at the byte budget. A single row always fits:
// stored values and parameters are individually far below MaxBatchBytes.
func pageFull(page *Page, err error) error {
	if len(page.Records) == 0 || !errors.Is(err, fault.Conflict) {
		return err
	}
	page.Next = After(page.Records[len(page.Records)-1])
	return errPageFull
}

// List returns this manager's registered settings ordered by group, sort order
// and name by reading consecutive ListPage pages; pages are separate snapshots.
// Prefix is a literal string, not SQL LIKE syntax. PublicOnly is explicit;
// callers still own endpoint authorization. The complete result retains at most
// MaxLoadBytes of JSON; use ListPage for larger administrative listings.
func List(ctx context.Context, m *Manager, filter Filter) ([]Record, error) {
	result := make([]Record, 0)
	retained := 0
	var cursor Cursor
	for {
		page, err := ListPage(ctx, m, filter, cursor, MaxKeys)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Records {
			size, err := recordBytes(r)
			if err != nil {
				return nil, err
			}
			if size > MaxLoadBytes-retained {
				return nil, fault.New(fault.Conflict, "settings list exceeds its byte limit; use ListPage")
			}
			retained += size
		}
		result = append(result, page.Records...)
		if page.Next.IsZero() {
			return result, nil
		}
		cursor = page.Next
	}
}
func recordBytes(r Record) (int, error) {
	text, err := r.data.Text()
	if err != nil {
		return 0, err
	}
	parameters, err := r.presentation.Parameters.Text()
	if err != nil {
		return 0, err
	}
	return len(text) + len(parameters), nil
}

// Groups lists the stored presentation groups of registered settings, ordered
// as List orders them. It reads presentation columns only, never values.
func Groups(ctx context.Context, m *Manager) ([]Group, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	names, err := m.listNames(Filter{})
	if err != nil {
		return nil, err
	}
	groups := make([]Group, 0)
	err = m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		groups = groups[:0]
		if len(names) == 0 {
			return nil
		}
		return store.SettingStates(m.listQuery(names, Filter{}).Limit(MaxKeys+1)).Each(ctx, tx, func(row store.SettingState) error {
			if !identifier.Semantic(row.GroupName) {
				return invalid()
			}
			if g := Group(row.GroupName); len(groups) == 0 || groups[len(groups)-1] != g {
				groups = append(groups, g)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// Batch is an immutable snapshot of selected settings. Values decode afresh on
// each access. Later writes do not change an existing Batch.
type Batch struct {
	manager  *Manager
	selected map[Name]value.Optional[Record]
}

func (Batch) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("settings batch")) }

// Load reads the selected registered keys with at most one query. Cached keys
// are served from the manager's cache; the others are read in one snapshot and
// may populate it. The retained JSON is bounded by MaxLoadBytes.
func Load(ctx context.Context, m *Manager, keys ...Selection) (Batch, error) {
	if err := m.Validate(); err != nil {
		return Batch{}, err
	}
	names := make([]Name, 0, len(keys))
	for _, key := range keys {
		registration, err := m.selected(key)
		if err != nil {
			return Batch{}, err
		}
		names = append(names, registration.name)
	}
	return m.load(ctx, names)
}

// LoadGroup loads every registered key whose declared presentation group is
// group, including keys without a stored row, which read as absent. Membership
// follows declarations so typed code sees a stable set of keys.
func LoadGroup(ctx context.Context, m *Manager, group Group) (Batch, error) {
	if err := m.Validate(); err != nil {
		return Batch{}, err
	}
	if !identifier.Semantic(string(group)) {
		return Batch{}, invalid()
	}
	names := make([]Name, 0)
	for name, registration := range m.keys {
		if registration.presentation.Group == group {
			names = append(names, name)
		}
	}
	return m.load(ctx, names)
}
func (m *Manager) load(ctx context.Context, names []Name) (Batch, error) {
	if ctx == nil {
		return Batch{}, invalid()
	}
	slices.Sort(names)
	names = slices.Compact(names)
	result := Batch{manager: m, selected: make(map[Name]value.Optional[Record], len(names))}
	epochs := make(map[Name]uint64, len(names))
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if hit, ok, epoch := m.cached(name); ok {
			result.selected[name] = hit
		} else {
			epochs[name] = epoch
			missing = append(missing, string(name))
		}
	}
	if len(missing) == 0 {
		if err := ctx.Err(); err != nil {
			return Batch{}, err
		}
		return result, nil
	}
	loaded := make(map[Name]value.Optional[Record], len(missing))
	err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		clear(loaded)
		for _, name := range missing {
			loaded[Name(name)] = value.Optional[Record]{}
		}
		f := store.SettingFields()
		retained := 0
		return store.QueryFoundrySettings().Where(f.Name.In(missing...)).OrderBy(f.Name.Asc()).Each(ctx, tx, func(row store.Setting) error {
			record, err := m.record(ctx, row)
			if err != nil {
				return err
			}
			size, err := recordBytes(record)
			if err != nil {
				return err
			}
			if size > MaxLoadBytes-retained {
				return fault.New(fault.Conflict, "settings batch exceeds its byte limit")
			}
			retained += size
			loaded[record.name] = value.Set(record)
			return nil
		})
	})
	if err != nil {
		return Batch{}, err
	}
	for name, record := range loaded {
		m.remember(name, epochs[name], record)
		result.selected[name] = record
	}
	return result, nil
}

// From decodes this key from a Batch without I/O. A key that was not selected
// for the batch is an error rather than a silently missing value.
func (k Key[V]) From(ctx context.Context, b Batch) (value.Optional[V], error) {
	if err := k.check(b.manager); err != nil {
		return value.Optional[V]{}, err
	}
	record, ok := b.selected[k.Name()]
	if !ok || ctx == nil {
		return value.Optional[V]{}, invalid()
	}
	r, present := record.Get()
	if !present {
		return value.Optional[V]{}, nil
	}
	decoded, err := k.Decode(ctx, r)
	if err != nil {
		return value.Optional[V]{}, err
	}
	return value.Set(decoded), nil
}

// FromOr applies GetOr's fallback rules to a Batch value.
func (k Key[V]) FromOr(ctx context.Context, b Batch, fallback V) (V, error) {
	result, err := k.From(ctx, b)
	if err != nil {
		return *new(V), err
	}
	if v, ok := result.Get(); ok {
		return v, nil
	}
	return k.fallback(ctx, fallback)
}
