package orm

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// KeyLookup retains a filtered declaration and a typed lookup key.
// Example: orm.Keyed(Users.Query(),Users.Key()).Find(ctx,db,id).
type KeyLookup[M any, K comparable] struct {
	_    [0]*M
	core *keyLookupState[K]
}

// Keep retained predicates behind a pointer: fmt bypasses formatters for handles
// inside unexported fields, but does not dereference their private state pointers.
type keyLookupState[K comparable] struct {
	state   queryState
	field   columnData[K]
	primary bool
	err     error
}

// Keyed retains filters, eager loads, soft-delete scope and retrieval hooks.
// Example: modelbinding.ByKey(db,orm.Keyed(Users.Query(),Users.Key()),pathKey).
func Keyed[M any, K comparable](q Query[M], key Key[M, K]) KeyLookup[M, K] {
	return KeyLookup[M, K]{core: &keyLookupState[K]{state: q.state, field: key.key, primary: true}}
}

// Unique binds a stored alternate key, rejecting a second matching row.
// Example: orm.Unique(Users.Query(),Users.Email).Find(ctx,db,email).
func Unique[M any, K comparable](q Query[M], field KeyColumn[M, K]) KeyLookup[M, K] {
	if !lookupColumn(field) {
		return KeyLookup[M, K]{core: &keyLookupState[K]{err: invalid("unique lookup requires a stored field")}}
	}
	return KeyLookup[M, K]{core: &keyLookupState[K]{state: q.state, field: field.ormColumn(nil)}}
}

// Validate checks the declaration without SQL. Example: lookup.Validate().
func (l KeyLookup[M, K]) Validate() error {
	if l.core == nil {
		return invalid("lookup requires its model's stored field")
	}
	if l.core.err != nil {
		return l.core.err
	}
	if l.core.state.table == nil || l.core.field.core == nil || l.core.field.core.owner != l.core.state.table {
		return invalid("lookup requires its model's stored field")
	}
	if l.core.primary && l.core.state.table.primary != l.core.field.core {
		return invalid("keyed lookup requires its model's primary key")
	}
	if !l.core.primary && (l.core.state.ast.Limit.IsSet() || l.core.state.ast.Offset != 0) {
		return invalid("unique lookup requires an unpaginated model scope")
	}
	return validateQuery(l.core.state)
}

// Find performs one parameterized SELECT; only unique results receive eager loads.
// Example: lookup.Find(ctx,db,key).
func (l KeyLookup[M, K]) Find(ctx context.Context, ex database.Executor, key K) (value.Optional[M], error) {
	if err := l.Validate(); err != nil {
		return value.Optional[M]{}, err
	}
	q := where(l.core.state, []Predicate[M]{comparison[M](l.core.field, querycore.Equal, []K{key})})
	if l.core.primary {
		return first[M](ctx, ex, q)
	}
	return uniqueModel[M](ctx, ex, q)
}
func uniqueModel[M any](ctx context.Context, ex database.Executor, q queryState) (value.Optional[M], error) {
	if err := executionContext(ctx, ex); err != nil {
		return value.Optional[M]{}, err
	}
	q.ast.ScopeContext = ctx
	q.ast.Limit = value.Set(2)
	rows, err := allRows[M](ctx, ex, q)
	if err != nil {
		return value.Optional[M]{}, err
	}
	if len(rows) > 1 {
		return value.Optional[M]{}, fault.New(fault.Internal, "model lookup key has multiple matches in its scope")
	}
	if len(rows) == 0 {
		return value.Optional[M]{}, nil
	}
	if err = loadPlans(ctx, ex, unsafe.Pointer(unsafe.SliceData(rows)), len(rows), q.relations); err != nil {
		return value.Optional[M]{}, err
	}
	if err = ctx.Err(); err != nil {
		return value.Optional[M]{}, err
	}
	return value.Set(rows[0]), nil
}

// DirectRelation restricts scoped binding to direct relationships.
// Example: func bind[R orm.DirectRelation[Team,Project]](r R).
type DirectRelation[P, M any] interface {
	BelongsTo[P, M] | HasOne[P, M] | HasMany[P, M]
	AggregateSource[P, M]
}

// RelatedLookup retains the parent, target and alternate-key types.
// Example: orm.RelatedUnique(TeamRelations.Projects,Projects.Slug).
type RelatedLookup[P, M any, K comparable] struct {
	_      [0]*P
	_      [0]*M
	plan   *relationPlan
	lookup *keyLookupState[K]
}

// RelatedUnique scopes a unique lookup through a loaded parent without a fallback query.
// Example: orm.RelatedUnique(TeamRelations.Projects,Projects.Slug).Find(ctx,db,team,slug).
func RelatedUnique[P, M any, K comparable, R DirectRelation[P, M]](relation R, field KeyColumn[M, K]) RelatedLookup[P, M, K] {
	var p *relationPlan
	switch r := any(relation).(type) {
	case BelongsTo[P, M]:
		p = r.plan
	case HasOne[P, M]:
		p = r.plan
	case HasMany[P, M]:
		p = r.plan
	}
	if p == nil {
		return RelatedLookup[P, M, K]{}
	}
	return RelatedLookup[P, M, K]{plan: p, lookup: Unique(Query[M]{state: p.query}, field).core}
}

// Validate rejects non-direct plans and windowed target scopes. Example: lookup.Validate().
func (l RelatedLookup[P, M, K]) Validate() error {
	if err := validateRelation(l.plan, nil); err != nil {
		return err
	}
	if l.plan.join != nil || l.plan.morph != nil || l.plan.choice != 0 {
		return invalid("related lookup requires a direct relationship")
	}
	return (KeyLookup[M, K]{core: l.lookup}).Validate()
}

// Find retains the relationship predicate, target filters and ordinary hydration.
// Example: lookup.Find(ctx,db,team,slug).
func (l RelatedLookup[P, M, K]) Find(ctx context.Context, ex database.Executor, parent P, key K) (value.Optional[M], error) {
	if err := executionContext(ctx, ex); err != nil {
		return value.Optional[M]{}, err
	}
	if err := l.Validate(); err != nil {
		return value.Optional[M]{}, err
	}
	p := l.plan
	local := pivotColumn(p.slot.owner, p.local.Column)
	foreign := pivotColumn(p.target, p.foreign.Column)
	// Each call adds its parent predicate to a private copy of the retained scope.
	q := *l.lookup
	present := false
	err := callback.Isolated("model relationship key", func() error {
		raw, err := local.get(unsafe.Pointer(&parent))
		if err != nil || raw == nil {
			return err
		}
		stored, err := foreign.storedAssignment(raw)
		if err != nil {
			return err
		}
		q.state = where(q.state, []Predicate[M]{{owner: p.target, expression: querycore.Comparison{Operand: p.foreign, Operator: querycore.Equal, Values: []any{stored.Value}, Bind: func(any) (driver.Value, error) { return stored.Bind() }}}})
		present = true
		return nil
	})
	if err != nil {
		return value.Optional[M]{}, err
	}
	if !present {
		return value.Optional[M]{}, nil
	}
	return (KeyLookup[M, K]{core: &q}).Find(ctx, ex, key)
}

// Validate the concrete framework family before invoking a promoted private method.
func lookupColumn(field any) bool {
	v := reflect.ValueOf(field)
	if !v.IsValid() {
		return false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct || v.Type().PkgPath() != reflect.TypeFor[columnCore]().PkgPath() {
		return false
	}
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).Anonymous {
			return false
		}
	}
	return true
}

// Format redacts scope literals and model metadata. Example: fmt.Sprintf("%+v", lookup).
func (KeyLookup[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("unique model lookup")) }

// String redacts scope literals. Example: fmt.Sprint(lookup).
func (KeyLookup[M, K]) String() string { return "unique model lookup" }

// GoString redacts Go-syntax diagnostics. Example: fmt.Sprintf("%#v", lookup).
func (KeyLookup[M, K]) GoString() string { return "unique model lookup" }

// Format redacts parent and target scopes. Example: fmt.Sprintf("%+v", related).
func (RelatedLookup[P, M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("related model lookup"))
}

// String redacts parent and target scopes. Example: fmt.Sprint(related).
func (RelatedLookup[P, M, K]) String() string { return "related model lookup" }

// GoString redacts Go-syntax diagnostics. Example: fmt.Sprintf("%#v", related).
func (RelatedLookup[P, M, K]) GoString() string { return "related model lookup" }
