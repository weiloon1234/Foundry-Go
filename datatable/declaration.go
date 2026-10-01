package datatable

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type Action string

const (
	QueryAction   Action = "query"
	ExportAction  Action = "export"
	InspectAction Action = "inspect"
)

// Spec binds one row contract to server-owned authorization and row scope.
// Source must return the complete unpaginated query with mandatory tenant,
// visibility and soft-delete policy already applied. Authorize is mandatory even
// for explicitly public/system reports. A is trusted server context, never
// decoded from Request. Use auth.Guard[M] when binding an authenticated policy.
// Stable orders must uniquely identify a result (including grouped/joined rows).
type Spec[S, R, A any] struct {
	ID          TableID
	Row         contract.JSON[R]
	Columns     []ColumnRegistration[S, R]
	Filters     []FilterRegistration[S]
	DefaultSort []Sort
	Stable      []query.ProjectionOrder[S]
	Authorize   func(context.Context, A, Action) error
	Source      func(context.Context, A) (query.ProjectionQuery[S, R], error)
	Exports     bool
}

type tableIdentity struct{ marker byte }
type Table[S, R, A any] struct{ definition *tableDefinition[S, R, A] }
type tableDefinition[S, R, A any] struct {
	identity *tableIdentity
	spec     Spec[S, R, A]
	columns  []columnDeclaration[S, R]
	byColumn map[string]columnDeclaration[S, R]
	filters  map[string]filterDeclaration[S]
	info     Description
	err      error
}

type NamedFilterInfo struct {
	Name   string          `json:"name"`
	Label  i18n.MessageKey `json:"label"`
	Filter FilterInfo      `json:"filter"`
}

// Description is the table contribution to the shared client manifest. Row and
// Request schemas originate from their typed contracts. No second TS schema is
// inferred from SQL, model fields, column names or arbitrary reflection.
type Description struct {
	ID          TableID           `json:"id"`
	Row         contract.Schema   `json:"row"`
	Request     contract.Schema   `json:"request"`
	Columns     []ColumnInfo      `json:"columns"`
	Filters     []NamedFilterInfo `json:"filters,omitempty"`
	DefaultSort []Sort            `json:"default_sort,omitempty"`
	Exports     bool              `json:"exports"`
}

func Define[S, R, A any](spec Spec[S, R, A]) Table[S, R, A] {
	d := &tableDefinition[S, R, A]{identity: &tableIdentity{}, spec: spec, byColumn: make(map[string]columnDeclaration[S, R]), filters: make(map[string]filterDeclaration[S])}
	d.spec.Columns = slices.Clone(spec.Columns)
	d.spec.Filters = slices.Clone(spec.Filters)
	d.spec.DefaultSort = slices.Clone(spec.DefaultSort)
	d.spec.Stable = slices.Clone(spec.Stable)
	d.err = d.initialize()
	return Table[S, R, A]{definition: d}
}
func (d *tableDefinition[S, R, A]) initialize() error {
	spec := d.spec
	if !identifier.Semantic(string(spec.ID)) || spec.Authorize == nil || spec.Source == nil || len(spec.Columns) == 0 || len(spec.Columns) > MaxColumns || len(spec.Filters) > MaxColumns || len(spec.Stable) == 0 || len(spec.Stable) > MaxSorts {
		return invalid("invalid datatable declaration")
	}
	for _, order := range spec.Stable {
		if nilValue(order) {
			return invalid("stable ordering requires typed expressions")
		}
	}
	row, err := spec.Row.Description()
	if err != nil {
		return err
	}
	if err := contract.RejectPasswordOutput(row, "table row"); err != nil {
		return err
	}
	request, err := RequestJSON().Description()
	if err != nil {
		return err
	}
	d.info = Description{ID: spec.ID, Row: row, Request: request, DefaultSort: slices.Clone(spec.DefaultSort), Exports: spec.Exports}
	exports := 0
	var searchPhase FilterPhase
	for _, registration := range spec.Columns {
		c := registration.declaration
		if c.err != nil {
			return c.err
		}
		if !validName(c.name) || c.label.Validate() != nil {
			return invalid("invalid datatable column")
		}
		if _, exists := d.byColumn[c.name]; exists {
			return fault.New(fault.Duplicate, "datatable column is repeated")
		}
		scalar, err := spec.Row.DescribeScalarProperty(c.name)
		if err != nil {
			return invalid("datatable column must name a declared scalar DTO property")
		}
		info := ColumnInfo{Name: c.name, Label: c.label, Value: scalar, Sortable: c.order != nil, Searchable: c.searchable, Exportable: c.cell != nil}
		if c.filter != nil {
			f := *c.filter
			if err := d.addFilter(f); err != nil {
				return err
			}
			if !sameScalar(scalar.Value, f.info) {
				return invalid("column filter scalar differs from its DTO property")
			}
			copy, err := cloneFilterInfo(f.info)
			if err != nil {
				return err
			}
			info.Filter = &copy
		}
		if c.searchable {
			if c.filter == nil {
				return invalid("searchable columns require a declared contains filter")
			}
			info.SearchOperator = searchOperator(c.filter.info)
			if info.SearchOperator == "" {
				return invalid("searchable columns require a declared contains filter")
			}
			// Global search ORs every searchable column; one OR cannot span
			// the WHERE and HAVING phases, so reject the table, not a request.
			if searchPhase != "" && searchPhase != c.filter.info.Phase {
				return invalid("searchable columns cannot mix WHERE and HAVING phases")
			}
			searchPhase = c.filter.info.Phase
		}
		if c.cell != nil {
			if c.cellInfo == nil || !sameScalar(scalar.Value, *c.cellInfo) {
				return invalid("column export scalar differs from its DTO property")
			}
			exports++
		}
		d.columns = append(d.columns, c)
		d.byColumn[c.name] = c
		d.info.Columns = append(d.info.Columns, info)
	}
	for _, registration := range spec.Filters {
		f := registration.declaration
		if _, exists := d.byColumn[f.name]; exists {
			return fault.New(fault.Duplicate, "extra filter overlaps a displayed column")
		}
		if err := d.addFilter(f); err != nil {
			return err
		}
		copy, err := cloneFilterInfo(f.info)
		if err != nil {
			return err
		}
		d.info.Filters = append(d.info.Filters, NamedFilterInfo{Name: f.name, Label: f.label, Filter: copy})
	}
	if spec.Exports && exports == 0 {
		return invalid("export-enabled table requires exportable columns")
	}
	return d.validateSort(spec.DefaultSort)
}
func (d *tableDefinition[S, R, A]) addFilter(f filterDeclaration[S]) error {
	if f.err != nil {
		return f.err
	}
	if !validName(f.name) || f.label.Validate() != nil || f.build == nil {
		return invalid("invalid datatable filter")
	}
	if _, exists := d.filters[f.name]; exists {
		return fault.New(fault.Duplicate, "datatable filter is repeated")
	}
	d.filters[f.name] = f
	return nil
}
func (t Table[S, R, A]) Validate() error {
	if t.definition == nil || t.definition.identity == nil {
		return invalid("datatable is not defined")
	}
	return t.definition.err
}
func (t Table[S, R, A]) ID() TableID {
	if t.definition == nil {
		return ""
	}
	return t.definition.spec.ID
}
func (t Table[S, R, A]) Description() (Description, error) {
	if err := t.Validate(); err != nil {
		return Description{}, err
	}
	// Descriptions contain only framework-owned metadata, no callbacks or values.
	data, err := json.Marshal(t.definition.info)
	if err != nil {
		return Description{}, err
	}
	var result Description
	err = json.Unmarshal(data, &result)
	return result, err
}

type Registration struct {
	identity *tableIdentity
	id       TableID
	validate func() error
	describe func() (Description, error)
}

func (t Table[S, R, A]) Registration() Registration {
	if t.definition == nil {
		return Registration{}
	}
	return Registration{identity: t.definition.identity, id: t.ID(), validate: t.Validate, describe: t.Description}
}

type Registry struct{ entries map[TableID]Registration }

func NewRegistry(registrations ...Registration) (*Registry, error) {
	if len(registrations) > 1024 {
		return nil, invalid("too many datatables")
	}
	r := &Registry{entries: make(map[TableID]Registration, len(registrations))}
	for _, item := range registrations {
		if item.identity == nil || item.validate == nil || item.describe == nil {
			return nil, invalid("invalid datatable registration")
		}
		if err := item.validate(); err != nil {
			return nil, err
		}
		if _, exists := r.entries[item.id]; exists {
			return nil, fault.New(fault.Duplicate, "datatable ID is already registered")
		}
		r.entries[item.id] = item
	}
	return r, nil
}
func (r *Registry) Validate() error {
	if r == nil || r.entries == nil {
		return invalid("datatable registry is not initialized")
	}
	return nil
}
func (r *Registry) Descriptions() ([]Description, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	ids := make([]TableID, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := make([]Description, 0, len(ids))
	for _, id := range ids {
		info, err := r.entries[id].describe()
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, nil
}
func validName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && utf8.ValidString(name) && !strings.ContainsFunc(name, func(r rune) bool { return r < 32 || r == 127 })
}
func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func cloneScalar(info foundryhttp.URLScalarInfo) (foundryhttp.URLScalarInfo, error) {
	return info.Normalize()
}
func cloneFilterInfo(info FilterInfo) (FilterInfo, error) {
	var err error
	info.Scalar, err = cloneScalar(info.Scalar)
	info.Operators = slices.Clone(info.Operators)
	return info, err
}
func sameScalar(typ contract.Type, filter FilterInfo) bool {
	scalar := filter.Scalar.Value
	if typ.Nullable != filter.Nullable || typ.Kind != scalar.Kind || typ.Format != scalar.Format || typ.Bits != scalar.Bits || typ.Signed != scalar.Signed {
		return false
	}
	if len(typ.Cases) != len(scalar.Cases) {
		return false
	}
	for i := range typ.Cases {
		if string(typ.Cases[i]) != string(scalar.Cases[i]) {
			return false
		}
	}
	return true
}
func searchOperator(info FilterInfo) Operator {
	if slices.Contains(info.Operators, InsensitiveContains) {
		return InsensitiveContains
	}
	if slices.Contains(info.Operators, Contains) {
		return Contains
	}
	return ""
}
