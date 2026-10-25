package datatable

import (
	"context"
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
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	identity  *tableIdentity
	metadata  tableMetadata
	row       contract.JSON[R]
	authorize func(context.Context, A, Action) error
	source    func(context.Context, A) (query.ProjectionQuery[S, R], error)
	stable    []query.ProjectionOrder[S]
	columns   []columnExecution[S, R]
	filters   map[string]func(Operator, []string) (condition[S], error)
	err       error
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
	d := &tableDefinition[S, R, A]{identity: &tableIdentity{}, row: spec.Row, authorize: spec.Authorize, source: spec.Source, stable: slices.Clone(spec.Stable)}
	d.metadata.info.ID = spec.ID
	d.err = d.initialize(spec)
	return Table[S, R, A]{definition: d}
}
func (d *tableDefinition[S, R, A]) initialize(spec Spec[S, R, A]) error {
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
	columns := make([]columnMetadataDeclaration, 0, len(spec.Columns))
	filters := make([]filterMetadataDeclaration, 0, len(spec.Filters))
	d.filters = make(map[string]func(Operator, []string) (condition[S], error))
	for _, registration := range spec.Columns {
		c := registration.declaration
		metadata := columnMetadataDeclaration{name: c.name, label: c.label, sortable: c.order != nil, searchable: c.searchable, exportable: c.cell != nil, cellInfo: c.cellInfo, err: c.err}
		execution := columnExecution[S, R]{order: c.order, cell: c.cell}
		if c.filter != nil {
			f := c.filter
			execution.filter = f.build
			metadata.filter = &filterMetadataDeclaration{name: f.name, label: f.label, info: f.info, hasBuilder: f.build != nil, err: f.err}
			d.filters[f.name] = f.build
		}
		columns = append(columns, metadata)
		d.columns = append(d.columns, execution)
	}
	for _, registration := range spec.Filters {
		f := registration.declaration
		filters = append(filters, filterMetadataDeclaration{name: f.name, label: f.label, info: f.info, hasBuilder: f.build != nil, err: f.err})
		d.filters[f.name] = f.build
	}
	return d.metadata.initialize(spec.ID, row, spec.Row.DescribeScalarProperty, columns, filters, spec.DefaultSort, spec.Exports)
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
	return t.definition.metadata.info.ID
}
func (t Table[S, R, A]) Description() (Description, error) {
	if err := t.Validate(); err != nil {
		return Description{}, err
	}
	return t.definition.metadata.description()
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
			return nil, safeerror.NewFault(fault.Duplicate, "datatable ID is already registered")
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
