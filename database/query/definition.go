package query

import (
	"context"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Column describes one persisted column at the generated declaration boundary.
// DatabaseDefault permits omission on insert; SQL defaults remain migration-owned.
// Encrypted values bind to the row identity and require an assigned primary key.
type Column querycore.Column

// Definition describes all persisted columns of one model and its full-row
// decoder. Private copies preserve column order and prevent query mutation.
// It describes application declarations, not verified physical database schema.
type Definition[M any] struct {
	modelDefinition  modelDefinition
	scan             func(database.Row) (M, error)
	modelFields      []ModelField[M]
	writeHooks       func(context.Context, lifecycle.Observers) (WriteHooks[M], error)
	hasWriteHooks    bool
	hasObserverHooks bool
	readHooks        func(context.Context, lifecycle.Observers) (RetrievalHooks[M], error)
	hasReadHooks     bool
	hasReadAdapter   bool
	// check memoizes Validate for the immutable copy owned by ForModel, so
	// compiling each statement does not revalidate the whole declaration.
	// Any other copy has a different address and validates itself.
	check *definitionCheck[M]
}

type definitionCheck[M any] struct {
	owner      *Definition[M]
	once       sync.Once
	err        error
	selections []selectItem
}

// Define is an explicit declaration boundary for generated code. The decoder
// must scan every declared column into a fresh M and publish only on success.
func Define[M any](table, primary string, columns []Column, scan func(database.Row) (M, error), modelFields ...ModelField[M]) Definition[M] {
	return Definition[M]{modelDefinition: modelDefinition{Table: table, Primary: primary, Columns: columnDataSlice(columns)}, scan: scan, modelFields: slices.Clone(modelFields)}
}

func (d Definition[M]) Validate() error {
	if d.hasReadAdapter && d.readHooks == nil {
		return safeerror.NewFault(fault.Invalid, "model retrieval hooks require an adapter")
	}
	if (d.hasWriteHooks || d.hasObserverHooks) && d.writeHooks == nil {
		return safeerror.NewFault(fault.Invalid, "model write hooks require a factory")
	}
	seen, err := d.modelDefinition.ValidateColumns(d.scan != nil)
	if err != nil {
		return err
	}
	if len(d.modelFields) > len(d.modelDefinition.Columns) {
		return safeerror.NewFault(fault.Invalid, "too many model field declarations")
	}
	fieldSeen := make(map[string]bool, len(d.modelFields))
	for _, field := range d.modelFields {
		if err := validateModelFieldMetadata(field.recordFieldMetadata, field.get != nil, seen, fieldSeen); err != nil {
			return err
		}
	}
	if err := d.validateTimestamps(); err != nil {
		return err
	}
	if err := d.modelDefinition.ValidateGlobalScopes(); err != nil {
		return err
	}
	return d.validateSoftDeletes()
}

// ForModel starts a query with complete model metadata and hydration behavior.
// The query owns an immutable copy; its declaration is validated once, on the
// first compilation, and the result is shared by every derived query.
func ForModel[M any](definition Definition[M]) Query[M] {
	owned := &definition
	owned.check = &definitionCheck[M]{owner: owned}
	return Query[M]{queryCore: queryCore{Table: definition.modelDefinition.Table, Model: &owned.modelDefinition}, definition: owned}
}

// validated returns the memoized declaration check of a ForModel copy.
func (d *Definition[M]) validated() error {
	if d.check == nil || d.check.owner != d {
		return d.Validate()
	}
	d.check.once.Do(func() {
		d.check.err = d.Validate()
		if d.check.err == nil {
			d.check.selections = columnSelections(d.modelDefinition.Table, d.modelDefinition.Columns)
		}
	})
	return d.check.err
}

// The model's ordered selection is immutable, like its decoder and columns.
// Requalification/aliasing paths copy before rewriting these expressions.
// Copies outside ForModel cannot borrow another definition's cached metadata.
func (d *Definition[M]) selections(table string) []selectItem {
	if d.check != nil && d.check.owner == d && table == d.modelDefinition.Table && d.validated() == nil {
		return d.check.selections
	}
	return columnSelections(table, d.modelDefinition.Columns)
}

// bindModel replaces only registered metadata; relation filters/options remain
// owned by the descriptor being bound. Both views refer to the same definition.
func (q Query[M]) bindModel(source Query[M]) Query[M] {
	q.queryCore.Table, q.queryCore.Model, q.definition = source.queryCore.Table, source.queryCore.Model, source.definition
	return q
}
