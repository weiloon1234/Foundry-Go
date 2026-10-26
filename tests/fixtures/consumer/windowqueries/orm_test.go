package windowqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestWindowReportSQLParity(t *testing.T) {
	got, err := rows.Compile(ranking())
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/windowqueries/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
}
