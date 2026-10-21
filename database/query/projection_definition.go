package query

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
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

func (d ProjectionDefinition[P]) sourceColumns() []columnData {
	columns := make([]columnData, len(d.columns))
	for i, c := range d.columns {
		columns[i] = columnData{Name: c.Name, Nullable: c.Nullable}
	}
	return columns
}

// ProjectionDefinition describes a complete declared result record, never a
// partially hydrated persisted model. Generated code owns the ordered decoder.
type ProjectionDefinition[P any] struct {
	columns []projectionColumn
	scan    func(database.Row) (P, error)
	fields  []RecordField[P]
}

func DefineProjection[P any](columns []ProjectionColumn[P], scan func(database.Row) (P, error), fields ...RecordField[P]) ProjectionDefinition[P] {
	lowered := make([]projectionColumn, len(columns))
	for i, c := range columns {
		lowered[i] = projectionColumn{Name: c.name, Typ: c.typ, Nullable: c.nullable}
	}
	return ProjectionDefinition[P]{columns: lowered, scan: scan, fields: slices.Clone(fields)}
}

func (d ProjectionDefinition[P]) Validate() error {
	if err := validateProjectionColumns(d.columns, d.scan != nil); err != nil {
		return err
	}
	if len(d.fields) > len(d.columns) {
		return fault.New(fault.Invalid, "too many projection field getters")
	}
	types := make(map[string]reflect.Type, len(d.columns))
	for _, column := range d.columns {
		types[column.Name] = column.Typ
	}
	for _, field := range d.fields {
		if err := validateProjectionFieldMetadata(field.recordFieldMetadata, field.get != nil, types); err != nil {
			return err
		}
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
