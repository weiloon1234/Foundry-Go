package querycore

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/value"
)

// All read paths share this relational SELECT node and compiler. Public typed
// declarations resolve model ownership before entering this private boundary.
type TableSource struct {
	Table, Alias string
	Columns      []Column
	Query        *SelectNode
	Cte          *CteNode
	Set          *SetNode
	Self         *RecursiveReference
}

func (s TableSource) Name() string {
	if s.Alias != "" {
		return s.Alias
	}
	if s.Cte != nil {
		return s.Cte.Name
	}
	if s.Self != nil {
		return s.Self.Name
	}
	return s.Table
}

func (c *Compiler) SourceSQL(s TableSource) (string, error) {
	if s.Self != nil {
		if c.Self != s.Self {
			return "", fault.New(fault.Invalid, "recursive reference is outside its owning step")
		}
		text := Quoted(s.Self.Name)
		if s.Alias != "" {
			text += " AS " + Quoted(s.Alias)
		}
		return text, nil
	}
	if s.Set != nil {
		text, err := c.SetSQL(*s.Set, s.Columns)
		return "(" + text + ") AS " + Quoted(s.Alias), err
	}
	if s.Query != nil {
		text, err := c.SelectSQL(*s.Query)
		return "(" + text + ") AS " + Quoted(s.Alias), err
	}
	if s.Cte != nil {
		if c.Ctes[s.Cte.Name] != s.Cte {
			return "", fault.New(fault.Invalid, "CTE reference is not available in this statement")
		}
		text := Quoted(s.Cte.Name)
		if s.Alias != "" {
			text += " AS " + Quoted(s.Alias)
		}
		return text, nil
	}
	text := QuotedTable(s.Table)
	if s.Alias != "" {
		text += " AS " + Quoted(s.Alias)
	}
	return text, nil
}

type JoinKind uint8

const (
	InnerJoin JoinKind = iota
	LeftJoin
	RightJoin
	FullJoin
	CrossJoin
)

type JoinNode struct {
	Source  TableSource
	On      Expression
	Kind    JoinKind
	Lateral *ScopeRequirement
}

type OrderNode struct {
	Expression ValueExpression
	Descending bool
	Nulls      NullPlacement
}

type SelectNode struct {
	Locks      []LockSpec
	Source     TableSource
	Selections []SelectItem // Empty selects the constant 1, used by count/exists.
	Distinct   DistinctSpec
	GroupBy    []ValueExpression
	Joins      []JoinNode
	Predicates []Expression
	Having     []Expression
	Orders     []OrderNode
	Limit      value.Optional[int]
	Offset     int
}

func OrderNodes(orders []QueryOrder) []OrderNode {
	result := make([]OrderNode, len(orders))
	for i, o := range orders {
		result[i] = OrderNode{o.Value(), o.Descending, o.Nulls}
	}
	return result
}

func (c *Compiler) SelectSQL(s SelectNode) (string, error) {
	return c.SelectSQLWithOuter(s, nil)
}

func (c *Compiler) SelectSQLWithOuter(s SelectNode, outer *OuterScope) (string, error) {
	if len(s.Locks) > 0 && !c.AllowLocks {
		return "", fault.New(fault.Invalid, "row locks require a transaction-scoped query")
	}
	c.SelectDepth++
	c.SelectNodes++
	defer func() { c.SelectDepth-- }()
	if c.SelectDepth > MaxExpressionDepth || c.SelectNodes > MaxExpressionNodes {
		return "", fault.New(fault.Invalid, "nested SELECT exceeds its resource bound")
	}
	if len(s.Joins) > MaxExpressionNodes || len(s.Selections) > MaxExpressionNodes || len(s.Orders) > MaxExpressionNodes || len(s.GroupBy) > MaxExpressionNodes {
		return "", fault.New(fault.Invalid, "select exceeds its resource bound")
	}
	if limit, set := s.Limit.Get(); (set && limit < 0) || s.Offset < 0 {
		return "", fault.New(fault.Invalid, "select has a negative window")
	}
	previousSources, previousOuter, previousSelf := c.Sources, c.Outer, c.DirectSelf
	defer func() { c.Sources, c.Outer, c.DirectSelf = previousSources, previousOuter, previousSelf }()
	previousWindow := c.InWindow
	c.InWindow = false
	defer func() { c.InWindow = previousWindow }()
	previousKeys := c.Keys
	c.Keys = nil
	defer func() { c.Keys = previousKeys }()
	c.Outer = outer
	c.DirectSelf = s.Source.Self != nil
	for _, join := range s.Joins {
		c.DirectSelf = c.DirectSelf || join.Source.Self != nil
	}
	c.Sources = make(map[string]map[string]Column, len(s.Joins)+1)
	add := func(source TableSource) error {
		if c.Outer != nil && c.Outer.Sources[source.Name()] != nil {
			return fault.New(fault.Invalid, "inner SELECT shadows a correlated outer source")
		}
		validSource := source.Query == nil && source.Cte == nil && source.Set == nil && source.Self == nil && sqlname.Table(source.Table)
		if source.Query != nil {
			validSource = source.Cte == nil && source.Set == nil && source.Self == nil && source.Table == "" && sqlname.Valid(source.Alias)
		}
		if source.Cte != nil {
			validSource = source.Query == nil && source.Set == nil && source.Self == nil && source.Table == "" && sqlname.Valid(source.Cte.Name) && c.Ctes[source.Cte.Name] == source.Cte
			if len(source.Columns) != len(source.Cte.Columns) {
				validSource = false
			} else {
				for i, column := range source.Columns {
					if column != source.Cte.Columns[i] {
						validSource = false
						break
					}
				}
			}
		}
		if source.Set != nil {
			validSource = source.Query == nil && source.Cte == nil && source.Self == nil && source.Table == "" && sqlname.Valid(source.Alias)
		}
		if source.Self != nil {
			validSource = source.Query == nil && source.Cte == nil && source.Set == nil && source.Table == "" && source.Self == c.Self && sqlname.Valid(source.Self.Name) && slices.Equal(source.Columns, source.Self.Columns)
		}
		if !validSource || (source.Alias != "" && !sqlname.Valid(source.Alias)) || c.Sources[source.Name()] != nil {
			return fault.New(fault.Invalid, "invalid or repeated select source")
		}
		columns := make(map[string]Column, len(source.Columns))
		for _, column := range source.Columns {
			if !sqlname.Valid(column.Name) {
				return fault.New(fault.Invalid, "invalid select column")
			}
			if _, exists := columns[column.Name]; exists {
				return fault.New(fault.Invalid, "repeated select column")
			}
			columns[column.Name] = column
		}
		c.Sources[source.Name()] = columns
		return nil
	}
	if err := add(s.Source); err != nil {
		return "", err
	}
	nodes := &c.ExpressionNodes
	for _, join := range s.Joins {
		if join.Kind > CrossJoin {
			return "", fault.New(fault.Invalid, "invalid join kind")
		}
		if join.Lateral != nil {
			if join.Source.Query == nil || join.Kind == RightJoin || join.Kind == FullJoin {
				return "", fault.New(fault.Invalid, "correlated lateral source requires an inner, left or cross derived join")
			}
			// Validate before adding this source: neither it nor a later source
			// may satisfy its own preceding-input requirement.
			if _, err := c.CorrelationScope(join.Lateral, nil, false); err != nil {
				return "", err
			}
		}
		if err := add(join.Source); err != nil {
			return "", err
		}
		if join.Kind == CrossJoin {
			if join.On != nil {
				return "", fault.New(fault.Invalid, "cross join cannot have an ON condition")
			}
			continue
		}
		// A join cannot refer to a source introduced by a later join.
		if join.On != nil || join.Lateral == nil {
			if err := ValidateExpressionFields(join.On, c.DeclaredField, 0, nodes); err != nil {
				return "", err
			}
		}
	}
	for _, p := range s.Predicates {
		if err := ValidateExpressionFields(p, c.DeclaredField, 0, nodes); err != nil {
			return "", err
		}
	}
	windows, err := OrderedWindows(s)
	if err != nil {
		return "", err
	}
	grouped, err := c.GroupKeys(s.GroupBy)
	if err != nil {
		return "", err
	}
	// HAVING creates an implicit single group even without GROUP BY or selected
	// aggregates. An aggregate ORDER BY also makes this an aggregate query.
	grouping := len(s.GroupBy) > 0 || len(s.Having) > 0
	for _, item := range s.Selections {
		if GroupsSelect(item.Expression) {
			grouping = true
		}
	}
	for _, order := range s.Orders {
		if GroupsSelect(order.Expression) {
			grouping = true
		}
	}
	for _, key := range s.Distinct.Keys {
		if GroupsSelect(key) {
			grouping = true
		}
	}
	for _, p := range s.Having {
		if err := ValidateExpressionValues(p, func(v ValueExpression) error {
			return c.ValidateHavingValue(v, grouped)
		}, 0, nodes); err != nil {
			return "", err
		}
	}
	names := make([]string, len(s.Selections))
	distinct, err := c.DistinctSQL(s, grouped, grouping)
	if err != nil {
		return "", err
	}
	var distinctOrdering DistinctOrder
	aliases := make(map[string]bool)
	for i, item := range s.Selections {
		if s.Distinct.Kind == DistinctRows {
			plan, err := c.PlanValue(item.Expression)
			if err != nil {
				return "", err
			}
			distinctOrdering.Add(plan, i+1)
		}
		text, err := c.SelectedExpression(item.Expression, grouped, grouping)
		if err != nil {
			return "", err
		}
		if item.Alias != "" {
			if !sqlname.Valid(item.Alias) || aliases[item.Alias] {
				return "", fault.New(fault.Invalid, "invalid or repeated selection alias")
			}
			aliases[item.Alias] = true
			text += " AS " + Quoted(item.Alias)
		}
		names[i] = text
	}
	selection := "1"
	if len(names) > 0 {
		selection = strings.Join(names, ", ")
	}
	var sql strings.Builder
	from, err := c.SourceSQL(s.Source)
	if err != nil {
		return "", err
	}
	sql.WriteString("SELECT " + distinct + selection + " FROM " + from)
	allSources := c.Sources
	available := map[string]map[string]Column{s.Source.Name(): allSources[s.Source.Name()]}
	for _, join := range s.Joins {
		c.Sources = available
		from, err := c.JoinSourceSQL(join)
		if err != nil {
			return "", err
		}
		available[join.Source.Name()] = allSources[join.Source.Name()]
		if join.Kind == CrossJoin {
			sql.WriteString(" CROSS JOIN " + from)
			continue
		}
		on := "TRUE"
		if join.On != nil {
			on, err = c.Expression(join.On)
			if err != nil {
				return "", err
			}
		}
		name := [...]string{" INNER JOIN ", " LEFT JOIN ", " RIGHT JOIN ", " FULL JOIN "}[join.Kind]
		sql.WriteString(name + from + " ON " + on)
	}
	c.Sources = allSources
	if err := c.Where(&sql, s.Predicates); err != nil {
		return "", err
	}
	for i, f := range s.GroupBy {
		text, err := c.SelectedExpression(f, grouped, false)
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
	if err := c.ConditionClauseAt(&sql, "HAVING", s.Having, grouped, true); err != nil {
		return "", err
	}
	windowSQL, err := c.NamedWindowsSQL(windows, grouped, grouping)
	if err != nil {
		return "", err
	}
	sql.WriteString(windowSQL)
	for i, order := range s.Orders {
		var text string
		var err error
		if s.Distinct.Kind == DistinctRows {
			text, err = distinctOrdering.Compile(c, order, grouped, grouping)
		} else {
			text, err = c.SelectedExpression(order.Expression, grouped, grouping)
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
		sql.WriteString(OrderDirection(order))
	}
	if limit, set := s.Limit.Get(); set {
		p, err := c.Parameter(int64(limit))
		if err != nil {
			return "", err
		}
		sql.WriteString(" LIMIT " + p)
	}
	if s.Offset != 0 {
		p, err := c.Parameter(int64(s.Offset))
		if err != nil {
			return "", err
		}
		sql.WriteString(" OFFSET " + p)
	}
	if len(s.Locks) > 0 {
		suffix, err := c.CompileLocks(s, s.Locks)
		if err != nil {
			return "", err
		}
		sql.WriteString(suffix)
	}
	return sql.String(), nil
}

func (c *Compiler) DeclaredField(f FieldRef) error {
	columns := c.Columns
	if c.Sources != nil {
		columns = c.Sources[f.Table]
		if columns == nil && c.Outer != nil {
			c.ObserveField(c.SelectDepth, f)
			return c.Outer.Field(f)
		}
	}
	if _, ok := columns[f.Column]; !ok {
		return fault.New(fault.Invalid, "query uses an undeclared model column or source")
	}
	if err := f.Validate(f.Table); err != nil {
		return err
	}
	c.ObserveField(c.SelectDepth, f)
	return nil
}

func (c *Compiler) ObserveField(level int, f FieldRef) {
	if c.FieldObserver != nil {
		c.FieldObserver(level, f)
	}
}
