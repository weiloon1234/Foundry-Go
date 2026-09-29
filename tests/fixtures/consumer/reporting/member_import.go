package reporting

import (
	"github.com/weiloon1234/Foundry-Go/datatable/importer"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MemberImport is one row of an uploaded member list. It is transport input,
// not a persistence model; the import handler owns tenant scope and writes.
type MemberImport struct {
	Name     string
	Nickname value.Nullable[string]
	State    State
	Balance  decimal.Decimal
}

// MemberImports maps spreadsheet headings onto MemberImport and validates each
// completely parsed row before the handler receives it.
var MemberImports = memberImports()

func memberImports() importer.Definition[MemberImport] {
	text := foundryhttp.StringQuery[string]()
	name := validation.DefineField("name", func(row MemberImport) string { return row.Name })
	return importer.Define(importer.Spec[MemberImport]{
		Columns: []importer.Column[MemberImport]{
			importer.Field("Name", text, func(row *MemberImport, v string) { row.Name = v }),
			importer.NullableField("Nickname", text, func(row *MemberImport, v value.Nullable[string]) { row.Nickname = v }),
			importer.Field("State", foundryhttp.EnumQuery[State, *State](Active.EnumDescriptor()), func(row *MemberImport, v State) { row.State = v }),
			importer.Field("Balance", foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal](), func(row *MemberImport, v decimal.Decimal) { row.Balance = v }),
		},
		Rules: []validation.Rule[MemberImport]{name.Rules(validation.NonBlank[string](), validation.MaxLength[string](80))},
	})
}
