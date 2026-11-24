package jsonqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DocumentTable and PolicyTable retain the existing model and JSON key identities.

var DocumentLinks = struct {
	Policy orm.BelongsTo[Document, Policy]
}{
	Policy: orm.BelongsToOf(Documents.Policy, Documents.PolicyID, Policies.Key()),
}

// Theme chooses the reader appearance; omitted uses the application default.
// It binds the payload field once for scoped JSON traversal.
var Theme = expr.JSONFieldOf(func(p *Preferences) *value.Optional[string] { return &p.Theme })

var LabelsField = expr.JSONFieldOf(func(p *Preferences) *value.Optional[map[string]string] { return &p.Labels })

var QuotaField = expr.JSONFieldOf(func(p *Preferences) *value.Optional[decimal.Decimal] { return &p.Quota })

// doc-example:begin json-models
var ProfileField = expr.JSONFieldOf(func(p *Preferences) *value.Optional[Profile] { return &p.Profile })

var NameField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Name })

var AgeField = expr.JSONFieldOf(func(p *Profile) *int64 { return &p.Age }) // doc-example:end json-models

var NicknameField = expr.JSONFieldOf(func(p *Profile) **string { return &p.Nickname })

var ScoresField = expr.JSONFieldOf(func(p *Profile) *[]int64 { return &p.Scores })

var AttributesField = expr.JSONFieldOf(func(p *Profile) *map[int]string { return &p.Attributes })

var NextField = expr.JSONFieldOf(func(p *Profile) **Profile { return &p.Next })

var QuotedField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Quoted })

var OddField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Odd })

func policyKeyCodec() codec.Codec[value.JSON[PolicyKey]] { return codec.JSON[PolicyKey]() }
