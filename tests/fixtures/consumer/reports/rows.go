package reports

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type UserSummaryColumns struct {
	rows.Row[UserSummary]
	ID rows.Field[UserSummary, model.ID[models.User]]
	// Email is the buyer contact address selected for this report.
	Email    rows.Field[UserSummary, string]
	Nickname rows.Field[UserSummary, value.Nullable[string]]
	Status   rows.Field[UserSummary, models.Status]
}

var UserSummaries = rows.Define(func(b rows.Builder[UserSummary], r *UserSummary) UserSummaryColumns {
	return UserSummaryColumns{Row: b.Row(),
		ID:       rows.IDOf(b, &r.ID),
		Email:    rows.TextOf(b, &r.Email),
		Nickname: rows.NullableTextOf(b, &r.Nickname),
		Status:   rows.EnumOf(b, &r.Status),
	}
})

type BuyerTotalsColumns struct {
	rows.Row[BuyerTotals]
	BuyerID rows.Field[BuyerTotals, model.ID[models.User]]
	Total   rows.Field[BuyerTotals, value.Nullable[decimal.Decimal]]
	Orders  rows.Field[BuyerTotals, int64]
	Average rows.Field[BuyerTotals, value.Nullable[decimal.Decimal]]
}

var BuyerTotalRows = rows.Define(func(b rows.Builder[BuyerTotals], r *BuyerTotals) BuyerTotalsColumns {
	return BuyerTotalsColumns{Row: b.Row(),
		BuyerID: rows.IDOf(b, &r.BuyerID),
		Total:   rows.NullableNumberOf(b, &r.Total),
		Orders:  rows.NumberOf(b, &r.Orders),
		Average: rows.NullableNumberOf(b, &r.Average),
	}
})

type OrderSummaryColumns struct {
	rows.Row[OrderSummary]
	Total rows.Field[OrderSummary, value.Nullable[decimal.Decimal]]
	Count rows.Field[OrderSummary, int64]
}

var OrderSummaries = rows.Define(func(b rows.Builder[OrderSummary], r *OrderSummary) OrderSummaryColumns {
	return OrderSummaryColumns{Row: b.Row(),
		Total: rows.NullableNumberOf(b, &r.Total),
		Count: rows.NumberOf(b, &r.Count),
	}
})

type ReferralRowColumns struct {
	rows.Row[ReferralRow]
	ID                 rows.Field[ReferralRow, model.ID[models.User]]
	Email              rows.Field[ReferralRow, string]
	IntroducerID       rows.Field[ReferralRow, value.Nullable[model.ID[models.User]]]
	IntroducerEmail    rows.Field[ReferralRow, value.Nullable[string]]
	IntroducerNickname rows.Field[ReferralRow, value.Nullable[string]]
	IntroducerStatus   rows.Field[ReferralRow, value.Nullable[models.Status]]
}

var ReferralRows = rows.Define(func(b rows.Builder[ReferralRow], r *ReferralRow) ReferralRowColumns {
	return ReferralRowColumns{Row: b.Row(),
		ID:                 rows.IDOf(b, &r.ID),
		Email:              rows.TextOf(b, &r.Email),
		IntroducerID:       rows.NullableIDOf(b, &r.IntroducerID),
		IntroducerEmail:    rows.NullableTextOf(b, &r.IntroducerEmail),
		IntroducerNickname: rows.NullableTextOf(b, &r.IntroducerNickname),
		IntroducerStatus:   rows.NullableEnumOf(b, &r.IntroducerStatus),
	}
})

type UserPairColumns struct {
	rows.Row[UserPair]
	LeftID  rows.Field[UserPair, value.Nullable[model.ID[models.User]]]
	RightID rows.Field[UserPair, value.Nullable[model.ID[models.User]]]
}

var UserPairs = rows.Define(func(b rows.Builder[UserPair], r *UserPair) UserPairColumns {
	return UserPairColumns{Row: b.Row(),
		LeftID:  rows.NullableIDOf(b, &r.LeftID),
		RightID: rows.NullableIDOf(b, &r.RightID),
	}
})

type OrderBuyerRowColumns struct {
	rows.Row[OrderBuyerRow]
	OrderID    rows.Field[OrderBuyerRow, model.ID[models.Order]]
	BuyerID    rows.Field[OrderBuyerRow, model.ID[models.User]]
	BuyerEmail rows.Field[OrderBuyerRow, string]
	TotalCents rows.Field[OrderBuyerRow, int64]
}

var OrderBuyerRows = rows.Define(func(b rows.Builder[OrderBuyerRow], r *OrderBuyerRow) OrderBuyerRowColumns {
	return OrderBuyerRowColumns{Row: b.Row(),
		OrderID:    rows.IDOf(b, &r.OrderID),
		BuyerID:    rows.IDOf(b, &r.BuyerID),
		BuyerEmail: rows.TextOf(b, &r.BuyerEmail),
		TotalCents: rows.NumberOf(b, &r.TotalCents),
	}
})

type ReferralOrderRowColumns struct {
	rows.Row[ReferralOrderRow]
	UserID            rows.Field[ReferralOrderRow, model.ID[models.User]]
	IntroducerID      rows.Field[ReferralOrderRow, value.Nullable[model.ID[models.User]]]
	IntroducerOrderID rows.Field[ReferralOrderRow, value.Nullable[model.ID[models.Order]]]
}

var ReferralOrderRows = rows.Define(func(b rows.Builder[ReferralOrderRow], r *ReferralOrderRow) ReferralOrderRowColumns {
	return ReferralOrderRowColumns{Row: b.Row(),
		UserID:            rows.IDOf(b, &r.UserID),
		IntroducerID:      rows.NullableIDOf(b, &r.IntroducerID),
		IntroducerOrderID: rows.NullableIDOf(b, &r.IntroducerOrderID),
	}
})

type LocationRowColumns struct {
	rows.Row[LocationRow]
	CountryCode  rows.Field[LocationRow, models.CountryCode]
	LocationCode rows.Field[LocationRow, models.LocationCode]
	CountryName  rows.Field[LocationRow, string]
}

var LocationRows = rows.Define(func(b rows.Builder[LocationRow], r *LocationRow) LocationRowColumns {
	return LocationRowColumns{Row: b.Row(),
		CountryCode:  rows.TextOf(b, &r.CountryCode),
		LocationCode: rows.TextOf(b, &r.LocationCode),
		CountryName:  rows.TextOf(b, &r.CountryName),
	}
})

type UserOrderStatsColumns struct {
	rows.Row[UserOrderStats]
	ID     rows.Field[UserOrderStats, model.ID[models.User]]
	Email  rows.Field[UserOrderStats, string]
	Total  rows.Field[UserOrderStats, value.Nullable[decimal.Decimal]]
	Orders rows.Field[UserOrderStats, value.Nullable[int64]]
}

var UserOrderStatsRows = rows.Define(func(b rows.Builder[UserOrderStats], r *UserOrderStats) UserOrderStatsColumns {
	return UserOrderStatsColumns{Row: b.Row(),
		ID:     rows.IDOf(b, &r.ID),
		Email:  rows.TextOf(b, &r.Email),
		Total:  rows.NullableNumberOf(b, &r.Total),
		Orders: rows.NullableNumberOf(b, &r.Orders),
	}
})

type ScalarReportColumns struct {
	rows.Row[ScalarReport]
	ID       rows.Field[ScalarReport, model.ID[models.User]]
	BuyerID  rows.Field[ScalarReport, value.Nullable[model.ID[models.User]]]
	Nickname rows.Field[ScalarReport, value.Nullable[string]]
	Orders   rows.Field[ScalarReport, value.Nullable[int64]]
}

var ScalarReports = rows.Define(func(b rows.Builder[ScalarReport], r *ScalarReport) ScalarReportColumns {
	return ScalarReportColumns{Row: b.Row(),
		ID:       rows.IDOf(b, &r.ID),
		BuyerID:  rows.NullableIDOf(b, &r.BuyerID),
		Nickname: rows.NullableTextOf(b, &r.Nickname),
		Orders:   rows.NullableNumberOf(b, &r.Orders),
	}
})
