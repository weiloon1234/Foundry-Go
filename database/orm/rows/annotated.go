package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Annotated pairs a complete hydrated model with a computed value from its row.
// Example: item.Model.Name and item.Value from rows.WithValue.
type Annotated[M, V any] struct {
	annotatedPhase [0]struct{}
	Model          M
	Value          V
}

// WithValue selects a complete model and a computed row value in one statement.
// Model retrieval callbacks and eager relations retain their ordinary ownership.
// Example: rows.WithValue(Users.Query(),expr.RelatedValue(UserRelations.Orders,expr.Count[Order]())).All(ctx,db).
func WithValue[M, V any](q orm.Query[M], computed expr.RowValue[M, V]) RecordRead[M, Annotated[M, V]] {
	r := orm.FoundryModelRead(q, sqlowner.Metadata())
	if r.Source.Err != nil {
		return RecordRead[M, Annotated[M, V]]{core: r}
	}
	selected := expr.Select(computed)
	expression := expr.FoundrySelected(selected, sqlowner.Expressions())
	column := expr.FoundrySelectedColumn(selected, sqlowner.Expressions())
	column.Sensitive = column.Sensitive || expression.Sensitive
	scalar := scalarRecord(column).record
	parent := r.Record
	var item Annotated[M, V]
	modelOffset, valueOffset := unsafe.Offsetof(item.Model), unsafe.Offsetof(item.Value)
	count := len(parent.Columns)

	d := &valueRecordDefinition{}
	d.record = ormcore.Record{
		Type: reflect.TypeFor[Annotated[M, V]](), Owner: d, Primary: parent.Primary,
		Columns: append(slices.Clone(parent.Columns), querycore.Column{Name: querycore.AnnotatedValueAlias, Nullable: column.Nullable}),
		Err:     errors.Join(parent.Err, scalar.Err), Checks: ormcore.AppendChecks(parent.Checks, scalar.Checks...),
		Scan: func(row database.Row, p unsafe.Pointer) error {
			if parent.Scan == nil {
				return ormcore.Invalid("annotated read requires a model decoder")
			}
			if err := parent.Scan(ormcore.RelationRow{Row: row, Count: count, Total: count + 1}, unsafe.Add(p, modelOffset)); err != nil {
				return err
			}
			return scalar.Scan(ormcore.RelationRow{Row: row, Start: count, Count: 1, Total: count + 1}, unsafe.Add(p, valueOffset))
		},
		Fields: func() []ormcore.RecordField {
			fields := parent.Fields()
			for i := range fields {
				get := fields[i].Get
				fields[i].Get = func(p unsafe.Pointer) (driver.Value, error) { return get(unsafe.Add(p, modelOffset)) }
			}
			field := scalar.Fields()[0]
			field.Name = querycore.AnnotatedValueAlias
			get := field.Get
			field.Get = func(p unsafe.Pointer) (driver.Value, error) { return get(unsafe.Add(p, valueOffset)) }
			return append(fields, field)
		},
		Callbacks: func(ex database.Executor, data func() (unsafe.Pointer, int)) *ormcore.ReadCallbacks {
			var models []M
			hooks := parent.Callbacks(ex, func() (unsafe.Pointer, int) { return unsafe.Pointer(unsafe.SliceData(models)), len(models) })
			return &ormcore.ReadCallbacks{Needed: hooks.Needed, Complete: func(ctx context.Context, set lifecycle.Observers) error {
				base, n := data()
				items := unsafe.Slice((*Annotated[M, V])(base), n)
				models = make([]M, n)
				for i := range items {
					models[i] = items[i].Model
				}
				if err := hooks.Complete(ctx, set); err != nil {
					return err
				}
				for i := range items {
					items[i].Model = models[i]
				}
				return nil
			}}
		},
	}
	r.Source.Err = errors.Join(r.Source.Err, expression.Err, ormcore.CheckReferenceSet(r.Source, expression.Refs, expression.ReferenceFree))
	r.Source.Node.Selections = append(slices.Clone(r.Source.Node.Selections), querycore.SelectItem{Expression: expression.Node, Alias: querycore.AnnotatedValueAlias, Sensitive: column.Sensitive})
	r.Record = d.record
	return RecordRead[M, Annotated[M, V]]{core: r}
}
