package jsonqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DocumentTable and PolicyTable retain the existing model and JSON key identities.
type DocumentTable struct {
	orm.Table[Document, int]
	Settings orm.JSON[Document, Preferences]
	Backup   orm.NullableJSON[Document, Preferences]
	Tags     orm.JSON[Document, []string]
	State    orm.JSON[Document, value.Nullable[string]]
	PolicyID orm.Column[Document, value.JSON[PolicyKey]]
	Policy   orm.One[Document, Policy]
}

type PolicyTable struct {
	orm.Table[Policy, value.JSON[PolicyKey]]
	Name orm.Text[Policy, string]
}

var Documents = orm.Define("json_documents", func(b orm.Builder[Document], d *Document) DocumentTable {
	return DocumentTable{
		Table:    orm.TableOf(b, &d.ID),
		Settings: orm.JSONOf(b, &d.Settings), Backup: orm.NullableJSONOf(b, &d.Backup),
		Tags: orm.JSONOf(b, &d.Tags), State: orm.JSONOf(b, &d.State),
		PolicyID: orm.ColumnOf(b, &d.PolicyID, orm.Codec(codec.JSON[PolicyKey]())),
		Policy:   orm.OneOf(b, &d.Policy),
	}
})
var Policies = orm.Define("json_policies", func(b orm.Builder[Policy], p *Policy) PolicyTable {
	return PolicyTable{orm.TableOf(b, &p.ID, orm.Codec(codec.JSON[PolicyKey]())), orm.TextOf(b, &p.Name)}
})
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

var ProfileField = expr.JSONFieldOf(func(p *Preferences) *value.Optional[Profile] { return &p.Profile })

var NameField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Name })

var AgeField = expr.JSONFieldOf(func(p *Profile) *int64 { return &p.Age })

var NicknameField = expr.JSONFieldOf(func(p *Profile) **string { return &p.Nickname })

var ScoresField = expr.JSONFieldOf(func(p *Profile) *[]int64 { return &p.Scores })

var AttributesField = expr.JSONFieldOf(func(p *Profile) *map[int]string { return &p.Attributes })

var NextField = expr.JSONFieldOf(func(p *Profile) **Profile { return &p.Next })

var QuotedField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Quoted })

var OddField = expr.JSONFieldOf(func(p *Profile) *string { return &p.Odd })
