package datatable

import (
	"context"
	"slices"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/database/query"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type prepared[S any] struct {
	page      query.PageRequest
	condition condition[S]
	orders    []query.ProjectionOrder[S]
}

func (d *tableDefinition[S, R, A]) validateSort(sorts []Sort) error {
	return validateSortColumns(sorts, func(name string) bool { column, ok := d.byColumn[name]; return ok && column.order != nil })
}

func validateSortColumns(sorts []Sort, sortable func(string) bool) error {
	if len(sorts) > MaxSorts {
		return invalid("too many datatable sorts")
	}
	seen := make(map[string]bool, len(sorts))
	for _, sort := range sorts {
		if !sortable(sort.Column) || seen[sort.Column] || (sort.Direction != Ascending && sort.Direction != Descending) {
			return invalid("invalid or undeclared datatable sort")
		}
		seen[sort.Column] = true
	}
	return nil
}

type requestBudget struct{ nodes, bytes int }

func (b *requestBudget) text(text string, maximum int) error {
	if len(text) > maximum || !utf8.ValidString(text) || len(text) > MaxRequestBytes-b.bytes {
		return requestInvalid("datatable request exceeds its text bounds")
	}
	b.bytes += len(text)
	return nil
}
func (d *tableDefinition[S, R, A]) prepare(request Request, config Config) (prepared[S], error) {
	var result prepared[S]
	page, err := request.page()
	if err != nil {
		return result, err
	}
	if page.Size > config.MaxPageSize || (page.Number-1) > config.MaxOffset/page.Size {
		return result, requestInvalid("datatable page exceeds its configured bound")
	}
	if err := d.validateSort(request.Sort); err != nil {
		return result, foundryhttp.BadRequest.WithCause(err)
	}
	budget := requestBudget{}
	for _, sort := range request.Sort {
		if err := budget.text(sort.Column, 128); err != nil {
			return result, err
		}
	}
	if err := budget.text(request.Search, MaxScalarBytes); err != nil {
		return result, err
	}
	// Complete structural validation precedes all scalar/extension callbacks.
	for _, filter := range request.Filters {
		if err := d.validateFilter(filter, 0, &budget); err != nil {
			return result, err
		}
	}
	result.page = page
	for _, filter := range request.Filters {
		c, err := d.buildFilter(filter)
		if err != nil {
			return prepared[S]{}, err
		}
		result.condition.where = append(result.condition.where, c.where...)
		result.condition.having = append(result.condition.having, c.having...)
	}
	if request.Search != "" {
		var children []condition[S]
		for _, column := range d.columns {
			if column.searchable {
				c, err := column.filter.build(searchOperator(column.filter.info), []string{request.Search})
				if err != nil {
					return prepared[S]{}, err
				}
				children = append(children, c)
			}
		}
		if len(children) == 0 {
			return prepared[S]{}, requestInvalid("table does not declare searchable columns")
		}
		c, err := combineConditions(Any, children)
		if err != nil {
			return prepared[S]{}, err
		}
		result.condition.where = append(result.condition.where, c.where...)
		result.condition.having = append(result.condition.having, c.having...)
	}
	sorts := request.Sort
	if len(sorts) == 0 {
		sorts = d.spec.DefaultSort
	}
	for _, sort := range sorts {
		result.orders = append(result.orders, d.byColumn[sort.Column].order(sort.Direction))
	}
	result.orders = append(result.orders, d.spec.Stable...)
	return result, nil
}
func (d *tableDefinition[S, R, A]) validateFilter(filter Filter, depth int, budget *requestBudget) error {
	if depth > MaxFilterDepth || budget.nodes >= MaxFilters {
		return requestInvalid("datatable filter tree exceeds its structural bound")
	}
	budget.nodes++
	if err := budget.text(filter.Column, 128); err != nil {
		return err
	}
	if len(filter.Values) > MaxFilterValues {
		return requestInvalid("too many filter values")
	}
	for _, text := range filter.Values {
		if err := budget.text(text, MaxScalarBytes); err != nil {
			return err
		}
	}
	switch filter.Op {
	case All, Any, Not:
		if filter.Column != "" || len(filter.Values) != 0 || len(filter.Children) == 0 || filter.Op == Not && len(filter.Children) != 1 {
			return requestInvalid("invalid datatable filter group")
		}
		for _, child := range filter.Children {
			if err := d.validateFilter(child, depth+1, budget); err != nil {
				return err
			}
		}
	default:
		declaration, ok := d.filters[filter.Column]
		if !ok || len(filter.Children) != 0 || !slices.Contains(declaration.info.Operators, filter.Op) {
			return requestInvalid("invalid or undeclared datatable filter")
		}
		return filterArity(filter.Op, len(filter.Values))
	}
	return nil
}
func (d *tableDefinition[S, R, A]) buildFilter(filter Filter) (condition[S], error) {
	if len(filter.Children) == 0 {
		return d.filters[filter.Column].build(filter.Op, filter.Values)
	}
	children := make([]condition[S], 0, len(filter.Children))
	for _, child := range filter.Children {
		c, err := d.buildFilter(child)
		if err != nil {
			return condition[S]{}, err
		}
		children = append(children, c)
	}
	return combineConditions(filter.Op, children)
}

// combineConditions builds AND/OR/NOT groups. NOT uses the null-aware
// complement of its child, so rows where a NULL operand makes the child unknown
// are included: De Morgan pushes the negation down to each declared comparison.
func combineConditions[S any](op Operator, children []condition[S]) (condition[S], error) {
	result := allOf(children)
	if op == All {
		result.complement = func() condition[S] { return negateEach(children, anyOf[S], result) }
		return result, nil
	}
	if len(result.where) != 0 && len(result.having) != 0 {
		return condition[S]{}, requestInvalid("OR and NOT cannot mix WHERE and HAVING phases")
	}
	if op == Not {
		if len(children) != 1 || children[0].complement == nil {
			return condition[S]{}, requestInvalid("invalid datatable filter group")
		}
		return children[0].complement(), nil
	}
	result = anyOf(children)
	result.complement = func() condition[S] { return negateEach(children, allOf[S], result) }
	return result, nil
}

// negateEach combines the complements of children and restores original as the
// complement of that result. NOT validates single-phase input before calling it.
func negateEach[S any](children []condition[S], combine func([]condition[S]) condition[S], original condition[S]) condition[S] {
	complements := make([]condition[S], len(children))
	for i, child := range children {
		complements[i] = child.complement()
	}
	negated := combine(complements)
	negated.complement = func() condition[S] { return original }
	return negated
}

// scopedSource is the authorized, filtered source. count omits the table's
// ordering: sorting cannot change a total and only costs the database work.
type scopedSource[S, R any] struct {
	rows, count query.ProjectionQuery[S, R]
}

func (d *tableDefinition[S, R, A]) scoped(ctx context.Context, subject A, action Action, p prepared[S]) (scopedSource[S, R], error) {
	if err := ctx.Err(); err != nil {
		return scopedSource[S, R]{}, err
	}
	if err := d.spec.Authorize(ctx, subject, action); err != nil {
		return scopedSource[S, R]{}, err
	}
	if err := ctx.Err(); err != nil {
		return scopedSource[S, R]{}, err
	}
	q, err := d.spec.Source(ctx, subject)
	if err != nil {
		return scopedSource[S, R]{}, err
	}
	filtered := q.Where(p.condition.where...).Having(p.condition.having...)
	result := scopedSource[S, R]{rows: filtered.ReorderUnwindowed(p.orders...), count: filtered}
	// Compiling the ordered result also rejects windowed/DISTINCT ON sources
	// before the unordered count can run.
	if _, err := result.rows.Compile(); err != nil {
		return scopedSource[S, R]{}, err
	}
	if err := ctx.Err(); err != nil {
		return scopedSource[S, R]{}, err
	}
	return result, nil
}
