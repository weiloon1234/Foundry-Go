package models_test

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMDeclarationSQLParity(t *testing.T) {
	check := func(name string, got orm.Statement, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if !oracleEqual(t, "consumer/models/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
	got, err := orm.Compile(models.Users.Query())
	check("Users", got, err)
	got, err = orm.Compile(models.Orders.Query())
	check("Orders", got, err)
	got, err = orm.Compile(models.Countries.Query())
	check("Countries", got, err)
	got, err = orm.Compile(models.Locations.Query())
	check("Locations", got, err)
	got, err = orm.Compile(models.Measurements.Query())
	check("Measurements", got, err)
	got, err = orm.Compile(models.LedgerEntries.Query())
	check("LedgerEntries", got, err)
	got, err = orm.Compile(models.Groups.Query())
	check("Groups", got, err)
	got, err = orm.Compile(models.Memberships.Query())
	check("Memberships", got, err)
	got, err = orm.Compile(models.Friendships.Query())
	check("Friendships", got, err)
	got, err = orm.Compile(models.WriteRecords.Query())
	check("WriteRecords", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Introducer)))
	check("Introducer", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Referrals)))
	check("Referrals", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Orders)))
	check("Orders", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.SingleOrder)))
	check("SingleOrder", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Groups)))
	check("Groups", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Friends)))
	check("Friends", got, err)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Measurements)))
	check("Measurements", got, err)
}
