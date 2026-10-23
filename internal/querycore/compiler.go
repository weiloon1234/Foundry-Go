package querycore

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
	SQLText  string
	Bindings []any
	Shape    string
}

type ReadKind uint8

const (
	ReadModels ReadKind = iota
	ReadCount
	ReadExists
)

type Compiler struct {
	fragments     *statementFragments
	AllowLocks    bool
	LockWork      int
	Ctes          map[string]*CteNode
	Self          *RecursiveReference
	DirectSelf    bool
	InWindow      bool
	IndexTarget   bool
	Outer         *OuterScope
	Keys          *SelectKeys
	FieldObserver func(int, FieldRef)
	Arguments     []any
	Columns       map[string]Column
	Sources       map[string]map[string]Column
	// scopeContext is the executing call's context; it resolves context
	// global scopes, including those lowered from related queries.
	ScopeContext                              context.Context
	ScopeShapeOnly                            bool
	SelectDepth, SelectNodes, ExpressionNodes int
	ValueDepth                                int
	ScalarSQLBytes                            int
}

func NewCompiler(columns []Column) Compiler {
	c := Compiler{Columns: make(map[string]Column, len(columns))}
	for _, column := range columns {
		c.Columns[column.Name] = column
	}
	return c
}

func SelectedColumns(table string, columns []Column) string {
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = Qualified(FieldRef{table, column.Name})
	}
	return strings.Join(names, ", ")
}

// countNode omits an outer ORDER BY that cannot affect a count. Orders remain
// when DISTINCT ON chooses rows or when a Limit/Offset window is counted.
func CountNode(node SelectNode) SelectNode {
	if _, limited := node.Limit.Get(); !limited && node.Offset == 0 && node.Distinct.Kind != DistinctOn {
		node.Orders = nil
	}
	return node
}

func Quoted(name string) string { return `"` + name + `"` }

func QuotedTable(table string) string {
	return Quoted(strings.ReplaceAll(table, ".", `"."`))
}

func Qualified(field FieldRef) string { return QuotedTable(field.Table) + "." + Quoted(field.Column) }

func (c *Compiler) Parameter(value any) (string, error) {
	if c.IndexTarget {
		return c.SchemaLiteral(value)
	}
	if len(c.Arguments) >= MaxParameters {
		return "", fault.New(fault.Invalid, "query exceeds PostgreSQL parameter bound")
	}
	if data, ok := value.([]byte); ok {
		value = slices.Clone(data)
	}
	c.Arguments = append(c.Arguments, value)
	return c.fragment("$"+strconv.Itoa(len(c.Arguments)), "?"), nil
}

func (c *Compiler) Expression(e Expression) (string, error) { return c.ExpressionAt(e, nil, false) }

func (c *Compiler) ExpressionAt(e Expression, grouped map[FieldRef]bool, grouping bool) (string, error) {
	switch e := e.(type) {
	case Comparison:
		return c.Comparison(e, grouped, grouping)
	case SubqueryPredicate:
		return c.SubqueryPredicate(e, grouped, grouping)
	case BinaryComparison:
		op, ok := BinaryOperator(e.Operator)
		if !ok {
			return "", fault.New(fault.Invalid, "invalid value comparison operator")
		}
		left, err := c.SelectedExpression(e.Left, grouped, grouping)
		if err != nil {
			return "", err
		}
		right, err := c.SelectedExpression(e.Right, grouped, grouping)
		if err != nil {
			return "", err
		}
		return "(" + left + " " + op + " " + right + ")", nil
	case RowComparison:
		return c.RowComparison(e, grouped, grouping)
	case ScopeNode:
		if c.ScopeShapeOnly && c.ScopeContext == nil && e.Context == nil {
			return "TRUE", nil
		}
		resolved, err := e.Resolved(c.ScopeContext)
		if err != nil {
			return "", err
		}
		return c.ExpressionAt(resolved, grouped, grouping)
	case Junction:
		if len(e.Children) == 0 {
			// Neutral dynamic filters: an empty AND is TRUE, an empty OR FALSE.
			if e.Any {
				return "FALSE", nil
			}
			return "TRUE", nil
		}
		parts := make([]string, len(e.Children))
		for i, child := range e.Children {
			text, err := c.ExpressionAt(child, grouped, grouping)
			if err != nil {
				return "", err
			}
			parts[i] = text
		}
		join := " AND "
		if e.Any {
			join = " OR "
		}
		return "(" + strings.Join(parts, join) + ")", nil
	case Negation:
		text, err := c.ExpressionAt(e.Child, grouped, grouping)
		return "(NOT " + text + ")", err
	default:
		return "", fault.New(fault.Invalid, "invalid query expression")
	}
}

func (c *Compiler) Where(sql *strings.Builder, predicates []Expression) error {
	return c.ConditionClause(sql, "WHERE", predicates)
}

func (c *Compiler) ConditionClause(sql *strings.Builder, clause string, predicates []Expression) error {
	return c.ConditionClauseAt(sql, clause, predicates, nil, false)
}

func (c *Compiler) ConditionClauseAt(sql *strings.Builder, clause string, predicates []Expression, grouped map[FieldRef]bool, grouping bool) error {
	for i, predicate := range predicates {
		text, err := c.ExpressionAt(predicate, grouped, grouping)
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

func (c *Compiler) Comparison(e Comparison, grouped map[FieldRef]bool, grouping bool) (string, error) {
	argumentStart := len(c.Arguments)
	field, err := c.SelectedExpression(e.Operand, grouped, grouping)
	if err != nil {
		return "", err
	}
	switch e.Operator {
	case IsNull:
		return "(" + field + " IS NULL)", nil
	case IsNotNull:
		return "(" + field + " IS NOT NULL)", nil
	case In, NotIn:
		if len(e.Values) == 0 {
			// Validate the complete operand, but the constant emits none of its
			// SQL. Retaining its parameters would leave holes or unused bindings.
			c.DiscardArguments(argumentStart)
			if e.Operator == NotIn {
				return "TRUE", nil
			}
			return "FALSE", nil
		}
		if len(e.Values) > MaxMembershipValues {
			return "", fault.New(fault.Invalid, "membership list exceeds its value bound")
		}
	}
	bound := make([]driver.Value, len(e.Values))
	for i, value := range e.Values {
		v, err := e.Bind(value)
		if err != nil {
			return "", err
		}
		if v == nil {
			return "", fault.New(fault.Invalid, "NULL comparison requires IsNull or IsNotNull")
		}
		if EscapedPattern(e.Operator) {
			text, ok := v.(string)
			if !ok {
				return "", fault.New(fault.Invalid, "text pattern operations require a text database representation")
			}
			v = LikePattern(e.Operator, text)
		}
		bound[i] = v
	}
	if _, column := e.Operand.(FieldRef); column && !c.IndexTarget && (e.Operator == In || e.Operator == NotIn) {
		// One array parameter keeps statement text independent of list length,
		// so the driver's statement cache is reused and lists are not limited
		// by the protocol's parameter count. PostgreSQL infers the array element
		// type from the column, preserving enum, domain and citext semantics.
		// Other operands and schema literals keep one element per value.
		if literal, ok := ArrayLiteral(e.Kind, bound); ok {
			parameter, err := c.Parameter(literal)
			if err != nil {
				return "", err
			}
			if e.Operator == NotIn {
				return "(" + field + " <> ALL(" + parameter + "))", nil
			}
			return "(" + field + " = ANY(" + parameter + "))", nil
		}
	}
	parameters := make([]string, len(bound))
	for i, v := range bound {
		var err error
		parameters[i], err = c.Parameter(v)
		if err != nil {
			return "", err
		}
	}
	switch e.Operator {
	case In:
		return "(" + field + " IN (" + c.fragment(strings.Join(parameters, ", "), parameters[0]) + "))", nil
	case NotIn:
		return "(" + field + " NOT IN (" + c.fragment(strings.Join(parameters, ", "), parameters[0]) + "))", nil
	}
	op, ok := ComparisonOperator(e.Operator)
	if !ok || len(parameters) != 1 {
		return "", fault.New(fault.Invalid, "unsupported query comparison")
	}
	result := fmt.Sprintf("(%s %s %s", field, op, parameters[0])
	if EscapedPattern(e.Operator) {
		result += " ESCAPE '!'"
	}
	return result + ")", nil
}

// likePattern escapes literal text for LIKE/ILIKE with '!' as the escape.
// Fixed SQL escaping grammar, safe for concurrent use across applications.
var LikeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

func LikePattern(op Operator, text string) string {
	escaped := LikeEscaper.Replace(text)
	switch op {
	case StartsWith, InsensitiveStartsWith:
		return escaped + "%"
	case EndsWith, InsensitiveEndsWith:
		return "%" + escaped
	default:
		return "%" + escaped + "%"
	}
}

// rowComparison compiles (a, b) > ($1, $2) for keyset traversal.
func (c *Compiler) RowComparison(e RowComparison, grouped map[FieldRef]bool, grouping bool) (string, error) {
	op, ok := ComparisonOperator(e.Operator)
	if !ok || len(e.Operands) != len(e.Values) || len(e.Operands) == 0 {
		return "", fault.New(fault.Invalid, "invalid row comparison")
	}
	left := make([]string, len(e.Operands))
	right := make([]string, len(e.Values))
	for i, operand := range e.Operands {
		text, err := c.SelectedExpression(operand, grouped, grouping)
		if err != nil {
			return "", err
		}
		left[i] = text
	}
	for i, v := range e.Values {
		parameter, err := c.Parameter(v)
		if err != nil {
			return "", err
		}
		right[i] = parameter
	}
	return "((" + strings.Join(left, ", ") + ") " + op + " (" + strings.Join(right, ", ") + "))", nil
}

func ComparisonOperator(op Operator) (string, bool) {
	switch op {
	case Equal:
		return "=", true
	case NotEqual:
		return "<>", true
	case Less:
		return "<", true
	case LessOrEqual:
		return "<=", true
	case Greater:
		return ">", true
	case GreaterOrEqual:
		return ">=", true
	case Like, Contains, StartsWith, EndsWith:
		return "LIKE", true
	case InsensitiveContains, InsensitiveLike, InsensitiveStartsWith, InsensitiveEndsWith:
		return "ILIKE", true
	case IsDistinctFrom:
		return "IS DISTINCT FROM", true
	case IsNotDistinctFrom:
		return "IS NOT DISTINCT FROM", true
	default:
		return "", false
	}
}
