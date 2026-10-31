package datatable

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FilterPhase identifies whether declared predicates apply before or after grouping.
//
// Example: datatable.WherePhase
type FilterPhase string

const (
	WherePhase  FilterPhase = "where"
	HavingPhase FilterPhase = "having"
)

// FilterInfo describes the exact scalar codec and supported operations. The
// declaration's owner/value types are erased only for inspection metadata.
//
// Example: source.Description()
type FilterInfo struct {
	Phase     FilterPhase               `json:"phase"`
	Scalar    foundryhttp.URLScalarInfo `json:"scalar"`
	Nullable  bool                      `json:"nullable"`
	Operators []Operator                `json:"operators"`
}

// condition keeps AND-ed predicates of one or both phases. complement builds
// the null-aware negation "this condition is not TRUE": SQL NOT alone would
// also drop rows where a NULL column makes the condition unknown.
type condition[S any] struct {
	where      []expr.Predicate[S]
	having     []expr.HavingPredicate[S]
	complement func() condition[S]
}

func rowCondition[S any](p expr.Predicate[S]) condition[S] {
	return condition[S]{where: []expr.Predicate[S]{p}}
}
func groupCondition[S any](p expr.HavingPredicate[S]) condition[S] {
	return condition[S]{having: []expr.HavingPredicate[S]{p}}
}

// sqlNot is three-valued SQL NOT of one single-phase condition.
func (c condition[S]) sqlNot() condition[S] {
	if len(c.where) > 0 {
		return rowCondition(expr.Not(expr.And(c.where...)))
	}
	return groupCondition(expr.HavingNot(expr.HavingAnd(c.having...)))
}

// anyOf ORs single-phase conditions of the same phase.
func anyOf[S any](children []condition[S]) condition[S] {
	if len(children) > 0 && len(children[0].where) > 0 {
		parts := make([]expr.Predicate[S], len(children))
		for i, c := range children {
			parts[i] = expr.And(c.where...)
		}
		return rowCondition(expr.Or(parts...))
	}
	parts := make([]expr.HavingPredicate[S], len(children))
	for i, c := range children {
		parts[i] = expr.HavingAnd(c.having...)
	}
	return groupCondition(expr.HavingOr(parts...))
}

// allOf concatenates AND-ed predicates, preserving both phases.
func allOf[S any](children []condition[S]) condition[S] {
	var result condition[S]
	for _, c := range children {
		result.where = append(result.where, c.where...)
		result.having = append(result.having, c.having...)
	}
	return result
}

// leaf attaches the complement of one declared comparison. unknown is the
// IS NULL test of a nullable operand that can make c unknown; it is nil when c
// is never unknown (non-null operands, NULL tests and NULL-inclusive negations).
func leaf[S any](c condition[S], unknown *condition[S]) condition[S] {
	c.complement = func() condition[S] {
		negated := c.sqlNot()
		if unknown != nil {
			negated = anyOf([]condition[S]{negated, *unknown})
		}
		negated.complement = func() condition[S] { return c }
		return negated
	}
	return c
}

// FilterSource retains both SQL scope and column value, including nullability.
// Construct with Where/Having or their nullable counterparts. Constructors
// infer supported comparisons from the supplied typed field/expression. The
// SQL pattern operator `like` is discovered but disabled until AllowLike.
//
// Example: datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(name))
type FilterSource[S, V any] struct {
	_       [0]*V
	info    FilterInfo
	build   func(Operator, []string) (condition[S], error)
	pattern bool
	err     error
}

// Validate reports a missing or invalid filter declaration.
func (s FilterSource[S, V]) Validate() error {
	if s.err != nil {
		return s.err
	}
	if s.build == nil || len(s.info.Operators) == 0 {
		return invalid("filter source is not defined")
	}
	return nil
}

// Description returns an owned snapshot of the validated filter metadata.
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

// RowEquality requires exact typed equality and membership predicates in the row phase.
//
// Example: expr.TextFilter(name)
type RowEquality[S, V any] interface {
	// Eq compares equality within the declared row scope.
	Eq(V) expr.Predicate[S]
	// Ne compares inequality within the declared row scope.
	Ne(V) expr.Predicate[S]
	// In tests membership within the declared row scope.
	In(...V) expr.Predicate[S]
}

// RowNullable adds SQL null tests to exact typed row comparisons.
//
// Example: expr.NullableTextFilter(nickname)
type RowNullable[S, V any] interface {
	RowEquality[S, V]
	// IsNull tests SQL nullness within the declared row scope.
	IsNull() expr.Predicate[S]
	// IsNotNull tests SQL presence within the declared row scope.
	IsNotNull() expr.Predicate[S]
}

// GroupEquality requires exact typed equality and membership predicates in HAVING.
//
// Example: expr.OrderValueFilter(expr.Count[User]())
type GroupEquality[S, V any] interface {
	// Eq compares equality within the declared HAVING scope.
	Eq(V) expr.HavingPredicate[S]
	// Ne compares inequality within the declared HAVING scope.
	Ne(V) expr.HavingPredicate[S]
	// In tests membership within the declared HAVING scope.
	In(...V) expr.HavingPredicate[S]
}

// GroupNullable adds SQL null tests to exact typed HAVING comparisons.
//
// Example: expr.NullableOrderValueFilter(expr.Sum(orm.NumberExpr(Orders.Total)))
type GroupNullable[S, V any] interface {
	GroupEquality[S, V]
	// IsNull tests SQL nullness within the declared HAVING scope.
	IsNull() expr.HavingPredicate[S]
	// IsNotNull tests SQL presence within the declared HAVING scope.
	IsNotNull() expr.HavingPredicate[S]
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
	// A nullable operand makes ordinary comparisons unknown for NULL rows.
	// Negated comparisons include those rows, matching what "not equal" means
	// to a report user; SQL NOT alone would silently exclude them.
	var unknown *condition[S]
	var nulls nullComparisons[P]
	if nullable {
		var ok bool
		if nulls, ok = source.(nullComparisons[P]); !ok {
			result.err = invalid("nullable filter requires explicit null comparisons")
			return result
		}
		isNull := wrap(nulls.IsNull())
		unknown = &isNull
	}
	orNull := func(c condition[S]) condition[S] {
		if unknown == nil {
			return c
		}
		return anyOf([]condition[S]{c, *unknown})
	}
	operators := map[Operator]func([]V) condition[S]{
		Equal:    func(v []V) condition[S] { return leaf(wrap(source.Eq(v[0])), unknown) },
		NotEqual: func(v []V) condition[S] { return leaf(orNull(wrap(source.Ne(v[0]))), nil) },
		In:       func(v []V) condition[S] { return leaf(wrap(source.In(v...)), unknown) },
		NotIn:    func(v []V) condition[S] { return leaf(orNull(wrap(source.In(v...)).sqlNot()), nil) },
	}
	if ordered, ok := source.(ranges[V, P]); ok {
		operators[Less] = func(v []V) condition[S] { return leaf(wrap(ordered.Lt(v[0])), unknown) }
		operators[LessEqual] = func(v []V) condition[S] { return leaf(wrap(ordered.Lte(v[0])), unknown) }
		operators[Greater] = func(v []V) condition[S] { return leaf(wrap(ordered.Gt(v[0])), unknown) }
		operators[GreaterEqual] = func(v []V) condition[S] { return leaf(wrap(ordered.Gte(v[0])), unknown) }
		operators[Between] = func(v []V) condition[S] {
			return leaf(allOf([]condition[S]{wrap(ordered.Gte(v[0])), wrap(ordered.Lte(v[1]))}), unknown)
		}
	}
	if text, ok := source.(textComparisons[V, P]); ok {
		operators[Contains] = func(v []V) condition[S] { return leaf(wrap(text.Contains(v[0])), unknown) }
		operators[Like] = func(v []V) condition[S] { return leaf(wrap(text.Like(v[0])), unknown) }
		result.pattern = true
	}
	if text, ok := source.(interface{ IContains(V) P }); ok {
		operators[InsensitiveContains] = func(v []V) condition[S] { return leaf(wrap(text.IContains(v[0])), unknown) }
	}
	if nullable {
		operators[IsNull] = func([]V) condition[S] { return leaf(wrap(nulls.IsNull()), nil) }
		operators[IsNotNull] = func([]V) condition[S] { return leaf(wrap(nulls.IsNotNull()), nil) }
	}
	for op := range operators {
		if op != Like {
			result.info.Operators = append(result.info.Operators, op)
		}
	}
	slices.Sort(result.info.Operators)
	result.build = func(op Operator, raw []string) (condition[S], error) {
		build, ok := operators[op]
		if !ok {
			return condition[S]{}, requestInvalid("filter operator is not declared")
		}
		if err := filterArity(op, len(raw)); err != nil {
			return condition[S]{}, err
		}
		values := make([]V, len(raw))
		for i, text := range raw {
			v, err := codec.Parse(text)
			if err != nil {
				return condition[S]{}, requestInvalid("filter scalar is invalid")
			}
			values[i] = v
		}
		return build(values), nil
	}
	return result
}

// Where declares a non-null row filter with its typed transport codec and comparison adapter.
//
// Example: datatable.Where(codec, source)
func Where[S, V any](codec foundryhttp.QueryCodec[V], source RowEquality[S, V]) FilterSource[S, V] {
	return makeFilter(codec, source, WherePhase, false, rowCondition[S])
}

// Having declares a non-null HAVING filter with its typed transport codec and comparison adapter.
//
// Example: datatable.Having(codec, source)
func Having[S, V any](codec foundryhttp.QueryCodec[V], source GroupEquality[S, V]) FilterSource[S, V] {
	return makeFilter(codec, source, HavingPhase, false, groupCondition[S])
}

// NullableWhere declares a nullable row filter whose codec parses present values.
//
// Example: datatable.NullableWhere(codec, source)
func NullableWhere[S, V any](codec foundryhttp.QueryCodec[V], source RowNullable[S, V]) FilterSource[S, value.Nullable[V]] {
	base := makeFilter(codec, source, WherePhase, true, rowCondition[S])
	return FilterSource[S, value.Nullable[V]]{info: base.info, build: base.build, pattern: base.pattern, err: base.err}
}

// NullableHaving declares a nullable HAVING filter whose codec parses present values.
//
// Example: datatable.NullableHaving(codec, source)
func NullableHaving[S, V any](codec foundryhttp.QueryCodec[V], source GroupNullable[S, V]) FilterSource[S, value.Nullable[V]] {
	base := makeFilter(codec, source, HavingPhase, true, groupCondition[S])
	return FilterSource[S, value.Nullable[V]]{info: base.info, build: base.build, pattern: base.pattern, err: base.err}
}

// Restrict narrows the enabled comparison allowlist without changing codecs.
// Call AllowLike first when the restricted set includes `like`.
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
	s.info.Operators = allowed
	return s
}

// AllowLike explicitly enables `like`, whose value is a SQL LIKE pattern: `%`
// and `_` are wildcards chosen by the client. It is off by default because a
// client pattern can defeat indexes. `contains`/`icontains` keep literal text.
func (s FilterSource[S, V]) AllowLike() FilterSource[S, V] {
	if s.Validate() != nil || slices.Contains(s.info.Operators, Like) {
		return s
	}
	if !s.pattern {
		s.err = invalid("filter source does not support like patterns")
		return s
	}
	s.info.Operators = append(slices.Clone(s.info.Operators), Like)
	slices.Sort(s.info.Operators)
	return s
}

// enabled returns the builder restricted to the declared allowlist. Request
// validation checks the same list before any scalar codec runs.
func (s FilterSource[S, V]) enabled() func(Operator, []string) (condition[S], error) {
	if s.build == nil {
		return nil
	}
	allowed, build := slices.Clone(s.info.Operators), s.build
	return func(op Operator, values []string) (condition[S], error) {
		if !slices.Contains(allowed, op) {
			return condition[S]{}, requestInvalid("filter operator is not declared")
		}
		return build(op, values)
	}
}

// Related lifts a declared child filter through a typed relation predicate, such
// as child.Where(predicate).Exists(). It cannot lift HAVING into a row filter.
// The callback is server-owned; the table operation owns its isolation/lifetime.
// The lifted comparison is never unknown, so its negation is NOT EXISTS.
//
// Example: datatable.Related(source, exists)
func Related[S, C, V any](source FilterSource[C, V], exists func(expr.Predicate[C]) expr.Predicate[S]) FilterSource[S, V] {
	result := FilterSource[S, V]{info: source.info, err: source.Validate()}
	if result.err != nil {
		return result
	}
	if exists == nil || source.info.Phase != WherePhase {
		result.err = invalid("relation filter requires a row predicate mapping")
		return result
	}
	child := source.enabled()
	result.build = func(op Operator, values []string) (condition[S], error) {
		c, err := child(op, values)
		if err != nil {
			return condition[S]{}, err
		}
		return leaf(rowCondition(exists(expr.And(c.where...))), nil), nil
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
		return requestInvalid("invalid filter value count")
	}
	if n != want {
		return requestInvalid("invalid filter value count")
	}
	return nil
}
