// Package clientcontracts exercises public client export from actual consumer
// declarations. No HTTP, WebSocket or schema implementation lives in the app.
package clientcontracts

import (
	"encoding/json"

	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:dto
type Payload struct {
	ID       model.ID[models.Order]                 `json:"id"`
	BuyerID  model.ID[models.User]                  `json:"buyer_id"`
	Natural  models.CountryCode                     `json:"natural"`
	Large    int64                                  `json:"large"`
	Counter  uint64                                 `json:"counter"`
	Amount   decimal.Decimal                        `json:"amount"`
	State    localization.Status                    `json:"state"`
	Optional value.Optional[value.Nullable[string]] `json:"optional,omitzero"`
	Nullable value.Nullable[string]                 `json:"nullable"`
	Quoted   int64                                  `json:"quoted,string"`
	Exact    json.Number                            `json:"exact"`
	When     temporal.DateTime                      `json:"when"`
	Day      temporal.Date                          `json:"day"`
	Clock    temporal.Time                          `json:"clock"`
	Local    temporal.LocalDateTime                 `json:"local"`
	Duration temporal.Interval                      `json:"duration"`
	Bytes    []byte                                 `json:"bytes"`
	Keys     map[int64]string                       `json:"keys"`
	Tags     []string                               `json:"tags"`
}

//foundry:dto
type Member struct {
	Display string `json:"display"`
}

//foundry:path pattern=/items/{key}
type ItemPath struct{ Key int64 }

//foundry:query
type Filters struct {
	Search value.Optional[string] `query:"q"`
	Tags   []string               `query:"tag"`
}
