package rows

import (
	"context"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// WhereSource, GroupSource, HavingSource and DistinctSource are the source-phase
// operations of a base source that a datatable or rows.Select then selects from.

// WhereSource appends scoped row predicates to an unselected source.
// Example: rows.WhereSource(joined, expr.Eq(expr.At(at, e.State), enums.EntityStateInReview)).
func WhereSource[S any](source SelectionSource[S], predicates ...expr.Predicate[S]) Input[S] {
	return Input[S]{source: whereSource(sourceOf(source), predicates)}
}

// GroupSource appends row-phase grouping keys to an unselected source.
// Example: rows.GroupSource(joined, expr.Group(expr.At(o, ut.Key()))).
func GroupSource[S any](source SelectionSource[S], keys ...expr.GroupKey[S]) Input[S] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, key := range keys {
		inputs[i] = expr.FoundryGroup(key, sqlowner.Expressions())
	}
	return Input[S]{source: ormcore.GroupBy(sourceOf(source), inputs)}
}

// HavingSource appends fixed group predicates to an unselected grouped source.
// Example: rows.HavingSource(grouped, expr.GtValue(expr.Count[Order](), 0)).
func HavingSource[S any](source SelectionSource[S], predicates ...expr.HavingPredicate[S]) Input[S] {
	inputs := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		inputs[i] = expr.FoundryHaving(p, sqlowner.Expressions())
	}
	return Input[S]{source: ormcore.Having(sourceOf(source), inputs)}
}

// DistinctSource marks an unselected source DISTINCT.
// Example: rows.DistinctSource(rows.From(Users.Query())).
func DistinctSource[S any](source SelectionSource[S]) Input[S] {
	return Input[S]{source: ormcore.Distinct(sourceOf(source), querycore.DistinctRows, nil)}
}

// The Foundry* plan declarations below are the bridge datatable uses to lower its
// columns into one sealed projection. Application code uses datatable, not these.

type dynamicSlotKey struct{ _ byte }

// planField records how one bound row field is selected: a static expression, or
// the dynamic slot whose expression arrives per request.
type planField struct {
	expression ormcore.Expression
	slot       *dynamicSlotKey
	custom     bool
}

type planSlot struct {
	position int
	custom   bool
}

type planCore struct {
	row         *rowCore
	staticItems []querycore.SelectItem
	staticExprs []ormcore.Expression
	slots       map[*dynamicSlotKey]planSlot
}

type planBuildState struct {
	decl   *declaration
	fields []planField
}

// FoundryPlanBuilder binds row fields while a plan is built; application code uses datatable.
type FoundryPlanBuilder[S, R any] struct {
	_     [0]*S
	_     [0]*R
	state *planBuildState
}

// FoundryPlan is the sealed projection of one datatable; application code uses datatable.
type FoundryPlan[S, R any] struct {
	_      [0]*S
	core   *planCore
	decode func(database.Row) (R, error)
}

// FoundryDynamicSlot names one request-dependent field of scope S and value V;
// application code uses datatable.DynamicSlot. The value type cannot be converted.
type FoundryDynamicSlot[S, V any] struct {
	_   [0]*S
	_   [0]*V
	key *dynamicSlotKey
}

// FoundryDynamicExpression is one request's expression for a FoundryDynamicSlot;
// application code uses datatable.BindDynamic.
type FoundryDynamicExpression[S any] struct {
	_          [0]*S
	key        *dynamicSlotKey
	expression ormcore.Expression
	// field binds the slot through the expression's own codec; nil when the
	// expression has no decoder. check is the selected column's own check.
	field func(owner *rowCore, offset uintptr, name string) *fieldCore
	check func() error
	err   error
}

// FoundryNewDynamicSlot issues a distinct slot; application code uses datatable.NewDynamicSlot.
func FoundryNewDynamicSlot[S, V any](seal sqlowner.QuerySeal) FoundryDynamicSlot[S, V] {
	if !seal.Valid() {
		return FoundryDynamicSlot[S, V]{}
	}
	return FoundryDynamicSlot[S, V]{key: new(dynamicSlotKey)}
}

// FoundryDynamicValue pairs a slot with a request's expression of the same scope
// and value type; application code uses datatable.BindDynamic.
func FoundryDynamicValue[S, V any](slot FoundryDynamicSlot[S, V], selected expr.Selected[S, V], seal sqlowner.QuerySeal) FoundryDynamicExpression[S] {
	if !seal.Valid() {
		return FoundryDynamicExpression[S]{err: ormcore.Invalid("invalid SQL owner capability")}
	}
	if slot.key == nil {
		return FoundryDynamicExpression[S]{err: ormcore.Invalid("dynamic projection slot is not initialized")}
	}
	e := expr.FoundrySelected(selected, sqlowner.Expressions())
	c := expr.FoundrySelectedColumn(selected, sqlowner.Expressions())
	e.Err = safeerror.Join("operation failed", e.Err, c.Err)
	result := FoundryDynamicExpression[S]{key: slot.key, expression: e, check: c.Check}
	if c.Scan != nil {
		result.field = func(owner *rowCore, offset uintptr, name string) *fieldCore {
			return newFieldCore[V](owner, offset, name, (*columnField[V])(&c))
		}
	}
	return result
}

// FoundryScan is one value type's custom scan codec, prepared once by
// FoundryScanWith; application code uses datatable.ScanWith. Only declarations
// that use it compile the codec binding.
type FoundryScan[V any] struct {
	_    [0]*V
	bind func(*declaration, *V) (*fieldCore, int)
}

// FoundryScanWith prepares option as a row field scan codec; application code uses datatable.ScanWith.
func FoundryScanWith[V any](option orm.ColumnOption[V], seal sqlowner.QuerySeal) FoundryScan[V] {
	if !seal.Valid() {
		return FoundryScan[V]{}
	}
	c := orm.FoundryCodec(option, sqlowner.Metadata())
	return FoundryScan[V]{bind: func(d *declaration, ptr *V) (*fieldCore, int) { return bindCodecField(d, ptr, &c) }}
}

// FoundryBindPlanField binds a row field to a selected value of the same type,
// scanned by the value's own codec or one FoundryScan, and returns the bound
// struct field index (-1 when nothing was bound); application code uses datatable.
func FoundryBindPlanField[S, R, V any](b FoundryPlanBuilder[S, R], ptr *V, selected expr.Selected[S, V], scan ...FoundryScan[V]) int {
	if d := b.declaration(); d != nil {
		return bindPlanField(b.state, ptr, expr.FoundrySelected(selected, sqlowner.Expressions()), expr.FoundrySelectedColumn(selected, sqlowner.Expressions()), scan)
	}
	return -1
}

// FoundryBindPlanDynamic binds a row field to a slot of the same value type whose
// expression is supplied per request, and returns the bound struct field index
// (-1 when nothing was bound); application code uses datatable.DynamicColumn.
func FoundryBindPlanDynamic[S, R, V any](b FoundryPlanBuilder[S, R], ptr *V, slot FoundryDynamicSlot[S, V], scan ...FoundryScan[V]) int {
	if d := b.declaration(); d != nil {
		return bindPlanDynamic(b.state, ptr, slot.key, scan)
	}
	return -1
}

// bindPlanField and bindPlanDynamic carry the binding work generic in V only, so
// one instantiation serves every table and scope of an application. A FoundryScan
// replaces only the scan codec: the selected column keeps its own error and check.
func bindPlanField[V any](state *planBuildState, ptr *V, e ormcore.Expression, col ormcore.Column[V], scan []FoundryScan[V]) int {
	d := state.decl
	custom, ok := planScan(d, scan)
	if !ok {
		return -1
	}
	e.Err = safeerror.Join("operation failed", e.Err, col.Err)
	index := -1
	if custom != nil {
		var f *fieldCore
		if f, index = custom(d, ptr); f != nil {
			f.check = col.Check
		}
	} else {
		_, index = bindColumnField(d, ptr, col)
	}
	if index >= 0 {
		state.fields[index] = planField{expression: e, custom: custom != nil}
	}
	return index
}

func bindPlanDynamic[V any](state *planBuildState, ptr *V, key *dynamicSlotKey, scan []FoundryScan[V]) int {
	d := state.decl
	if key == nil {
		d.errors = append(d.errors, ormcore.Invalid("dynamic projection slot is not initialized"))
		return -1
	}
	custom, ok := planScan(d, scan)
	if !ok {
		return -1
	}
	for _, f := range state.fields {
		if f.slot == key {
			d.errors = append(d.errors, ormcore.Invalid("dynamic projection slot is already declared"))
			return -1
		}
	}
	index := -1
	if custom != nil {
		_, index = custom(d, ptr)
	} else {
		_, index = bindDynamicPlaceholder(d, ptr)
	}
	if index >= 0 {
		state.fields[index] = planField{slot: key, custom: custom != nil}
	}
	return index
}

// planScan accepts at most one prepared scan codec.
func planScan[V any](d *declaration, scan []FoundryScan[V]) (func(*declaration, *V) (*fieldCore, int), bool) {
	switch {
	case len(scan) == 0:
		return nil, true
	case len(scan) > 1:
		d.errors = append(d.errors, ormcore.Invalid("row field scan codec option is repeated"))
	case scan[0].bind == nil:
		d.errors = append(d.errors, ormcore.Invalid("row field scan codec is not prepared"))
	default:
		return scan[0].bind, true
	}
	return nil, false
}

func (b FoundryPlanBuilder[S, R]) declaration() *declaration {
	if b.state == nil || b.state.decl == nil || b.state.decl.sealed {
		return nil
	}
	return b.state.decl
}

// FoundryBuildPlan binds and seals every row field once, reporting the same
// declaration errors as rows.Define; application code uses datatable.Define.
func FoundryBuildPlan[S, R any](seal sqlowner.QuerySeal, build func(FoundryPlanBuilder[S, R], *R)) (FoundryPlan[S, R], error) {
	if !seal.Valid() {
		err := ormcore.Invalid("invalid SQL owner capability")
		return FoundryPlan[S, R]{}, err
	}
	d, template := newDeclaration[R]()
	state := &planBuildState{decl: d, fields: make([]planField, len(d.fields))}
	if build == nil {
		d.errors = append(d.errors, ormcore.Invalid("projection plan callback is missing"))
	} else {
		build(FoundryPlanBuilder[S, R]{state: state}, template)
	}
	core := sealPlan(state)
	row := core.row
	decode := func(r database.Row) (R, error) {
		var item R
		if err := scanDeclaredRow(row, r, unsafe.Pointer(&item)); err != nil {
			return *new(R), err
		}
		return item, nil
	}
	return FoundryPlan[S, R]{core: core, decode: decode}, rowError(row)
}

func sealPlan(state *planBuildState) *planCore {
	d := state.decl
	for i, f := range d.fields {
		if f != nil && state.fields[i].slot == nil && state.fields[i].expression.Err != nil {
			d.errors = append(d.errors, state.fields[i].expression.Err)
		}
	}
	seal(d)
	row := d.core
	n := len(row.fields)
	core := &planCore{row: row, staticItems: make([]querycore.SelectItem, 0, n), staticExprs: make([]ormcore.Expression, 0, n)}
	for i, f := range d.fields {
		if f == nil {
			continue
		}
		m := state.fields[i]
		if m.slot != nil {
			if core.slots == nil {
				core.slots = make(map[*dynamicSlotKey]planSlot)
			}
			core.slots[m.slot] = planSlot{position: len(core.staticItems), custom: m.custom}
			core.staticItems = append(core.staticItems, querycore.SelectItem{Alias: f.column.Name, Sensitive: f.column.Sensitive})
			continue
		}
		core.staticExprs = append(core.staticExprs, m.expression)
		// Same item as querycore.SelectProjection builds for rows.Select.
		core.staticItems = append(core.staticItems, querycore.SelectItem{Expression: m.expression.Node, Alias: f.column.Name, Sensitive: f.column.Sensitive || m.expression.Sensitive, NonCollatable: m.expression.NonCollatable})
	}
	return core
}

// FoundryApply selects the plan from source with one expression for every dynamic
// slot; the query behaves as rows.Select does. Application code uses datatable.
func (p FoundryPlan[S, R]) FoundryApply(source SelectionSource[S], seal sqlowner.QuerySeal, dynamic ...FoundryDynamicExpression[S]) Query[S, R] {
	if !seal.Valid() {
		return Query[S, R]{source: ormcore.Source{Err: ormcore.Invalid("invalid SQL owner capability")}}
	}
	if p.core == nil || ormcore.NilDescriptor(source) {
		return Query[S, R]{source: ormcore.Source{Err: ormcore.Invalid("projection requires a source and plan")}}
	}
	src := source.rowSource(nil)
	row := p.core.row
	src.Err = safeerror.Join("operation failed", src.Err, row.err)
	for _, e := range p.core.staticExprs {
		src.Err = safeerror.Join("operation failed", src.Err, e.Err, ormcore.CheckReferenceSet(src, e.Refs, e.ReferenceFree))
	}
	owner, items := row, p.core.staticItems
	if len(p.core.slots) != 0 || len(dynamic) != 0 {
		var err error
		owner, items, err = applyDynamic(p.core, src, dynamic)
		src.Err = safeerror.Join("operation failed", src.Err, err)
	}
	if owner.err == nil {
		src.Checks = ormcore.AppendChecks(src.Checks, owner.check)
	}
	src.Node.Selections = items
	decode := p.decode
	if owner != row {
		decode = func(r database.Row) (R, error) {
			var item R
			if err := scanDeclaredRow(owner, r, unsafe.Pointer(&item)); err != nil {
				return *new(R), err
			}
			return item, nil
		}
	}
	return Query[S, R]{source: src, decode: decode, recordOwner: owner}
}

// FoundryCompileRead compiles reading q as its execution does, with the request
// context for context-reading scopes; application code uses datatable.Compile.
func FoundryCompileRead[S, R any](ctx context.Context, q Query[S, R], seal sqlowner.QuerySeal) (orm.Statement, error) {
	if err := compileCapability(ctx, seal); err != nil {
		return orm.Statement{}, err
	}
	return compileReadContext(ctx, readSourceOf(Read(q)))
}

// FoundryCompileCount compiles counting q as its execution does, with the request
// context for context-reading scopes; application code uses datatable.Compile.
func FoundryCompileCount[S, R any](ctx context.Context, q Query[S, R], seal sqlowner.QuerySeal) (orm.Statement, error) {
	if err := compileCapability(ctx, seal); err != nil {
		return orm.Statement{}, err
	}
	if err := ctx.Err(); err != nil {
		return orm.Statement{}, err
	}
	s, err := ormcore.CompileRecordCount(ctx, readSourceOf(Read(q)))
	if err != nil {
		return orm.Statement{}, err
	}
	return orm.FoundryStatement(s, sqlowner.Metadata())
}

func compileCapability(ctx context.Context, seal sqlowner.QuerySeal) error {
	if !seal.Valid() {
		return ormcore.Invalid("invalid SQL owner capability")
	}
	if ctx == nil {
		return ormcore.Invalid("query compilation requires a context")
	}
	return nil
}

// applyDynamic builds this request's row declaration: each dynamic field is bound
// through newFieldCore from its expression's codec, exactly as a static field. A
// slot with a FoundryScan keeps that codec and takes the expression's check.
func applyDynamic[S any](core *planCore, src ormcore.Source, dynamic []FoundryDynamicExpression[S]) (*rowCore, []querycore.SelectItem, error) {
	row := core.row
	if len(dynamic) != len(core.slots) {
		return row, core.staticItems, ormcore.Invalid("projection requires one expression for every dynamic slot")
	}
	items := slices.Clone(core.staticItems)
	owner := &rowCore{typ: row.typ, fields: slices.Clone(row.fields), columns: slices.Clone(row.columns), err: row.err}
	var err error
	for _, d := range dynamic {
		if d.err != nil {
			err = safeerror.Join("operation failed", err, d.err)
			continue
		}
		slot, ok := core.slots[d.key]
		if d.key == nil || !ok || items[slot.position].Expression != nil {
			err = safeerror.Join("operation failed", err, ormcore.Invalid("invalid or repeated dynamic projection slot"))
			continue
		}
		err = safeerror.Join("operation failed", err, d.expression.Err, ormcore.CheckReferenceSet(src, d.expression.Refs, d.expression.ReferenceFree))
		f := owner.fields[slot.position]
		switch {
		case slot.custom:
			if d.check != nil {
				// The plan's field is shared by every request: copy it to attach the check.
				custom := *f
				custom.check = d.check
				f = &custom
				owner.fields[slot.position] = f
			}
		case d.field == nil:
			err = safeerror.Join("operation failed", err, ormcore.Invalid("missing column decoder"))
		default:
			f = d.field(owner, f.offset, f.column.Name)
			owner.fields[slot.position] = f
			owner.columns[slot.position] = f.column
		}
		items[slot.position] = querycore.SelectItem{Expression: d.expression.Node, Alias: f.column.Name, Sensitive: f.column.Sensitive || d.expression.Sensitive, NonCollatable: d.expression.NonCollatable}
	}
	attachRowCheck(owner)
	return owner, items, err
}
