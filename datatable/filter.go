package datatable

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type FilterPhase string

const (
	WherePhase  FilterPhase = "where"
	HavingPhase FilterPhase = "having"
)

// FilterInfo describes the exact scalar codec and supported operations. The
// declaration's owner/value types are erased only for inspection metadata.
type FilterInfo struct {
	Phase     FilterPhase               `json:"phase"`
	Scalar    foundryhttp.URLScalarInfo `json:"scalar"`
	Nullable  bool                      `json:"nullable"`
	Operators []Operator                `json:"operators"`
}

type condition[S any] struct {
	where  []query.Predicate[S]
	having []query.HavingPredicate[S]
}

func rowCondition[S any](p query.Predicate[S]) condition[S] {
	return condition[S]{where: []query.Predicate[S]{p}}
}
func groupCondition[S any](p query.HavingPredicate[S]) condition[S] {
	return condition[S]{having: []query.HavingPredicate[S]{p}}
}
func (c condition[S]) not() condition[S] {
	if len(c.where) > 0 {
		return rowCondition(query.And(c.where...).Not())
	}
	return groupCondition(query.HavingAnd(c.having...).Not())
}

// FilterSource retains both SQL scope and column value, including nullability.
// Construct with Where/Having or their nullable counterparts. Constructors
// infer supported comparisons from the supplied typed field/expression.
type FilterSource[S, V any] struct {
	_     [0]*V
	info  FilterInfo
	build func(Operator, []string) (condition[S], error)
	err   error
}

func (s FilterSource[S, V]) Validate() error {
	if s.err != nil {
		return s.err
	}
	if s.build == nil || len(s.info.Operators) == 0 {
		return invalid("filter source is not defined")
	}
	return nil
}
func (s FilterSource[S, V]) Description() (FilterInfo, error) {
	if err := s.Validate(); err != nil {
		return FilterInfo{}, err
	}
	result := s.info
	result.Operators = slices.Clone(result.Operators)
	// Scalar metadata is copied once more through the existing descriptor boundary.
	scalar, err := cloneScalar(result.Scalar)
	result.Scalar = scalar
	return result, err
}

type RowEquality[S, V any] interface {
	Eq(V) query.Predicate[S]
	Ne(V) query.Predicate[S]
	In(...V) query.Predicate[S]
}
type RowNullable[S, V any] interface {
	RowEquality[S, V]
	IsNull() query.Predicate[S]
	IsNotNull() query.Predicate[S]
}
type GroupEquality[S, V any] interface {
	Eq(V) query.HavingPredicate[S]
	Ne(V) query.HavingPredicate[S]
	In(...V) query.HavingPredicate[S]
}
type GroupNullable[S, V any] interface {
	GroupEquality[S, V]
	IsNull() query.HavingPredicate[S]
	IsNotNull() query.HavingPredicate[S]
}

type comparisons[V, P any] interface {
	Eq(V) P
	Ne(V) P
	In(...V) P
}
type ranges[V, P any] interface {
	Lt(V) P
	Lte(V) P
	Gt(V) P
	Gte(V) P
}
type textComparisons[V, P any] interface {
	Contains(V) P
	Like(V) P
}
type nullComparisons[P any] interface {
	IsNull() P
	IsNotNull() P
}

func makeFilter[S, V, P any](codec foundryhttp.QueryCodec[V], source comparisons[V, P], phase FilterPhase, nullable bool, wrap func(P) condition[S]) FilterSource[S, V] {
	result := FilterSource[S, V]{info: FilterInfo{Phase: phase, Nullable: nullable}}
	scalar, err := foundryhttp.DescribePathCodec(codec)
	if err != nil || nilValue(source) {
		result.err = invalid("filter requires a described scalar codec and typed comparisons")
		return result
	}
	result.info.Scalar = scalar
	operators := map[Operator]func([]V) condition[S]{
		Equal:    func(v []V) condition[S] { return wrap(source.Eq(v[0])) },
		NotEqual: func(v []V) condition[S] { return wrap(source.Ne(v[0])) },
		In:       func(v []V) condition[S] { return wrap(source.In(v...)) },
		NotIn:    func(v []V) condition[S] { return wrap(source.In(v...)).not() },
	}
	if ordered, ok := source.(ranges[V, P]); ok {
		operators[Less] = func(v []V) condition[S] { return wrap(ordered.Lt(v[0])) }
		operators[LessEqual] = func(v []V) condition[S] { return wrap(ordered.Lte(v[0])) }
		operators[Greater] = func(v []V) condition[S] { return wrap(ordered.Gt(v[0])) }
		operators[GreaterEqual] = func(v []V) condition[S] { return wrap(ordered.Gte(v[0])) }
		operators[Between] = func(v []V) condition[S] {
			a, b := wrap(ordered.Gte(v[0])), wrap(ordered.Lte(v[1]))
			return condition[S]{where: append(a.where, b.where...), having: append(a.having, b.having...)}
		}
	}
	if text, ok := source.(textComparisons[V, P]); ok {
		operators[Contains] = func(v []V) condition[S] { return wrap(text.Contains(v[0])) }
		operators[Like] = func(v []V) condition[S] { return wrap(text.Like(v[0])) }
	}
	if text, ok := source.(interface{ IContains(V) P }); ok {
		operators[InsensitiveContains] = func(v []V) condition[S] { return wrap(text.IContains(v[0])) }
	}
	if nullable {
		nulls, ok := source.(nullComparisons[P])
		if !ok {
			result.err = invalid("nullable filter requires explicit null comparisons")
			return result
		}
		operators[IsNull] = func([]V) condition[S] { return wrap(nulls.IsNull()) }
		operators[IsNotNull] = func([]V) condition[S] { return wrap(nulls.IsNotNull()) }
	}
	for op := range operators {
		result.info.Operators = append(result.info.Operators, op)
	}
	slices.Sort(result.info.Operators)
	result.build = func(op Operator, raw []string) (condition[S], error) {
		build, ok := operators[op]
		if !ok {
			return condition[S]{}, invalid("filter operator is not declared")
		}
		if err := filterArity(op, len(raw)); err != nil {
			return condition[S]{}, err
		}
		values := make([]V, len(raw))
		for i, text := range raw {
			v, err := codec.Parse(text)
			if err != nil {
				return condition[S]{}, invalid("filter scalar is invalid")
			}
			values[i] = v
		}
		return build(values), nil
	}
	return result
}
func Where[S, V any](codec foundryhttp.QueryCodec[V], source RowEquality[S, V]) FilterSource[S, V] {
	return makeFilter(codec, source, WherePhase, false, rowCondition[S])
}
func Having[S, V any](codec foundryhttp.QueryCodec[V], source GroupEquality[S, V]) FilterSource[S, V] {
	return makeFilter(codec, source, HavingPhase, false, groupCondition[S])
}
func NullableWhere[S, V any](codec foundryhttp.QueryCodec[V], source RowNullable[S, V]) FilterSource[S, value.Nullable[V]] {
	base := makeFilter(codec, source, WherePhase, true, rowCondition[S])
	return FilterSource[S, value.Nullable[V]]{info: base.info, build: base.build, err: base.err}
}
func NullableHaving[S, V any](codec foundryhttp.QueryCodec[V], source GroupNullable[S, V]) FilterSource[S, value.Nullable[V]] {
	base := makeFilter(codec, source, HavingPhase, true, groupCondition[S])
	return FilterSource[S, value.Nullable[V]]{info: base.info, build: base.build, err: base.err}
}

// Restrict narrows the discovered comparison allowlist without changing codecs.
func (s FilterSource[S, V]) Restrict(operators ...Operator) FilterSource[S, V] {
	if s.Validate() != nil {
		return s
	}
	allowed := slices.Clone(operators)
	slices.Sort(allowed)
	if len(allowed) == 0 {
		s.err = invalid("filter requires at least one allowed operator")
		return s
	}
	for i, op := range allowed {
		if !slices.Contains(s.info.Operators, op) || i > 0 && allowed[i-1] == op {
			s.err = invalid("invalid filter operator restriction")
			return s
		}
	}
	original := s.build
	s.info.Operators = allowed
	s.build = func(op Operator, values []string) (condition[S], error) {
		if !slices.Contains(allowed, op) {
			return condition[S]{}, invalid("filter operator is not declared")
		}
		return original(op, values)
	}
	return s
}

// Related lifts a declared child filter through a typed relation predicate, such
// as child.Where(predicate).Exists(). It cannot lift HAVING into a row filter.
// The callback is server-owned; the table operation owns its isolation/lifetime.
func Related[S, C, V any](source FilterSource[C, V], exists func(query.Predicate[C]) query.Predicate[S]) FilterSource[S, V] {
	result := FilterSource[S, V]{info: source.info, err: source.Validate()}
	if result.err != nil {
		return result
	}
	if exists == nil || source.info.Phase != WherePhase {
		result.err = invalid("relation filter requires a row predicate mapping")
		return result
	}
	result.build = func(op Operator, values []string) (condition[S], error) {
		child, err := source.build(op, values)
		if err != nil {
			return condition[S]{}, err
		}
		return rowCondition(exists(query.And(child.where...))), nil
	}
	return result
}

func filterArity(op Operator, n int) error {
	want := 1
	switch op {
	case IsNull, IsNotNull:
		want = 0
	case Between:
		want = 2
	case In, NotIn:
		if n >= 1 && n <= MaxFilterValues {
			return nil
		}
		return invalid("invalid filter value count")
	}
	if n != want {
		return invalid("invalid filter value count")
	}
	return nil
}
