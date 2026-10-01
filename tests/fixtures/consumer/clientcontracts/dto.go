// Package clientcontracts exercises public client export from actual consumer
// declarations. No HTTP, WebSocket or schema implementation lives in the app.
package clientcontracts

import (
	"encoding/json"

	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/i18n"
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
	Amount   decimal.Decimal                        `json:"amount" client:"kind=money,label=fields.amount,help=help.amount"`
	State    localization.Status                    `json:"state"`
	Optional value.Optional[value.Nullable[string]] `json:"optional,omitzero" client:"kind=multiline"`
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
	// Titles is keyed by the catalog's supported locales in client requests.
	Titles value.Optional[map[i18n.LocaleID]string] `json:"titles,omitzero"`
}

// OwnerIndex is a schema-only DTO for descriptor navigation: entries keyed by
// model IDs and enum cases, and a fixed-length array.
//
//foundry:dto
type OwnerIndex struct {
	Owners map[model.ID[models.User]]string `json:"owners"`
	States map[localization.Status]string   `json:"states"`
	Pair   [2]string                        `json:"pair"`
}

//foundry:dto
type Member struct {
	Display string `json:"display"`
}

//foundry:path pattern=/items/{key}
type ItemPath struct{ Key int64 }

//foundry:query
type Filters struct {
	Search value.Optional[string] `query:"q" client:"kind=text,label=fields.search"`
	Tags   []string               `query:"tag"`
}
