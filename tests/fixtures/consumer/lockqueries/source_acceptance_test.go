package lockqueries_test

import (
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type unlockedRecordAlias struct{}

func TestUnlockedSourcesCanBeAliased(t *testing.T) {
	q := models.Users.Query()
	record := rows.SelectRecord(rows.From(q), rows.ModelScope(q))
	for _, test := range []struct {
		name  string
		alias rows.Aliased[unlockedRecordAlias, models.User]
	}{
		// lock_clause_source adds LockRows to this complete record.
		{"record", rows.AsRecord[unlockedRecordAlias, models.User](record, "unlocked")},
		// locked_subquery adds ForUpdate to this model query.
		{"model", rows.As[unlockedRecordAlias](q, "unlocked")},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := rows.Scope(test.alias)
			selected := rows.Select(test.alias, reports.UserSummaries,
				rows.Map(reports.UserSummaries.ID, expr.At(scope, models.Users.ID)),
				rows.Map(reports.UserSummaries.Email, expr.At(scope, models.Users.Email)),
				rows.Map(reports.UserSummaries.Nickname, expr.At(scope, models.Users.Nickname)),
				rows.Map(reports.UserSummaries.Status, expr.At(scope, models.Users.Status)))
			if _, err := rows.CompileRead(rows.Read(selected)); err != nil {
				t.Fatalf("ordinary alias rejected: %v", err)
			}
		})
	}
}
