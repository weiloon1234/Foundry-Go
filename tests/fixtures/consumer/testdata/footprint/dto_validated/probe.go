package dto_validated

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ItemDTO struct {
	Name    string                 `json:"name"`
	Age     int                    `json:"age"`
	Enabled bool                   `json:"enabled"`
	Score   float64                `json:"score"`
	Alias   value.Optional[string] `json:"alias,omitzero"`
	Rank    value.Nullable[int]    `json:"rank"`
	Tags    []string               `json:"tags"`
	Codes   []int                  `json:"codes"`
}

func DTOJSON() contract.JSON[ItemDTO] {
	return contract.DefineJSON[ItemDTO](contract.Schema{
		Root: "foundry.test/consumer/testdata/footprint/dto_validated.ItemDTO",
		Types: []contract.Type{
			{
				ID:   "foundry.test/consumer/testdata/footprint/dto_validated.ItemDTO",
				Kind: contract.ObjectKind,
				Properties: []contract.Property{
					{Name: "name", Type: "string", Required: true},
					{Name: "age", Type: "integer", Required: true},
					{Name: "enabled", Type: "boolean", Required: true},
					{Name: "score", Type: "number", Required: true},
					{Name: "alias", Type: "github.com/weiloon1234/Foundry-Go/value.Optional[string]", Required: false},
					{Name: "rank", Type: "github.com/weiloon1234/Foundry-Go/value.Nullable[int]", Required: false},
					{Name: "tags", Type: "[]string", Required: true},
					{Name: "codes", Type: "[]int", Required: true},
				},
			},
			{ID: "string", Kind: contract.StringKind},
			{ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
			{ID: "boolean", Kind: contract.BooleanKind},
			{ID: "number", Kind: contract.NumberKind, Bits: 64},
			{ID: "github.com/weiloon1234/Foundry-Go/value.Optional[string]", Kind: contract.AliasKind, Element: "string"},
			{ID: "github.com/weiloon1234/Foundry-Go/value.Nullable[int]", Kind: contract.AliasKind, Nullable: true, Element: "integer"},
			{ID: "[]string", Kind: contract.ArrayKind, Element: "string"},
			{ID: "[]int", Kind: contract.ArrayKind, Element: "integer"},
		},
	})
}

func DTORules() validation.Rule[ItemDTO] {
	nameField := validation.DefineField("name", func(v ItemDTO) string { return v.Name })
	ageField := validation.DefineField("age", func(v ItemDTO) int { return v.Age })
	enabledField := validation.DefineField("enabled", func(v ItemDTO) bool { return v.Enabled })
	scoreField := validation.DefineField("score", func(v ItemDTO) float64 { return v.Score })
	aliasField := validation.DefineField("alias", func(v ItemDTO) value.Optional[string] { return v.Alias })
	rankField := validation.DefineField("rank", func(v ItemDTO) value.Nullable[int] { return v.Rank })
	tagsField := validation.DefineField("tags", func(v ItemDTO) []string { return v.Tags })
	codesField := validation.DefineField("codes", func(v ItemDTO) []int { return v.Codes })

	return validation.All(
		validation.Rules(nameField, validation.NonBlank[string]()),
		validation.Rules(ageField, validation.Min(18)),
		validation.Rules(enabledField, validation.OneOf(true)),
		validation.Rules(scoreField, validation.Min(0.0)),
		validation.Rules(aliasField, validation.Optional(validation.NonBlank[string]())),
		validation.Rules(rankField, validation.Nullable(validation.Min(0))),
		validation.Rules(tagsField, validation.Each[[]string](validation.NonBlank[string]())),
		validation.Rules(codesField, validation.Each[[]int](validation.Min(0))),
	)
}

func Check(ctx context.Context, input ItemDTO) error {
	return validation.Check(DTORules(), ctx, input, validation.DefaultLimits())
}

func Decode(ctx context.Context, raw []byte) (ItemDTO, error) {
	limits := contract.JSONLimits{Bytes: 1024 * 1024, Depth: 16, Nodes: 1000, Steps: 1000, Issues: 10}
	return DTOJSON().Decode(ctx, raw, limits)
}
