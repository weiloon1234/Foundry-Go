package query

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

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
	return Statement{sql: statement.SQLText, arguments: statement.Bindings}
}
