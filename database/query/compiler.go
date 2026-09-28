package query

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Statement is a compiled parameterized PostgreSQL statement. SQL and Arguments
// are explicit inspection/integration boundaries. Formatting omits bindings.
type Statement struct {
	sql       string
	arguments []any
}

func (s Statement) SQL() string { return s.sql }
func (s Statement) Arguments() []any {
	result := slices.Clone(s.arguments)
	for i, v := range result {
		if data, ok := v.([]byte); ok {
			result[i] = slices.Clone(data)
		}
	}
	return result
}
func (s Statement) String() string   { return s.sql }
func (s Statement) GoString() string { return s.sql }

type readKind uint8

const (
	readModels readKind = iota
	readCount
	readExists
)

type compiler struct {
	allowLocks                                bool
	lockWork                                  int
	ctes                                      map[string]*cteNode
	self                                      *recursiveReference
	directSelf                                bool
	inWindow                                  bool
	indexTarget                               bool
	outer                                     *outerScope
	keys                                      *selectKeys
	fieldObserver                             func(int, fieldRef)
	arguments                                 []any
	columns                                   map[string]Column
	sources                                   map[string]map[string]Column
	selectDepth, selectNodes, expressionNodes int
	valueDepth                                int
	scalarSQLBytes                            int
}

func newCompiler(columns []Column) compiler {
	c := compiler{columns: make(map[string]Column, len(columns))}
	for _, column := range columns {
		c.columns[column.Name] = column
	}
	return c
}

func selectedColumns(table string, columns []Column) string {
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = qualified(fieldRef{table, column.Name})
	}
	return strings.Join(names, ", ")
}

// Compile validates declarations and codecs, then compiles a complete model
// SELECT. It never connects or interpolates values into statement text.
func (q Query[M]) Compile() (Statement, error) { return q.compile(readModels) }

func (q Query[M]) compile(kind readKind) (Statement, error) {
	if err := q.Validate(); err != nil {
		return Statement{}, err
	}
	if q.definition == nil {
		return Statement{}, fault.New(fault.Invalid, "model execution requires generated metadata and hydration")
	}
	c := newCompiler(q.definition.columns)
	if kind == readExists {
		q = q.atMostOne()
	}
	node := q.modelSelect()
	if kind != readModels {
		node.selections = nil
	}
	statement, err := c.compileSelect(node)
	if err != nil {
		return Statement{}, err
	}
	switch kind {
	case readCount:
		statement = `SELECT COUNT(*) FROM (` + statement + `) AS "foundry_count"`
	case readExists:
		statement = "SELECT EXISTS(" + statement + ")"
	}
	return Statement{sql: statement, arguments: c.arguments}, nil
}

func (q Query[M]) modelSelect() selectNode {
	return selectNode{source: tableSource{table: q.table, columns: q.definition.columns}, selections: columnSelections(q.table, q.definition.columns), predicates: q.effectivePredicates(), orders: orderNodes(q.orders), limit: q.limit, offset: q.offset}
}

func quoted(name string) string { return `"` + name + `"` }
func quotedTable(table string) string {
	parts := strings.Split(table, ".")
	for i, part := range parts {
		parts[i] = quoted(part)
	}
	return strings.Join(parts, ".")
}
func qualified(field fieldRef) string { return quotedTable(field.table) + "." + quoted(field.column) }

func (c *compiler) parameter(value any) (string, error) {
	if c.indexTarget {
		return c.schemaLiteral(value)
	}
	if len(c.arguments) >= MaxParameters {
		return "", fault.New(fault.Invalid, "query exceeds PostgreSQL parameter bound")
	}
	if data, ok := value.([]byte); ok {
		value = slices.Clone(data)
	}
	c.arguments = append(c.arguments, value)
	return "$" + strconv.Itoa(len(c.arguments)), nil
}

func (c *compiler) expression(e expression) (string, error) { return c.expressionAt(e, nil, false) }
func (c *compiler) expressionAt(e expression, grouped map[fieldRef]bool, grouping bool) (string, error) {
	switch e := e.(type) {
	case comparison:
		return c.comparison(e, grouped, grouping)
	case subqueryPredicate:
		return c.subqueryPredicate(e, grouped, grouping)
	case binaryComparison:
		op, ok := binaryOperator(e.operator)
		if !ok {
			return "", fault.New(fault.Invalid, "invalid value comparison operator")
		}
		left, err := c.selectedExpression(e.left, grouped, grouping)
		if err != nil {
			return "", err
		}
		right, err := c.selectedExpression(e.right, grouped, grouping)
		if err != nil {
			return "", err
		}
		return "(" + left + " " + op + " " + right + ")", nil
	case junction:
		parts := make([]string, len(e.children))
		for i, child := range e.children {
			text, err := c.expressionAt(child, grouped, grouping)
			if err != nil {
				return "", err
			}
			parts[i] = text
		}
		join := " AND "
		if e.any {
			join = " OR "
		}
		return "(" + strings.Join(parts, join) + ")", nil
	case negation:
		text, err := c.expressionAt(e.child, grouped, grouping)
		return "(NOT " + text + ")", err
	default:
		return "", fault.New(fault.Invalid, "invalid query expression")
	}
}

func (c *compiler) where(sql *strings.Builder, predicates []expression) error {
	return c.conditionClause(sql, "WHERE", predicates)
}

func (c *compiler) conditionClause(sql *strings.Builder, clause string, predicates []expression) error {
	return c.conditionClauseAt(sql, clause, predicates, nil, false)
}
func (c *compiler) conditionClauseAt(sql *strings.Builder, clause string, predicates []expression, grouped map[fieldRef]bool, grouping bool) error {
	for i, predicate := range predicates {
		text, err := c.expressionAt(predicate, grouped, grouping)
		if err != nil {
			return err
		}
		if i == 0 {
			sql.WriteString(" " + clause + " ")
		} else {
			sql.WriteString(" AND ")
		}
		sql.WriteString(text)
	}
	return nil
}

func (c *compiler) comparison(e comparison, grouped map[fieldRef]bool, grouping bool) (string, error) {
	argumentStart := len(c.arguments)
	field, err := c.selectedExpression(e.operand, grouped, grouping)
	if err != nil {
		return "", err
	}
	switch e.operator {
	case isNull:
		return "(" + field + " IS NULL)", nil
	case isNotNull:
		return "(" + field + " IS NOT NULL)", nil
	case in:
		if len(e.values) == 0 {
			// Validate the complete operand, but FALSE emits none of its SQL.
			// Retaining its parameters would leave holes or unused bindings.
			c.discardArguments(argumentStart)
			return "FALSE", nil
		}
	}
	parameters := make([]string, len(e.values))
	for i, value := range e.values {
		bound, err := e.bind(value)
		if err != nil {
			return "", err
		}
		if bound == nil {
			return "", fault.New(fault.Invalid, "NULL comparison requires IsNull or IsNotNull")
		}
		if e.operator == contains || e.operator == insensitiveContains {
			text, ok := bound.(string)
			if !ok {
				return "", fault.New(fault.Invalid, "Contains requires a text database representation")
			}
			bound = "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(text) + "%"
		}
		parameters[i], err = c.parameter(bound)
		if err != nil {
			return "", err
		}
	}
	if e.operator == in {
		return "(" + field + " IN (" + strings.Join(parameters, ", ") + "))", nil
	}
	op, ok := comparisonOperator(e.operator)
	if !ok || len(parameters) != 1 {
		return "", fault.New(fault.Invalid, "unsupported query comparison")
	}
	result := fmt.Sprintf("(%s %s %s", field, op, parameters[0])
	if e.operator == contains || e.operator == insensitiveContains {
		result += " ESCAPE '!'"
	}
	return result + ")", nil
}

func comparisonOperator(op operator) (string, bool) {
	switch op {
	case equal:
		return "=", true
	case notEqual:
		return "<>", true
	case less:
		return "<", true
	case lessOrEqual:
		return "<=", true
	case greater:
		return ">", true
	case greaterOrEqual:
		return ">=", true
	case like, contains:
		return "LIKE", true
	case insensitiveContains:
		return "ILIKE", true
	case isDistinctFrom:
		return "IS DISTINCT FROM", true
	case isNotDistinctFrom:
		return "IS NOT DISTINCT FROM", true
	default:
		return "", false
	}
}
