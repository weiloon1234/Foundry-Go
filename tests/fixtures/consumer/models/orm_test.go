package models

import (
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
		if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
			t.Fatalf("%s SQL changed\nnew: %s\nold: %s", name, got.SQL(), want.SQL())
		}
	}
	got, err := orm.Compile(Users.Query())
	check("Users", got, err, QueryUsers().Compile)
	got, err = orm.Compile(Orders.Query())
	check("Orders", got, err, QueryOrders().Compile)
	got, err = orm.Compile(Countries.Query())
	check("Countries", got, err, QueryCountries().Compile)
	got, err = orm.Compile(Locations.Query())
	check("Locations", got, err, QueryLocations().Compile)
	got, err = orm.Compile(Measurements.Query())
	check("Measurements", got, err, QueryMeasurements().Compile)
	got, err = orm.Compile(LedgerEntries.Query())
	check("LedgerEntries", got, err, QueryLedgerEntries().Compile)
	got, err = orm.Compile(Groups.Query())
	check("Groups", got, err, QueryGroups().Compile)
	got, err = orm.Compile(Memberships.Query())
	check("Memberships", got, err, QueryMemberships().Compile)
	got, err = orm.Compile(Friendships.Query())
	check("Friendships", got, err, QueryFriendships().Compile)
	got, err = orm.Compile(WriteRecords.Query())
	check("WriteRecords", got, err, QueryWriteRecords().Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.Introducer)))
	check("Introducer", got, err, QueryUsers().Where(UserRelations().Introducer.Exists()).Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.Referrals)))
	check("Referrals", got, err, QueryUsers().Where(UserRelations().Referrals.Exists()).Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.Orders)))
	check("Orders", got, err, QueryUsers().Where(UserRelations().Orders.Exists()).Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.SingleOrder)))
	check("SingleOrder", got, err, QueryUsers().Where(UserRelations().SingleOrder.Exists()).Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.Groups)))
	check("Groups", got, err, QueryUsers().Where(UserRelations().Groups.Exists()).Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.Friends)))
	check("Friends", got, err, QueryUsers().Where(UserRelations().Friends.Exists()).Compile)
	got, err = orm.Compile(Users.Query().Where(orm.Has(UserLinks.Measurements)))
	check("Measurements", got, err, QueryUsers().Where(UserRelations().Measurements.Exists()).Compile)
}
