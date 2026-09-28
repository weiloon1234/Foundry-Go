package reports

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:projection
type ReferralRow struct {
	ID                 model.ID[models.User]
	Email              string
	IntroducerID       value.Nullable[model.ID[models.User]]
	IntroducerEmail    value.Nullable[string]
	IntroducerNickname value.Nullable[string]
	IntroducerStatus   value.Nullable[models.Status]
}

//foundry:projection
type UserPair struct {
	LeftID  value.Nullable[model.ID[models.User]]
	RightID value.Nullable[model.ID[models.User]]
}

//foundry:projection
type OrderBuyerRow struct {
	OrderID    model.ID[models.Order]
	BuyerID    model.ID[models.User]
	BuyerEmail string
	TotalCents int64
}

//foundry:projection
type ReferralOrderRow struct {
	UserID            model.ID[models.User]
	IntroducerID      value.Nullable[model.ID[models.User]]
	IntroducerOrderID value.Nullable[model.ID[models.Order]]
}

//foundry:projection
type LocationRow struct {
	CountryCode  models.CountryCode
	LocationCode models.LocationCode
	CountryName  string
}
