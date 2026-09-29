package query

import (
	"context"
	"database/sql/driver"
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
	allowLocks    bool
	lockWork      int
	ctes          map[string]*cteNode
	self          *recursiveReference
	directSelf    bool
	inWindow      bool
	indexTarget   bool
	outer         *outerScope
	keys          *selectKeys
	fieldObserver func(int, fieldRef)
	arguments     []any
	columns       map[string]Column
	sources       map[string]map[string]Column
	// scopeContext is the executing call's context; it resolves context
	// global scopes, including those lowered from related queries.
	scopeContext                              context.Context
	scopeShapeOnly                            bool
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
	c.scopeContext = q.scopeContext
	if kind == readExists {
		q = q.atMostOne()
	}
	node := q.modelSelect()
	if kind != readModels {
		node.selections = nil
	}
	if kind == readCount {
		node = countNode(node)
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

// countNode omits an outer ORDER BY that cannot affect a count. Orders remain
// when DISTINCT ON chooses rows or when a Limit/Offset window is counted.
func countNode(node selectNode) selectNode {
	if _, limited := node.limit.Get(); !limited && node.offset == 0 && node.distinct.kind != distinctOn {
		node.orders = nil
	}
	return node
}

func (q Query[M]) modelSelect() selectNode {
	node := selectNode{source: tableSource{table: q.table, columns: q.definition.columns}, selections: columnSelections(q.table, q.definition.columns), predicates: q.effectivePredicates(), orders: orderNodes(q.orders), limit: q.limit, offset: q.offset}
	if q.distinctOnKey != nil {
		// One-of-many loading keeps the first row per relation key.
		node.distinct = distinctSpec{kind: distinctOn, keys: []valueExpression{*q.distinctOnKey}}
		node.orders = append([]orderNode{{expression: *q.distinctOnKey}}, node.orders...)
	}
	return node
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
	case rowComparison:
		return c.rowComparison(e, grouped, grouping)
	case scopeNode:
		if c.scopeShapeOnly && c.scopeContext == nil && e.context == nil {
			return "TRUE", nil
		}
		resolved, err := e.resolved(c.scopeContext)
		if err != nil {
			return "", err
		}
		return c.expressionAt(resolved, grouped, grouping)
	case junction:
		if len(e.children) == 0 {
			// Neutral dynamic filters: an empty AND is TRUE, an empty OR FALSE.
			if e.any {
				return "FALSE", nil
			}
			return "TRUE", nil
		}
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
	case in, notIn:
		if len(e.values) == 0 {
			// Validate the complete operand, but the constant emits none of its
			// SQL. Retaining its parameters would leave holes or unused bindings.
			c.discardArguments(argumentStart)
			if e.operator == notIn {
				return "TRUE", nil
			}
			return "FALSE", nil
		}
		if len(e.values) > MaxMembershipValues {
			return "", fault.New(fault.Invalid, "membership list exceeds its value bound")
		}
	}
	bound := make([]driver.Value, len(e.values))
	for i, value := range e.values {
		v, err := e.bind(value)
		if err != nil {
			return "", err
		}
		if v == nil {
			return "", fault.New(fault.Invalid, "NULL comparison requires IsNull or IsNotNull")
		}
		if escapedPattern(e.operator) {
			text, ok := v.(string)
			if !ok {
				return "", fault.New(fault.Invalid, "text pattern operations require a text database representation")
			}
			v = likePattern(e.operator, text)
		}
		bound[i] = v
	}
	if _, column := e.operand.(fieldRef); column && !c.indexTarget && (e.operator == in || e.operator == notIn) {
		// One array parameter keeps statement text independent of list length,
		// so the driver's statement cache is reused and lists are not limited
		// by the protocol's parameter count. PostgreSQL infers the array element
		// type from the column, preserving enum, domain and citext semantics.
		// Other operands and schema literals keep one element per value.
		if literal, ok := arrayLiteral(e.kind, bound); ok {
			parameter, err := c.parameter(literal)
			if err != nil {
				return "", err
			}
			if e.operator == notIn {
				return "(" + field + " <> ALL(" + parameter + "))", nil
			}
			return "(" + field + " = ANY(" + parameter + "))", nil
		}
	}
	parameters := make([]string, len(bound))
	for i, v := range bound {
		var err error
		parameters[i], err = c.parameter(v)
		if err != nil {
			return "", err
		}
	}
	switch e.operator {
	case in:
		return "(" + field + " IN (" + strings.Join(parameters, ", ") + "))", nil
	case notIn:
		return "(" + field + " NOT IN (" + strings.Join(parameters, ", ") + "))", nil
	}
	op, ok := comparisonOperator(e.operator)
	if !ok || len(parameters) != 1 {
		return "", fault.New(fault.Invalid, "unsupported query comparison")
	}
	result := fmt.Sprintf("(%s %s %s", field, op, parameters[0])
	if escapedPattern(e.operator) {
		result += " ESCAPE '!'"
	}
	return result + ")", nil
}

// likePattern escapes literal text for LIKE/ILIKE with '!' as the escape.
func likePattern(op operator, text string) string {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(text)
	switch op {
	case startsWith, insensitiveStartsWith:
		return escaped + "%"
	case endsWith, insensitiveEndsWith:
		return "%" + escaped
	default:
		return "%" + escaped + "%"
	}
}

// rowComparison compiles (a, b) > ($1, $2) for keyset traversal.
func (c *compiler) rowComparison(e rowComparison, grouped map[fieldRef]bool, grouping bool) (string, error) {
	op, ok := comparisonOperator(e.operator)
	if !ok || len(e.operands) != len(e.values) || len(e.operands) == 0 {
		return "", fault.New(fault.Invalid, "invalid row comparison")
	}
	left := make([]string, len(e.operands))
	right := make([]string, len(e.values))
	for i, operand := range e.operands {
		text, err := c.selectedExpression(operand, grouped, grouping)
		if err != nil {
			return "", err
		}
		left[i] = text
	}
	for i, v := range e.values {
		parameter, err := c.parameter(v)
		if err != nil {
			return "", err
		}
		right[i] = parameter
	}
	return "((" + strings.Join(left, ", ") + ") " + op + " (" + strings.Join(right, ", ") + "))", nil
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
	case like, contains, startsWith, endsWith:
		return "LIKE", true
	case insensitiveContains, insensitiveLike, insensitiveStartsWith, insensitiveEndsWith:
		return "ILIKE", true
	case isDistinctFrom:
		return "IS DISTINCT FROM", true
	case isNotDistinctFrom:
		return "IS NOT DISTINCT FROM", true
	default:
		return "", false
	}
}
