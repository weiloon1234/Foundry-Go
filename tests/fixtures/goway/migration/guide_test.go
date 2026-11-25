package migration_test

import (
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

func TestDeclarationsAndReadSQL(t *testing.T) {
	if err := orm.Check(Users.Table, Archives.Table, models.Users.Table, models.Orders.Table); err != nil {
		t.Fatal(err)
	}
	after, err := orm.Compile(models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100)))
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "goway/migration/guide_test/1", oracle.Snapshot([]any{after.SQL(), after.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}

	if err := Theme.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := observer(); err != nil {
		t.Fatal(err)
	}
}
