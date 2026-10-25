package models_test

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestORMDeclarationSQLParity(t *testing.T) {
	check := func(name string, got orm.Statement, err error, legacy func() (query.Statement, error)) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, err := legacy()
		if err != nil {
			t.Fatal(err)
		}
		if want.SQL() != got.SQL() || !reflect.DeepEqual(want.Arguments(), got.Arguments()) || !oracleEqual(t, "consumer/models/orm_test/1", oracle.Snapshot([]any{want.SQL(), want.Arguments()}), oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
			t.Fatalf("%s SQL changed\nnew: %s\nold: %s", name, got.SQL(), want.SQL())
		}
	}
	got, err := orm.Compile(models.Users.Query())
	check("Users", got, err, models.QueryUsers().Compile)
	got, err = orm.Compile(models.Orders.Query())
	check("Orders", got, err, models.QueryOrders().Compile)
	got, err = orm.Compile(models.Countries.Query())
	check("Countries", got, err, models.QueryCountries().Compile)
	got, err = orm.Compile(models.Locations.Query())
	check("Locations", got, err, models.QueryLocations().Compile)
	got, err = orm.Compile(models.Measurements.Query())
	check("Measurements", got, err, models.QueryMeasurements().Compile)
	got, err = orm.Compile(models.LedgerEntries.Query())
	check("LedgerEntries", got, err, models.QueryLedgerEntries().Compile)
	got, err = orm.Compile(models.Groups.Query())
	check("Groups", got, err, models.QueryGroups().Compile)
	got, err = orm.Compile(models.Memberships.Query())
	check("Memberships", got, err, models.QueryMemberships().Compile)
	got, err = orm.Compile(models.Friendships.Query())
	check("Friendships", got, err, models.QueryFriendships().Compile)
	got, err = orm.Compile(models.WriteRecords.Query())
	check("WriteRecords", got, err, models.QueryWriteRecords().Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Introducer)))
	check("Introducer", got, err, models.QueryUsers().Where(models.UserRelations().Introducer.Exists()).Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Referrals)))
	check("Referrals", got, err, models.QueryUsers().Where(models.UserRelations().Referrals.Exists()).Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Orders)))
	check("Orders", got, err, models.QueryUsers().Where(models.UserRelations().Orders.Exists()).Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.SingleOrder)))
	check("SingleOrder", got, err, models.QueryUsers().Where(models.UserRelations().SingleOrder.Exists()).Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Groups)))
	check("Groups", got, err, models.QueryUsers().Where(models.UserRelations().Groups.Exists()).Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Friends)))
	check("Friends", got, err, models.QueryUsers().Where(models.UserRelations().Friends.Exists()).Compile)
	got, err = orm.Compile(models.Users.Query().Where(orm.Has(models.UserLinks.Measurements)))
	check("Measurements", got, err, models.QueryUsers().Where(models.UserRelations().Measurements.Exists()).Compile)
}
