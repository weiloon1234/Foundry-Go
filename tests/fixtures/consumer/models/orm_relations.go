package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

var UserLinks = struct {
	Introducer   orm.BelongsTo[User, User]
	Referrals    orm.HasMany[User, User]
	Orders       orm.HasMany[User, Order]
	SingleOrder  orm.HasOne[User, Order]
	Groups       orm.ThroughRelation[User, Group, Membership]
	Friends      orm.ThroughRelation[User, User, Friendship]
	Measurements orm.HasMany[User, Measurement]
}{
	Introducer:   orm.BelongsToOf(Users.Introducer, Users.IntroducerID, Users.ID),
	Referrals:    orm.HasManyOf(Users.Referrals, Users.ID, Users.IntroducerID),
	Orders:       orm.HasManyOf(Users.Orders, Users.ID, Orders.BuyerID),
	SingleOrder:  orm.HasOneOf(Users.SingleOrder, Users.ID, Orders.BuyerID),
	Groups:       orm.ManyToManyOf(Users.Groups, Users.ID, Memberships.UserID, Memberships.GroupCode, Groups.Code),
	Friends:      orm.ManyToManyOf(Users.Friends, Users.ID, Friendships.FromID, Friendships.ToID, Users.ID),
	Measurements: orm.HasManyOf(Users.Measurements, Users.ID, Measurements.UserID),
}
var OrderLinks = struct{ Buyer orm.BelongsTo[Order, User] }{orm.BelongsToOf(Orders.Buyer, Orders.BuyerID, Users.ID)}
var CountryLinks = struct {
	Locations orm.HasMany[Country, Location]
}{orm.HasManyOf(Countries.Locations, Countries.Code, Locations.CountryCode)}
var LocationLinks = struct {
	Country orm.BelongsTo[Location, Country]
}{orm.BelongsToOf(Locations.Country, Locations.CountryCode, Countries.Code)}
var GroupLinks = struct{ Owner orm.BelongsTo[Group, User] }{orm.BelongsToOf(Groups.Owner, Groups.OwnerID, Users.ID)}
var MembershipLinks = struct {
	Inviter orm.BelongsTo[Membership, User]
}{orm.BelongsToOf(Memberships.Inviter, Memberships.InviterID, Users.ID)}

var UserValues = struct {
	OrderCount         orm.AggregateRelation[User, int64]
	OrderTotal         orm.AggregateRelation[User, value.Nullable[decimal.Decimal]]
	OrderAverage       orm.AggregateRelation[User, value.Nullable[decimal.Decimal]]
	OrderMinimum       orm.AggregateRelation[User, value.Nullable[int64]]
	OrderMaximum       orm.AggregateRelation[User, value.Nullable[int64]]
	GroupCount         orm.AggregateRelation[User, int64]
	GroupDistinct      orm.AggregateRelation[User, int64]
	GroupPriority      orm.AggregateRelation[User, value.Nullable[decimal.Decimal]]
	HasGroups          orm.AggregateRelation[User, bool]
	MeasurementTotal   orm.AggregateRelation[User, value.Nullable[decimal.Decimal]]
	MeasurementAverage orm.AggregateRelation[User, value.Nullable[decimal.Decimal]]
	MeasurementScore   orm.AggregateRelation[User, value.Nullable[float64]]
	MeasurementCount   orm.AggregateRelation[User, int64]
	FirstLabel         orm.AggregateRelation[User, value.Nullable[string]]
}{
	OrderCount:         Users.OrderCount.Using(expr.Related(UserLinks.Orders, expr.Count[Order]())),
	OrderTotal:         Users.OrderTotal.Using(expr.Related(UserLinks.Orders, expr.Sum(Orders.TotalCents))),
	OrderAverage:       Users.OrderAverage.Using(expr.Related(UserLinks.Orders, expr.Avg(Orders.TotalCents))),
	OrderMinimum:       Users.OrderMinimum.Using(expr.Related(UserLinks.Orders, expr.Min(Orders.TotalCents))),
	OrderMaximum:       Users.OrderMaximum.Using(expr.Related(UserLinks.Orders, expr.Max(Orders.TotalCents))),
	GroupCount:         Users.GroupCount.Using(expr.Related(UserLinks.Groups, expr.Count[Group]())),
	GroupDistinct:      Users.GroupDistinct.Using(expr.Related(UserLinks.Groups, expr.CountDistinctField(Groups.Code))),
	GroupPriority:      Users.GroupPriority.Using(expr.Related(orm.Pivot(UserLinks.Groups), expr.Sum(Memberships.Priority))),
	HasGroups:          Users.HasGroups.Using(expr.Related(UserLinks.Groups, expr.Exists[Group]())),
	MeasurementTotal:   Users.MeasurementTotal.Using(expr.Related(UserLinks.Measurements, expr.Sum(Measurements.Amount))),
	MeasurementAverage: Users.MeasurementAverage.Using(expr.Related(UserLinks.Measurements, expr.Avg(Measurements.Amount))),
	MeasurementScore:   Users.MeasurementScore.Using(expr.Related(UserLinks.Measurements, expr.AvgFloat(Measurements.Score))),
	MeasurementCount:   Users.MeasurementCount.Using(expr.Related(UserLinks.Measurements, expr.CountField(Measurements.Amount))),
	FirstLabel:         Users.FirstLabel.Using(expr.Related(UserLinks.Measurements, expr.Min(Measurements.Label))),
}
