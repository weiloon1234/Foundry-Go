package query

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ProjectionField retains both the declared result record and concrete field
// type. Generated field sets own these descriptors and SQL output aliases.
type ProjectionField[P, V any] struct{ column ProjectionColumn[P] }

// ProjectionColumn is erased field metadata inside one declared result record.
// Type identity remains available to validate explicit declaration boundaries.
type ProjectionColumn[P any] struct {
	_        [0]*P
	name     string
	typ      reflect.Type
	nullable bool
}

func NewProjectionField[P, V any](name string) ProjectionField[P, V] {
	return ProjectionField[P, V]{column: ProjectionColumn[P]{name: name, typ: reflect.TypeFor[V](), nullable: value.IsNullableType[V]()}}
}
func (f ProjectionField[P, V]) Column() ProjectionColumn[P] { return f.column }

func (d ProjectionDefinition[P]) sourceColumns() []Column {
	columns := make([]Column, len(d.columns))
	for i, c := range d.columns {
		columns[i] = Column{Name: c.name, Nullable: c.nullable}
	}
	return columns
}

// ProjectionDefinition describes a complete declared result record, never a
// partially hydrated persisted model. Generated code owns the ordered decoder.
type ProjectionDefinition[P any] struct {
	columns []ProjectionColumn[P]
	scan    func(database.Row) (P, error)
	fields  []RecordField[P]
}

func DefineProjection[P any](columns []ProjectionColumn[P], scan func(database.Row) (P, error), fields ...RecordField[P]) ProjectionDefinition[P] {
	return ProjectionDefinition[P]{columns: slices.Clone(columns), scan: scan, fields: slices.Clone(fields)}
}
func (d ProjectionDefinition[P]) Validate() error {
	if len(d.columns) == 0 || len(d.columns) > MaxExpressionNodes || d.scan == nil {
		return fault.New(fault.Invalid, "projection requires bounded columns and a complete decoder")
	}
	seen := make(map[string]bool, len(d.columns))
	for _, column := range d.columns {
		if !sqlname.Valid(column.name) || seen[column.name] || column.typ == nil {
			return fault.New(fault.Invalid, "invalid or repeated projection field")
		}
		seen[column.name] = true
	}
	if len(d.fields) > len(d.columns) {
		return fault.New(fault.Invalid, "too many projection field getters")
	}
	types := make(map[string]reflect.Type, len(d.columns))
	for _, column := range d.columns {
		types[column.name] = column.typ
	}
	for _, field := range d.fields {
		if types[field.column] == nil || types[field.column] != field.typ || field.get == nil || field.decode == nil {
			return fault.New(fault.Invalid, "invalid or repeated projection field getter")
		}
		delete(types, field.column)
	}
	return nil
}

// ProjectionMapping binds one declared result field to an expression from S.
// Map checks field value types; Project validates full and unique coverage.
type ProjectionMapping[S, P any] struct {
	_          [0]*S
	column     ProjectionColumn[P]
	expression valueExpression
}

func Map[S, P, V any](field ProjectionField[P, V], expression Expression[S, V]) ProjectionMapping[S, P] {
	return ProjectionMapping[S, P]{column: field.column, expression: expression.node}
}
