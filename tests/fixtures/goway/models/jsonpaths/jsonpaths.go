package jsonpaths

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Profile struct {
	Name       string         `json:"name"`
	Age        int64          `json:"age,string"`
	Nickname   *string        `json:"nickname"`
	Scores     []int64        `json:"scores"`
	Attributes map[int]string `json:"attributes"`
	Next       *Profile       `json:"next"`
	Quoted     string         `json:"quoted,string"`
	Odd        string         `json:"odd%path"`
}
type Preferences struct {
	Theme   value.Optional[string]            `json:"theme,omitzero"`
	Labels  value.Optional[map[string]string] `json:"labels,omitzero"`
	Quota   value.Optional[decimal.Decimal]   `json:"quota,omitzero"`
	Profile value.Optional[Profile]           `json:"profile,omitzero"`
}
type Document struct {
	ID       int64
	Settings value.JSON[Preferences]
	Backup   value.Nullable[value.JSON[Preferences]]
	Tags     value.JSON[[]string]
	Fixed    value.JSON[[3]int64]
	State    value.JSON[value.Nullable[string]]
}
type Table struct {
	orm.Table[Document, int64]
	Settings orm.JSON[Document, Preferences]
	Backup   orm.NullableJSON[Document, Preferences]
	Tags     orm.JSON[Document, []string]
	Fixed    orm.JSON[Document, [3]int64]
	State    orm.JSON[Document, value.Nullable[string]]
}

var Documents = Declare()

func Declare() Table {
	return orm.Define("json_documents", func(b orm.Builder[Document], d *Document) Table {
		return Table{orm.TableOf(b, &d.ID), orm.JSONOf(b, &d.Settings), orm.NullableJSONOf(b, &d.Backup), orm.JSONOf(b, &d.Tags), orm.JSONOf(b, &d.Fixed), orm.JSONOf(b, &d.State)}
	})
}

// Handwritten declarations are resolved once and validated during application startup.
var Theme = expr.JSONFieldOf(func(p *Preferences) *value.Optional[string] { return &p.Theme })
var Labels = expr.JSONFieldOf(func(p *Preferences) *value.Optional[map[string]string] { return &p.Labels })
var Quota = expr.JSONFieldOf(func(p *Preferences) *value.Optional[decimal.Decimal] { return &p.Quota })
var UserProfile = expr.JSONFieldOf(func(p *Preferences) *value.Optional[Profile] { return &p.Profile })
var Name = expr.JSONFieldOf(func(p *Profile) *string { return &p.Name })
var Age = expr.JSONFieldOf(func(p *Profile) *int64 { return &p.Age })
var Nickname = expr.JSONFieldOf(func(p *Profile) **string { return &p.Nickname })
var Scores = expr.JSONFieldOf(func(p *Profile) *[]int64 { return &p.Scores })
var Attributes = expr.JSONFieldOf(func(p *Profile) *map[int]string { return &p.Attributes })
var Next = expr.JSONFieldOf(func(p *Profile) **Profile { return &p.Next })
var Quoted = expr.JSONFieldOf(func(p *Profile) *string { return &p.Quoted })
var Odd = expr.JSONFieldOf(func(p *Profile) *string { return &p.Odd })
var FixedElements = expr.JSONFixedArrayOf(func(p *[3]int64) *int64 { return &p[0] })

func Check() error {
	if err := orm.Check(Documents); err != nil {
		return err
	}
	for _, err := range []error{Theme.Validate(), Labels.Validate(), Quota.Validate(), UserProfile.Validate(), Name.Validate(), Age.Validate(), Nickname.Validate(), Scores.Validate(), Attributes.Validate(), Next.Validate(), Quoted.Validate(), Odd.Validate(), FixedElements.Validate()} {
		if err != nil {
			return err
		}
	}
	return nil
}
