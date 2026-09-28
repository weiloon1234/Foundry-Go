package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// Definition describes all persisted columns of one model and its full-row
// decoder. Private copies preserve column order and prevent query mutation.
// It describes application declarations, not verified physical database schema.
type Definition[M any] struct {
	table, primary   string
	columns          []Column
	scan             func(database.Row) (M, error)
	modelFields      []ModelField[M]
	writeHooks       func(context.Context, lifecycle.Observers) (WriteHooks[M], error)
	hasWriteHooks    bool
	hasObserverHooks bool
	readHooks        func(context.Context, lifecycle.Observers) (RetrievalHooks[M], error)
	hasReadHooks     bool
	hasReadAdapter   bool
	timestamps       *timestampColumns
	softDelete       *softDeleteColumn
}

// Column describes one persisted column at the generated declaration boundary.
// DatabaseDefault permits omission of a non-nullable field on insert; the actual
// SQL default expression remains owned by the migration, never duplicated here.
type Column struct {
	Name            string
	Nullable        bool
	DatabaseDefault bool
}

// Define is an explicit declaration boundary for generated code. The decoder
// must scan every declared column into a fresh M and publish only on success.
func Define[M any](table, primary string, columns []Column, scan func(database.Row) (M, error), modelFields ...ModelField[M]) Definition[M] {
	return Definition[M]{table: table, primary: primary, columns: slices.Clone(columns), scan: scan, modelFields: slices.Clone(modelFields)}
}

func (d Definition[M]) Validate() error {
	if d.hasReadAdapter && d.readHooks == nil {
		return fault.New(fault.Invalid, "model retrieval hooks require an adapter")
	}
	if (d.hasWriteHooks || d.hasObserverHooks) && d.writeHooks == nil {
		return fault.New(fault.Invalid, "model write hooks require a factory")
	}
	if !sqlname.Table(d.table) || !sqlname.Valid(d.primary) || len(d.columns) == 0 || len(d.columns) > MaxExpressionNodes || d.scan == nil {
		return fault.New(fault.Invalid, "invalid model query definition")
	}
	seen := make(map[string]bool, len(d.columns))
	for _, column := range d.columns {
		if !sqlname.Valid(column.Name) || seen[column.Name] || (column.Name == d.primary && column.Nullable) {
			return fault.New(fault.Invalid, "invalid or repeated model column")
		}
		seen[column.Name] = true
	}
	if !seen[d.primary] {
		return fault.New(fault.Invalid, "model primary column is not declared")
	}
	if len(d.modelFields) > len(d.columns) {
		return fault.New(fault.Invalid, "too many model field declarations")
	}
	fieldSeen := make(map[string]bool, len(d.modelFields))
	for _, field := range d.modelFields {
		if !seen[field.column] || fieldSeen[field.column] || field.get == nil || field.decode == nil {
			return fault.New(fault.Invalid, "invalid or repeated model field declaration")
		}
		fieldSeen[field.column] = true
	}
	if err := d.validateTimestamps(); err != nil {
		return err
	}
	return d.validateSoftDeletes()
}

// ForModel starts a query with complete model metadata and hydration behavior.
func ForModel[M any](definition Definition[M]) Query[M] {
	return Query[M]{table: definition.table, definition: &definition}
}
