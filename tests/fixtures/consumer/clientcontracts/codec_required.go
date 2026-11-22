package clientcontracts

import (
	"encoding/json"
	"reflect"
	"strings"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// AppSKU is an application-owned scalar codec that rejects empty text and
// declares NonEmpty: true through the public contract.DefineJSONValue boundary.
type AppSKU string

func (s AppSKU) MarshalText() ([]byte, error) {
	if !strings.HasPrefix(string(s), "SKU-") || len(s) < 5 {
		return nil, fault.Invalid
	}
	return []byte(s), nil
}

func (s *AppSKU) UnmarshalText(data []byte) error {
	text := string(data)
	if !strings.HasPrefix(text, "SKU-") || len(text) < 5 {
		return fault.Invalid
	}
	*s = AppSKU(text)
	return nil
}

func (s AppSKU) MarshalJSON() ([]byte, error) {
	text, err := s.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (s *AppSKU) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fault.Invalid
	}
	return s.UnmarshalText([]byte(text))
}

// JSONContract declares AppSKU as a non-empty JSON string so form bindings
// publish required: true whenever presence is required on every path to it.
func (AppSKU) JSONContract() contract.JSON[AppSKU] {
	typ := reflect.TypeFor[AppSKU]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[AppSKU](contract.Schema{
		Root:  id,
		Types: []contract.Type{{ID: id, Kind: contract.StringKind, NonEmpty: true}},
	})
}

// AppNote is an application-owned scalar codec that does NOT declare NonEmpty.
// Without NonEmpty: true, bindings keep required: false unless a rule demands it.
type AppNote string

func (n AppNote) MarshalJSON() ([]byte, error) { return json.Marshal(string(n)) }
func (n *AppNote) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fault.Invalid
	}
	*n = AppNote(text)
	return nil
}

func (AppNote) JSONContract() contract.JSON[AppNote] {
	typ := reflect.TypeFor[AppNote]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[AppNote](contract.Schema{
		Root:  id,
		Types: []contract.Type{{ID: id, Kind: contract.StringKind}},
	})
}

//foundry:dto
type CodecFormLine struct {
	ItemID      model.ID[models.Order] `json:"item_id" client:"label=fields.item_id"`
	Amount      decimal.Decimal        `json:"amount" client:"kind=money,label=fields.amount"`
	OptionalSKU value.Optional[AppSKU] `json:"optional_sku,omitzero" client:"label=fields.optional_sku"`
}

//foundry:dto http=body
type CodecFormCreate struct {
	OwnerID     model.ID[models.User]             `json:"owner_id" client:"label=fields.owner_id"`
	OmitZeroID  model.ID[models.User]             `json:"omitzero_id,omitzero" client:"label=fields.omitzero_id"`
	Password    password.Plaintext                `json:"password" client:"kind=password,label=fields.password"`
	TOTP        mfa.TOTPCode                      `json:"totp" client:"label=fields.totp"`
	Recovery    mfa.RecoveryCode                  `json:"recovery" client:"label=fields.recovery"`
	ResetToken  passwordreset.Token[models.User]  `json:"reset_token" client:"label=fields.reset_token"`
	Contact     email.Address                     `json:"contact" client:"kind=email,label=fields.contact"`
	AssetKey    storage.ObjectKey                 `json:"asset_key" client:"label=fields.asset_key"`
	Currency    money.Currency                    `json:"currency" client:"label=fields.currency"`
	SKU         AppSKU                            `json:"sku" client:"label=fields.sku"`
	OptionalSKU value.Optional[AppSKU]            `json:"optional_sku,omitzero" client:"label=fields.optional_sku"`
	OpaqueNote  AppNote                           `json:"opaque_note" client:"label=fields.opaque_note"`
	RequiredIDs value.List[model.ID[models.User]] `json:"required_ids" client:"label=fields.required_ids"`
	NullableIDs []model.ID[models.User]           `json:"nullable_ids" client:"label=fields.nullable_ids"`
	Lines       value.List[CodecFormLine]         `json:"lines" client:"label=fields.lines"`
}

//foundry:dto http=body
type CodecFormPatch struct {
	OwnerID          model.ID[models.User]                                 `json:"owner_id" client:"label=fields.owner_id"`
	OptionalOwnerID  value.Optional[model.ID[models.User]]                 `json:"optional_owner_id,omitzero" client:"label=fields.optional_owner_id"`
	NullableOwnerID  value.Optional[value.Nullable[model.ID[models.User]]] `json:"nullable_owner_id,omitzero" client:"label=fields.nullable_owner_id"`
	PointerOwnerID   *model.ID[models.User]                                `json:"pointer_owner_id" client:"label=fields.pointer_owner_id"`
	ForcedOptionalID value.Optional[model.ID[models.User]]                 `json:"forced_optional_id,omitzero" client:"label=fields.forced_optional_id"`
	OptionalSKU      value.Optional[AppSKU]                                `json:"optional_sku,omitzero" client:"label=fields.optional_sku"`
}

//foundry:query
type DecimalAmountQuery struct {
	Amount         decimal.Decimal                 `query:"amount" client:"kind=money,label=fields.amount"`
	SKU            AppSKU                          `query:"sku" client:"label=fields.sku"`
	OptionalAmount value.Optional[decimal.Decimal] `query:"optional_amount" client:"kind=money,label=fields.optional_amount"`
}
