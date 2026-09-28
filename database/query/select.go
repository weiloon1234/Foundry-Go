package query

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/value"
)

// All read paths share this relational SELECT node and compiler. Public typed
// declarations resolve model ownership before entering this private boundary.
type tableSource struct {
	table, alias string
	columns      []Column
	query        *selectNode
	cte          *cteNode
	set          *setNode
	self         *recursiveReference
}

func (s tableSource) name() string {
	if s.alias != "" {
		return s.alias
	}
	if s.cte != nil {
		return s.cte.name
	}
	if s.self != nil {
		return s.self.name
	}
	return s.table
}
func (c *compiler) sourceSQL(s tableSource) (string, error) {
	if s.self != nil {
		if c.self != s.self {
			return "", fault.New(fault.Invalid, "recursive reference is outside its owning step")
		}
		text := quoted(s.self.name)
		if s.alias != "" {
			text += " AS " + quoted(s.alias)
		}
		return text, nil
	}
	if s.set != nil {
		text, err := c.setSQL(*s.set, s.columns)
		return "(" + text + ") AS " + quoted(s.alias), err
	}
	if s.query != nil {
		text, err := c.selectSQL(*s.query)
		return "(" + text + ") AS " + quoted(s.alias), err
	}
	if s.cte != nil {
		if c.ctes[s.cte.name] != s.cte {
			return "", fault.New(fault.Invalid, "CTE reference is not available in this statement")
		}
		text := quoted(s.cte.name)
		if s.alias != "" {
			text += " AS " + quoted(s.alias)
		}
		return text, nil
	}
	text := quotedTable(s.table)
	if s.alias != "" {
		text += " AS " + quoted(s.alias)
	}
	return text, nil
}

type joinKind uint8

const (
	innerJoin joinKind = iota
	leftJoin
	rightJoin
	fullJoin
	crossJoin
)

type joinNode struct {
	source  tableSource
	on      expression
	kind    joinKind
	lateral *scopeRequirement
}
type orderNode struct {
	expression valueExpression
	descending bool
}
type selectNode struct {
	locks      []lockSpec
	source     tableSource
	selections []selectItem // Empty selects the constant 1, used by count/exists.
	distinct   distinctSpec
	groupBy    []valueExpression
	joins      []joinNode
	predicates []expression
	having     []expression
	orders     []orderNode
	limit      value.Optional[int]
	offset     int
}

func orderNodes[M any](orders []Order[M]) []orderNode {
	result := make([]orderNode, len(orders))
	for i, o := range orders {
		result[i] = orderNode{o.value(), o.descending}
	}
	return result
}

func (c *compiler) selectSQL(s selectNode) (string, error) {
	return c.selectSQLWithOuter(s, nil)
}
func (c *compiler) selectSQLWithOuter(s selectNode, outer *outerScope) (string, error) {
	if len(s.locks) > 0 && !c.allowLocks {
		return "", fault.New(fault.Invalid, "row locks require a transaction-scoped query")
	}
	c.selectDepth++
	c.selectNodes++
	defer func() { c.selectDepth-- }()
	if c.selectDepth > MaxExpressionDepth || c.selectNodes > MaxExpressionNodes {
		return "", fault.New(fault.Invalid, "nested SELECT exceeds its resource bound")
	}
	if len(s.joins) > MaxExpressionNodes || len(s.selections) > MaxExpressionNodes || len(s.orders) > MaxExpressionNodes || len(s.groupBy) > MaxExpressionNodes {
		return "", fault.New(fault.Invalid, "select exceeds its resource bound")
	}
	if limit, set := s.limit.Get(); (set && limit < 0) || s.offset < 0 {
		return "", fault.New(fault.Invalid, "select has a negative window")
	}
	previousSources, previousOuter, previousSelf := c.sources, c.outer, c.directSelf
	defer func() { c.sources, c.outer, c.directSelf = previousSources, previousOuter, previousSelf }()
	previousWindow := c.inWindow
	c.inWindow = false
	defer func() { c.inWindow = previousWindow }()
	previousKeys := c.keys
	c.keys = nil
	defer func() { c.keys = previousKeys }()
	c.outer = outer
	c.directSelf = s.source.self != nil
	for _, join := range s.joins {
		c.directSelf = c.directSelf || join.source.self != nil
	}
	c.sources = make(map[string]map[string]Column, len(s.joins)+1)
	add := func(source tableSource) error {
		if c.outer != nil && c.outer.sources[source.name()] != nil {
			return fault.New(fault.Invalid, "inner SELECT shadows a correlated outer source")
		}
		validSource := source.query == nil && source.cte == nil && source.set == nil && source.self == nil && sqlname.Table(source.table)
		if source.query != nil {
			validSource = source.cte == nil && source.set == nil && source.self == nil && source.table == "" && sqlname.Valid(source.alias)
		}
		if source.cte != nil {
			validSource = source.query == nil && source.set == nil && source.self == nil && source.table == "" && sqlname.Valid(source.cte.name) && c.ctes[source.cte.name] == source.cte
			if len(source.columns) != len(source.cte.columns) {
				validSource = false
			} else {
				for i, column := range source.columns {
					if column != source.cte.columns[i] {
						validSource = false
						break
					}
				}
			}
		}
		if source.set != nil {
			validSource = source.query == nil && source.cte == nil && source.self == nil && source.table == "" && sqlname.Valid(source.alias)
		}
		if source.self != nil {
			validSource = source.query == nil && source.cte == nil && source.set == nil && source.table == "" && source.self == c.self && sqlname.Valid(source.self.name) && slices.Equal(source.columns, source.self.columns)
		}
		if !validSource || (source.alias != "" && !sqlname.Valid(source.alias)) || c.sources[source.name()] != nil {
			return fault.New(fault.Invalid, "invalid or repeated select source")
		}
		columns := make(map[string]Column, len(source.columns))
		for _, column := range source.columns {
			if !sqlname.Valid(column.Name) {
				return fault.New(fault.Invalid, "invalid select column")
			}
			if _, exists := columns[column.Name]; exists {
				return fault.New(fault.Invalid, "repeated select column")
			}
			columns[column.Name] = column
		}
		c.sources[source.name()] = columns
		return nil
	}
	if err := add(s.source); err != nil {
		return "", err
	}
	nodes := &c.expressionNodes
	for _, join := range s.joins {
		if join.kind > crossJoin {
			return "", fault.New(fault.Invalid, "invalid join kind")
		}
		if join.lateral != nil {
			if join.source.query == nil || join.kind == rightJoin || join.kind == fullJoin {
				return "", fault.New(fault.Invalid, "correlated lateral source requires an inner, left or cross derived join")
			}
			// Validate before adding this source: neither it nor a later source
			// may satisfy its own preceding-input requirement.
			if _, err := c.correlationScope(join.lateral, nil, false); err != nil {
				return "", err
			}
		}
		if err := add(join.source); err != nil {
			return "", err
		}
		if join.kind == crossJoin {
			if join.on != nil {
				return "", fault.New(fault.Invalid, "cross join cannot have an ON condition")
			}
			continue
		}
		// A join cannot refer to a source introduced by a later join.
		if join.on != nil || join.lateral == nil {
			if err := validateExpressionFields(join.on, c.declaredField, 0, nodes); err != nil {
				return "", err
			}
		}
	}
	for _, p := range s.predicates {
		if err := validateExpressionFields(p, c.declaredField, 0, nodes); err != nil {
			return "", err
		}
	}
	windows, err := orderedWindows(s)
	if err != nil {
		return "", err
	}
	grouped, err := c.groupKeys(s.groupBy)
	if err != nil {
		return "", err
	}
	// HAVING creates an implicit single group even without GROUP BY or selected
	// aggregates. An aggregate ORDER BY also makes this an aggregate query.
	grouping := len(s.groupBy) > 0 || len(s.having) > 0
	for _, item := range s.selections {
		if groupsSelect(item.expression) {
			grouping = true
		}
	}
	for _, order := range s.orders {
		if groupsSelect(order.expression) {
			grouping = true
		}
	}
	for _, key := range s.distinct.keys {
		if groupsSelect(key) {
			grouping = true
		}
	}
	for _, p := range s.having {
		if err := validateExpressionValues(p, func(v valueExpression) error {
			return c.validateHavingValue(v, grouped)
		}, 0, nodes); err != nil {
			return "", err
		}
	}
	names := make([]string, len(s.selections))
	distinct, err := c.distinctSQL(s, grouped, grouping)
	if err != nil {
		return "", err
	}
	var distinctOrdering distinctOrder
	aliases := make(map[string]bool)
	for i, item := range s.selections {
		if s.distinct.kind == distinctRows {
			plan, err := c.planValue(item.expression)
			if err != nil {
				return "", err
			}
			distinctOrdering.add(plan, i+1)
		}
		text, err := c.selectedExpression(item.expression, grouped, grouping)
		if err != nil {
			return "", err
		}
		if item.alias != "" {
			if !sqlname.Valid(item.alias) || aliases[item.alias] {
				return "", fault.New(fault.Invalid, "invalid or repeated selection alias")
			}
			aliases[item.alias] = true
			text += " AS " + quoted(item.alias)
		}
		names[i] = text
	}
	selection := "1"
	if len(names) > 0 {
		selection = strings.Join(names, ", ")
	}
	var sql strings.Builder
	from, err := c.sourceSQL(s.source)
	if err != nil {
		return "", err
	}
	sql.WriteString("SELECT " + distinct + selection + " FROM " + from)
	allSources := c.sources
	available := map[string]map[string]Column{s.source.name(): allSources[s.source.name()]}
	for _, join := range s.joins {
		c.sources = available
		from, err := c.joinSourceSQL(join)
		if err != nil {
			return "", err
		}
		available[join.source.name()] = allSources[join.source.name()]
		if join.kind == crossJoin {
			sql.WriteString(" CROSS JOIN " + from)
			continue
		}
		on := "TRUE"
		if join.on != nil {
			on, err = c.expression(join.on)
			if err != nil {
				return "", err
			}
		}
		name := [...]string{" INNER JOIN ", " LEFT JOIN ", " RIGHT JOIN ", " FULL JOIN "}[join.kind]
		sql.WriteString(name + from + " ON " + on)
	}
	c.sources = allSources
	if err := c.where(&sql, s.predicates); err != nil {
		return "", err
	}
	for i, f := range s.groupBy {
		text, err := c.selectedExpression(f, grouped, false)
		if err != nil {
			return "", err
		}
		if i == 0 {
			sql.WriteString(" GROUP BY ")
		} else {
			sql.WriteString(", ")
		}
		sql.WriteString(text)
	}
	if err := c.conditionClauseAt(&sql, "HAVING", s.having, grouped, true); err != nil {
		return "", err
	}
	windowSQL, err := c.namedWindowsSQL(windows, grouped, grouping)
	if err != nil {
		return "", err
	}
	sql.WriteString(windowSQL)
	for i, order := range s.orders {
		var text string
		var err error
		if s.distinct.kind == distinctRows {
			text, err = distinctOrdering.compile(c, order, grouped, grouping)
		} else {
			text, err = c.selectedExpression(order.expression, grouped, grouping)
		}
		if err != nil {
			return "", err
		}
		if i == 0 {
			sql.WriteString(" ORDER BY ")
		} else {
			sql.WriteString(", ")
		}
		sql.WriteString(text)
		if order.descending {
			sql.WriteString(" DESC")
		} else {
			sql.WriteString(" ASC")
		}
	}
	if limit, set := s.limit.Get(); set {
		p, err := c.parameter(int64(limit))
		if err != nil {
			return "", err
		}
		sql.WriteString(" LIMIT " + p)
	}
	if s.offset != 0 {
		p, err := c.parameter(int64(s.offset))
		if err != nil {
			return "", err
		}
		sql.WriteString(" OFFSET " + p)
	}
	if len(s.locks) > 0 {
		suffix, err := c.compileLocks(s, s.locks)
		if err != nil {
			return "", err
		}
		sql.WriteString(suffix)
	}
	return sql.String(), nil
}

func (c *compiler) declaredField(f fieldRef) error {
	columns := c.columns
	if c.sources != nil {
		columns = c.sources[f.table]
		if columns == nil && c.outer != nil {
			c.observeField(c.selectDepth, f)
			return c.outer.field(f)
		}
	}
	if _, ok := columns[f.column]; !ok {
		return fault.New(fault.Invalid, "query uses an undeclared model column or source")
	}
	if err := f.validate(f.table); err != nil {
		return err
	}
	c.observeField(c.selectDepth, f)
	return nil
}

func (c *compiler) observeField(level int, f fieldRef) {
	if c.fieldObserver != nil {
		c.fieldObserver(level, f)
	}
}

// requalify is used only after the original model scope has been validated.
// It copies expression containers; the caller's predicates remain unchanged.
func requalify(e expression, alias string) expression {
	switch e := e.(type) {
	case comparison:
		e.operand = requalifyValue(e.operand, alias)
		return e
	case subqueryPredicate:
		e.query = requalifyCorrelation(e.query, alias)
		if e.operand != nil {
			e.operand = requalifyValue(e.operand, alias)
		}
		return e
	case binaryComparison:
		e.left, e.right = requalifyValue(e.left, alias), requalifyValue(e.right, alias)
		return e
	case junction:
		children := make([]expression, len(e.children))
		for i, child := range e.children {
			children[i] = requalify(child, alias)
		}
		e.children = children
		return e
	case negation:
		return negation{requalify(e.child, alias)}
	default:
		return e
	}
}

func requalifyValue(v valueExpression, alias string) valueExpression {
	if mapped, ok := mapComputedValue(v, func(v valueExpression) valueExpression { return requalifyValue(v, alias) }, func(p expression) expression { return requalify(p, alias) }); ok {
		return mapped
	}
	switch v := v.(type) {
	case fieldRef:
		v.table = alias
		return v
	case aggregateNode:
		return requalifyAggregate(v, alias)
	case scalarSubquery:
		v.query = requalifyCorrelation(v.query, alias)
		return v
	default:
		return v
	}
}
