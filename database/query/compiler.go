package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Statement is a compiled parameterized PostgreSQL statement. SQL and Arguments
// are explicit inspection/integration boundaries. Formatting omits SQL and bindings.
type Statement struct{ core *querycore.StatementState }

type readKind = querycore.ReadKind

const readModels = querycore.ReadModels

const readCount = querycore.ReadCount

const readExists = querycore.ReadExists

type compiler = querycore.Compiler

func newCompiler(columns []columnData) compiler { return querycore.NewCompiler(columns) }

func selectedColumns(table string, columns []columnData) string {
	return querycore.SelectedColumns(table, columns)
}

func countNode(node selectNode) selectNode { return querycore.CountNode(node) }

func quoted(name string) string { return querycore.Quoted(name) }

func quotedTable(table string) string { return querycore.QuotedTable(table) }

func qualified(field fieldRef) string { return querycore.Qualified(field) }

var likeEscaper = querycore.LikeEscaper

func likePattern(op operator, text string) string { return querycore.LikePattern(op, text) }

func comparisonOperator(op operator) (string, bool) { return querycore.ComparisonOperator(op) }

func (s Statement) SQL() string { return s.coreStatement().SQLText }

func (s Statement) Arguments() []any {
	result := slices.Clone(s.coreStatement().Bindings)
	for i, v := range result {
		if data, ok := v.([]byte); ok {
			result[i] = slices.Clone(data)
		}
	}
	return result
}

func (s Statement) String() string { return "query statement (redacted)" }

func (s Statement) GoString() string { return "query statement (redacted)" }

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
	result, err := q.queryCore.CompileModel(kind, q.definition.selections(q.queryCore.Table))
	return statementFromCore(result), err
}

func (q Query[M]) modelSelect() selectNode {
	return q.queryCore.ModelSelect(q.definition.selections(q.queryCore.Table))
}
