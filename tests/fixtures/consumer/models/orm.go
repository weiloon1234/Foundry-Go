package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// UserColumns preserves the stored fields and typed relation slots of User.
type UserColumns struct {
	orm.Table[User, model.ID[User]]
	ID                 orm.Primary[User, model.ID[User]]
	Email              orm.Text[User, string]
	Age                orm.Number[User, int]
	Nickname           orm.NullableText[User, string]
	Status             orm.Enum[User, Status]
	Level              orm.Enum[User, Level]
	Birthday           orm.NullableTime[User, temporal.Date]
	IntroducerID       orm.NullableColumn[User, model.ID[User]]
	Introducer         orm.One[User, User]
	Referrals          orm.Many[User, User]
	Orders             orm.Many[User, Order]
	SingleOrder        orm.One[User, Order]
	Groups             orm.Through[User, Group, Membership]
	Friends            orm.Through[User, User, Friendship]
	Measurements       orm.Many[User, Measurement]
	OrderCount         orm.Value[User, int64]
	OrderTotal         orm.Value[User, value.Nullable[decimal.Decimal]]
	OrderAverage       orm.Value[User, value.Nullable[decimal.Decimal]]
	OrderMinimum       orm.Value[User, value.Nullable[int64]]
	OrderMaximum       orm.Value[User, value.Nullable[int64]]
	GroupCount         orm.Value[User, int64]
	GroupDistinct      orm.Value[User, int64]
	GroupPriority      orm.Value[User, value.Nullable[decimal.Decimal]]
	HasGroups          orm.Value[User, bool]
	MeasurementTotal   orm.Value[User, value.Nullable[decimal.Decimal]]
	MeasurementAverage orm.Value[User, value.Nullable[decimal.Decimal]]
	MeasurementScore   orm.Value[User, value.Nullable[float64]]
	MeasurementCount   orm.Value[User, int64]
	FirstLabel         orm.Value[User, value.Nullable[string]]
}

var Users = orm.Define("users", func(b orm.Builder[User], m *User) UserColumns {
	table := orm.TableOf(b, &m.ID)
	return UserColumns{Table: table, ID: orm.PrimaryOf(b, table.Key()),
		Email:              orm.TextOf(b, &m.Email),
		Age:                orm.NumberOf(b, &m.Age),
		Nickname:           orm.NullableTextOf(b, &m.Nickname),
		Status:             orm.EnumOf(b, &m.Status),
		Level:              orm.EnumOf(b, &m.Level),
		Birthday:           orm.NullableTimeOf(b, &m.Birthday),
		IntroducerID:       orm.NullableIDOf(b, &m.IntroducerID),
		Introducer:         orm.OneOf(b, &m.Introducer),
		Referrals:          orm.ManyOf(b, &m.Referrals),
		Orders:             orm.ManyOf(b, &m.Orders),
		SingleOrder:        orm.OneOf(b, &m.SingleOrder),
		Groups:             orm.ThroughOf(b, &m.Groups),
		Friends:            orm.ThroughOf(b, &m.Friends),
		Measurements:       orm.ManyOf(b, &m.Measurements),
		OrderCount:         orm.ValueOf(b, &m.OrderCount),
		OrderTotal:         orm.ValueOf(b, &m.OrderTotal),
		OrderAverage:       orm.ValueOf(b, &m.OrderAverage),
		OrderMinimum:       orm.ValueOf(b, &m.OrderMinimum),
		OrderMaximum:       orm.ValueOf(b, &m.OrderMaximum),
		GroupCount:         orm.ValueOf(b, &m.GroupCount),
		GroupDistinct:      orm.ValueOf(b, &m.GroupDistinct),
		GroupPriority:      orm.ValueOf(b, &m.GroupPriority),
		HasGroups:          orm.ValueOf(b, &m.HasGroups),
		MeasurementTotal:   orm.ValueOf(b, &m.MeasurementTotal),
		MeasurementAverage: orm.ValueOf(b, &m.MeasurementAverage),
		MeasurementScore:   orm.ValueOf(b, &m.MeasurementScore),
		MeasurementCount:   orm.ValueOf(b, &m.MeasurementCount),
		FirstLabel:         orm.ValueOf(b, &m.FirstLabel),
	}
})

// OrderColumns preserves the stored fields and typed relation slots of Order.
type OrderColumns struct {
	orm.Table[Order, model.ID[Order]]
	ID         orm.Primary[Order, model.ID[Order]]
	BuyerID    orm.Column[Order, model.ID[User]]
	TotalCents orm.Number[Order, int64]
	Buyer      orm.One[Order, User]
}

var Orders = orm.Define("orders", func(b orm.Builder[Order], m *Order) OrderColumns {
	table := orm.TableOf(b, &m.ID)
	return OrderColumns{Table: table, ID: orm.PrimaryOf(b, table.Key()),
		BuyerID:    orm.IDOf(b, &m.BuyerID),
		TotalCents: orm.NumberOf(b, &m.TotalCents),
		Buyer:      orm.OneOf(b, &m.Buyer),
	}
})

// CountryColumns preserves the stored fields and typed relation slots of Country.
type CountryColumns struct {
	orm.Table[Country, CountryCode]
	Code      orm.Primary[Country, CountryCode]
	Name      orm.Text[Country, string]
	Locations orm.Many[Country, Location]
}

var Countries = orm.Define("countries", func(b orm.Builder[Country], m *Country) CountryColumns {
	table := orm.TableOf(b, &m.Code)
	return CountryColumns{Table: table, Code: orm.PrimaryOf(b, table.Key()),
		Name:      orm.TextOf(b, &m.Name),
		Locations: orm.ManyOf(b, &m.Locations),
	}
})

// LocationColumns preserves the stored fields and typed relation slots of Location.
type LocationColumns struct {
	orm.Table[Location, LocationCode]
	Code        orm.Primary[Location, LocationCode]
	CountryCode orm.Text[Location, CountryCode]
	Country     orm.One[Location, Country]
}

var Locations = orm.Define("locations", func(b orm.Builder[Location], m *Location) LocationColumns {
	table := orm.TableOf(b, &m.Code)
	return LocationColumns{Table: table, Code: orm.PrimaryOf(b, table.Key()),
		CountryCode: orm.TextOf(b, &m.CountryCode),
		Country:     orm.OneOf(b, &m.Country),
	}
})

// MeasurementColumns preserves the stored fields and typed relation slots of Measurement.
type MeasurementColumns struct {
	orm.Table[Measurement, model.ID[Measurement]]
	ID     orm.Primary[Measurement, model.ID[Measurement]]
	UserID orm.Column[Measurement, model.ID[User]]
	Amount orm.NullableNumber[Measurement, decimal.Decimal]
	Score  orm.NullableNumber[Measurement, float64]
	Label  orm.Text[Measurement, string]
}

var Measurements = orm.Define("measurements", func(b orm.Builder[Measurement], m *Measurement) MeasurementColumns {
	table := orm.TableOf(b, &m.ID)
	return MeasurementColumns{Table: table, ID: orm.PrimaryOf(b, table.Key()),
		UserID: orm.IDOf(b, &m.UserID),
		Amount: orm.NullableNumberOf(b, &m.Amount),
		Score:  orm.NullableNumberOf(b, &m.Score),
		Label:  orm.TextOf(b, &m.Label),
	}
})

// LedgerEntryColumns preserves the stored fields and typed relation slots of LedgerEntry.
type LedgerEntryColumns struct {
	orm.Table[LedgerEntry, model.ID[LedgerEntry]]
	ID      orm.Primary[LedgerEntry, model.ID[LedgerEntry]]
	Amount  orm.Number[LedgerEntry, decimal.Decimal]
	Balance orm.NullableNumber[LedgerEntry, decimal.Decimal]
}

var LedgerEntries = orm.Define("ledger_entries", func(b orm.Builder[LedgerEntry], m *LedgerEntry) LedgerEntryColumns {
	table := orm.TableOf(b, &m.ID)
	return LedgerEntryColumns{Table: table, ID: orm.PrimaryOf(b, table.Key()),
		Amount:  orm.NumberOf(b, &m.Amount),
		Balance: orm.NullableNumberOf(b, &m.Balance),
	}
})

// GroupColumns preserves the stored fields and typed relation slots of Group.
type GroupColumns struct {
	orm.Table[Group, GroupCode]
	Code    orm.Primary[Group, GroupCode]
	Name    orm.Text[Group, string]
	OwnerID orm.Column[Group, model.ID[User]]
	Owner   orm.One[Group, User]
}

var Groups = orm.Define("groups", func(b orm.Builder[Group], m *Group) GroupColumns {
	table := orm.TableOf(b, &m.Code)
	return GroupColumns{Table: table, Code: orm.PrimaryOf(b, table.Key()),
		Name:    orm.TextOf(b, &m.Name),
		OwnerID: orm.IDOf(b, &m.OwnerID),
		Owner:   orm.OneOf(b, &m.Owner),
	}
})

// MembershipColumns preserves the stored fields and typed relation slots of Membership.
type MembershipColumns struct {
	orm.Table[Membership, string]
	ID        orm.Primary[Membership, string]
	UserID    orm.Column[Membership, model.ID[User]]
	GroupCode orm.NullableText[Membership, GroupCode]
	Lookup    orm.Text[Membership, string]
	Priority  orm.Number[Membership, int]
	Level     orm.Enum[Membership, Level]
	InviterID orm.NullableColumn[Membership, model.ID[User]]
	Inviter   orm.One[Membership, User]
}

var Memberships = orm.Define("memberships", func(b orm.Builder[Membership], m *Membership) MembershipColumns {
	table := orm.TableOf(b, &m.ID)
	return MembershipColumns{Table: table, ID: orm.PrimaryOf(b, table.Key()),
		UserID:    orm.IDOf(b, &m.UserID),
		GroupCode: orm.NullableTextOf(b, &m.GroupCode),
		Lookup:    orm.TextOf(b, &m.Lookup),
		Priority:  orm.NumberOf(b, &m.Priority),
		Level:     orm.EnumOf(b, &m.Level),
		InviterID: orm.NullableIDOf(b, &m.InviterID),
		Inviter:   orm.OneOf(b, &m.Inviter),
	}
})

// FriendshipColumns preserves the stored fields and typed relation slots of Friendship.
type FriendshipColumns struct {
	orm.Table[Friendship, model.ID[Friendship]]
	ID     orm.Primary[Friendship, model.ID[Friendship]]
	FromID orm.Column[Friendship, model.ID[User]]
	ToID   orm.Column[Friendship, model.ID[User]]
	Note   orm.Text[Friendship, string]
}

var Friendships = orm.Define("friendships", func(b orm.Builder[Friendship], m *Friendship) FriendshipColumns {
	table := orm.TableOf(b, &m.ID)
	return FriendshipColumns{Table: table, ID: orm.PrimaryOf(b, table.Key()),
		FromID: orm.IDOf(b, &m.FromID),
		ToID:   orm.IDOf(b, &m.ToID),
		Note:   orm.TextOf(b, &m.Note),
	}
})

// WriteRecordColumns preserves the stored fields and typed relation slots of WriteRecord.
type WriteRecordColumns struct {
	orm.Table[WriteRecord, RecordKey]
	Code    orm.Primary[WriteRecord, RecordKey]
	Name    orm.Text[WriteRecord, string]
	Enabled orm.Column[WriteRecord, bool]
	Note    orm.NullableText[WriteRecord, string]
	Amount  orm.Number[WriteRecord, decimal.Decimal]
}

var WriteRecords = orm.Define("write_records", func(b orm.Builder[WriteRecord], m *WriteRecord) WriteRecordColumns {
	table := orm.TableOf(b, &m.Code)
	return WriteRecordColumns{Table: table, Code: orm.PrimaryOf(b, table.Key()),
		Name:    orm.TextOf(b, &m.Name),
		Enabled: orm.ColumnOf(b, &m.Enabled),
		Note:    orm.NullableTextOf(b, &m.Note),
		Amount:  orm.NumberOf(b, &m.Amount),
	}
})
