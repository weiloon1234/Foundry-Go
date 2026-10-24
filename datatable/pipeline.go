package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type prepared[S any] struct {
	page      orm.PageRequest
	condition condition[S]
	orders    []expr.ProjectionOrder[S]
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

func (d *tableDefinition[S, R, A]) prepare(request Request, config Config) (prepared[S], error) {
	var result prepared[S]
	page, err := d.metadata.validateRequest(request, config)
	if err != nil {
		return result, err
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
		for i, column := range d.metadata.info.Columns {
			if column.Searchable {
				c, err := d.columns[i].filter(column.SearchOperator, []string{request.Search})
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
		sorts = d.metadata.info.DefaultSort
	}
	for _, sort := range sorts {
		result.orders = append(result.orders, d.columns[d.metadata.byColumn[sort.Column]].order(sort.Direction))
	}
	result.orders = append(result.orders, d.stable...)
	return result, nil
}
func (d *tableDefinition[S, R, A]) buildFilter(filter Filter) (condition[S], error) {
	if len(filter.Children) == 0 {
		return d.filters[filter.Column](filter.Op, filter.Values)
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
	rows, count rows.Query[S, R]
}

func (d *tableDefinition[S, R, A]) scoped(ctx context.Context, subject A, action Action, p prepared[S]) (scopedSource[S, R], error) {
	if err := ctx.Err(); err != nil {
		return scopedSource[S, R]{}, err
	}
	if err := d.authorize(ctx, subject, action); err != nil {
		return scopedSource[S, R]{}, err
	}
	if err := ctx.Err(); err != nil {
		return scopedSource[S, R]{}, err
	}
	q, err := d.source(ctx, subject)
	if err != nil {
		return scopedSource[S, R]{}, err
	}
	filtered := rows.Having(q.Where(p.condition.where...), p.condition.having...)
	result := scopedSource[S, R]{rows: rows.ReorderUnwindowed(filtered, p.orders...), count: filtered}
	// Compiling the ordered result also rejects windowed/DISTINCT ON sources
	// before the unordered count can run.
	if err := rows.Preflight(ctx, result.rows); err != nil {
		return scopedSource[S, R]{}, err
	}
	if err := ctx.Err(); err != nil {
		return scopedSource[S, R]{}, err
	}
	return result, nil
}
