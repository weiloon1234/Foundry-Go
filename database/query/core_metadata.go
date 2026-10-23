package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Named public declarations retain their package identity. Only their immutable
// SQL data is lowered at registration; applications cannot reach engine state.
type columnData = querycore.Column
type scopeNameData = querycore.GlobalScopeName

func columnDataSlice(columns []Column) []columnData {
	if columns == nil {
		return nil
	}
	owned := make([]columnData, len(columns))
	for i, column := range columns {
		owned[i] = columnData(column)
	}
	return owned
}
func statementFromCore(statement querycore.Statement) Statement {
	return Statement{sql: statement.SQLText, arguments: statement.Bindings, shape: statement.Shape}
}

func (s Statement) coreStatement() querycore.Statement {
	return querycore.Statement{SQLText: s.sql, Bindings: s.arguments, Shape: s.shape}
}
func (s Statement) executionContext(ctx context.Context) context.Context {
	return s.coreStatement().Context(ctx)
}
func (s Statement) envelope(prefix, suffix string) Statement {
	return statementFromCore(s.coreStatement().Envelope(prefix, suffix))
}
