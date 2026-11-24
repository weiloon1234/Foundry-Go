package generate

import (
	"fmt"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestModelDeclarationRules(t *testing.T) {
	prefix := `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/codec"
 "github.com/weiloon1234/Foundry-Go/database/encrypted"
 "github.com/weiloon1234/Foundry-Go/attachments"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/temporal"
 "github.com/weiloon1234/Foundry-Go/value"
)
var _ codec.Codec[string]
var _ encrypted.Text
var _ attachments.Many[Account]
var _ model.ID[Account]
var _ temporal.Interval
var _ value.Nullable[string]
//foundry:enum
type State string
const Enabled State="enabled"
`
	cases := []struct{ name, fields, options, extra, want string }{
		{"redundant identity", "ID int", "identity=true", "", "identity=true is redundant; remove it"},
		{"retired table type", "ID int", "table-type=Fields", "", "unknown directive option table-type"},
		{"text role", "ID int; Name string `foundry:\"role=text\"`", "", "", "role=text is inferred from the field type; remove it"},
		{"number role", "ID int; Count int `foundry:\"role=number\"`", "", "", "role=number is inferred from the field type; remove it"},
		{"bool column role", "ID int; Flag value.Nullable[bool] `foundry:\"role=column\"`", "", "", "role=column is inferred from the field type; remove it"},
		{"input bool column role", "ID int; Flag bool `foundry:\"role=column,input=bool\"`", "", "func(Account) MutateFlag(v bool)(bool,error){return v,nil}", "role=column is inferred from the field type; remove it"},
		{"enum role", "ID int; State State `foundry:\"role=enum\"`", "", "", "role=enum is inferred from the field type; remove it"},
		{"json role", "ID int; Data value.JSON[string] `foundry:\"role=json\"`", "", "", "role=json is inferred from the field type; remove it"},
		{"interval role", "ID int; Duration temporal.Interval `foundry:\"role=interval\"`", "", "", "role=interval is inferred from the field type; remove it"},
		{"slot flag", "ID int; Files attachments.Many[Account] `foundry:\"slot\"`", "", "", "slot is inferred from the field type; remove it"},
		{"method name", "ID int; Name string `foundry:\"input=string,mutate=MutateName\"`", "", "func(Account) MutateName(v string)(string,error){return v,nil}", "mutate names the conventional method; remove it"},
		{"redundant member", "ID int; Name string `foundry:\"member=Name\"`", "", "", "member=Name is redundant; remove it"},
		{"hidden key", "ID int `foundry:\"member=none\"`", "", "", "member=none is redundant on the key; remove it"},
		{"key rename", "ID int `foundry:\"member=Other\"`", "", "", "member=Other is redundant without a Table method collision; remove it"},
		{"number key role", "ID int `foundry:\"member=ID,key-role=number\"`", "", "", "key-role=number is inferred from the key type; remove it"},
		{"text key role", "ID string `foundry:\"member=ID,key-role=text\"`", "", "", "key-role=text is inferred from the key type; remove it"},
		{"lookup key role", "ID model.ID[Account] `foundry:\"member=ID,key-role=lookup\"`", "", "", "key-role=lookup is inferred from the key type; remove it"},
		{"column name", "ID int; Name string `foundry:\"column=name\"`", "", "", "column=name is redundant; remove it"},
		{"duplicate SQL name", "ID int; Name string `foundry:\"column=id\"`", "", "", "column duplicates ID"},
		{"embedded", "ID int; Embedded", "", "type Embedded struct {Name string}", "embedded fields are not supported"},
		{"ignored embedded", "ID int; Embedded `foundry:\"-\"`", "", "type Embedded struct {Name string}", "embedded fields are not supported"},
		{"created default", "ID int; Created temporal.DateTime `foundry:\"role=created-at,default=database\"`", "", "", "managed timestamp roles cannot use default=database"},
		{"updated default", "ID int; Updated temporal.DateTime `foundry:\"role=updated-at,default=database\"`", "", "", "managed timestamp roles cannot use default=database"},
		{"deleted default", "ID int; Deleted value.Nullable[temporal.DateTime] `foundry:\"role=soft-delete,default=database\"`", "", "", "managed timestamp roles cannot use default=database"},
		{"missing updated", "ID int; Created temporal.DateTime `foundry:\"role=created-at\"`", "", "", "declare both role=created-at and role=updated-at"},
		{"missing created", "ID int; Updated temporal.DateTime `foundry:\"role=updated-at\"`", "", "", "declare both role=created-at and role=updated-at"},
		{"duplicate created", "ID int; First temporal.DateTime `foundry:\"role=created-at\"`; Second temporal.DateTime `foundry:\"role=created-at\"`", "", "", "timestamp role is already declared by First"},
		{"created name", "ID int; CreatedAt temporal.DateTime", "", "", "declare role=time for an ordinary time column"},
		{"updated name", "ID int; UpdatedAt value.Nullable[temporal.DateTime]", "", "", "declare role=time for an ordinary time column"},
		{"deleted name", "ID int; DeletedAt value.Nullable[temporal.DateTime]", "", "", "declare role=soft-delete on DeletedAt, or use softdeletes=none on the model"},
		{"noninstant convention", "ID int; CreatedAt string; UpdatedAt string", "", "", "use timestamps=none on the model"},
		{"noninstant deleted", "ID int; DeletedAt string", "", "", "use softdeletes=none on the model"},
		{"encrypted default", "ID int; Secret encrypted.Text `foundry:\"default=database\"`", "", "", "encrypted fields cannot have database defaults"},
		{"encrypted key default", "ID int `foundry:\"default=database\"`; Secret encrypted.Text", "", "", "encrypted fields require an application-assigned primary key"},
		{"table field shadow", "ID int; Table string", "", "", "field collides with the generated Table field; declare member=<Name>"},
		{"table method shadow", "ID int; Query string", "", "", "field collides with a promoted Table method; declare member=<Name>"},
		{"soft member collision", "ID int; Soft bool; Deleted value.Nullable[temporal.DateTime] `foundry:\"role=soft-delete\"`", "", "", "duplicate table member Soft"},
		{"input interface", "ID int; Name string `foundry:\"input=interface{}\"`", "", "", "mutator input must be concrete and non-null"},
		{"input nullable", "ID int; Name string `foundry:\"input=value.Nullable[string]\"`", "", "", "mutator input must be concrete and non-null"},
		{"pointer mutator", "ID int; Name string `foundry:\"input=string\"`", "", "func(*Account) MutateName(v string)(string,error){return v,nil}", "requires a value receiver"},
		{"orphan mutator", "ID int", "", "func(Account) MutateMissing(v string)(string,error){return v,nil}", "targets no persisted field"},
		{"ignored mutator", "ID int; Name string `foundry:\"-\"`", "", "func(Account) MutateName(v string)(string,error){return v,nil}", "targets no persisted field"},
		{"slot accessor", "ID int; Files attachments.Many[Account]", "", "func(Account) AccessFiles()(string,error){return \"\",nil}", "targets no persisted field"},
		{"pointer accessor", "ID int; Name string", "", "func(*Account) AccessName()(string,error){return \"\",nil}", "requires a value receiver"},
		{"accessor signature", "ID int; Name string", "", "func(Account) AccessName()(interface{},error){return nil,nil}", "requires signature () (Result, error) with a concrete result"},
		{"bool key", "ID bool", "", "", "unsupported primary key type bool"},
		{"float key", "ID float64", "", "", "unsupported primary key type float64"},
		{"codec column role", "ID int; Name string `foundry:\"role=column,codec=stringCodec\"`", "", "func stringCodec()codec.Codec[string]{return codec.String[string]()}", "role=column is implied by codec; remove it"},
		{"codec capability role", "ID int; Name string `foundry:\"role=text,codec=stringCodec\"`", "", "func stringCodec()codec.Codec[string]{return codec.String[string]()}", "codec selects a plain column; remove role=text"},
	}
	fixtures := newRejectionFixtures(t)
	location := regexp.MustCompile(`models.go:\d+:\d+: model Account`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := prefix + "\n//foundry:model table=accounts var=Accounts key=ID " + tc.options + "\ntype Account struct{" + tc.fields + "}\n" + tc.extra
			dir := fixtures.fixture(t, source)
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !location.MatchString(err.Error()) {
				t.Fatalf("got %v; want source/model location and %s", err, tc.want)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid model published output")
			}
		})
	}
}

func TestModelColumnLimit(t *testing.T) {
	typ := types.NewNamed(types.NewTypeName(token.NoPos, nil, "Account", nil), types.NewStruct(nil, nil), nil)
	d := modelDeclaration{name: "Account", typ: typ}
	for i := 0; i <= querycore.MaxExpressionNodes; i++ {
		f := types.NewVar(token.NoPos, nil, fmt.Sprintf("Field%d", i), types.Typ[types.Int])
		d.fields = append(d.fields, modelField{field: f, stored: f.Type()})
	}
	err := validateModel(&packageInput{fset: token.NewFileSet()}, &d)
	if err == nil || !strings.Contains(err.Error(), "persisted column count exceeds ORM declaration limit") {
		t.Fatalf("column bound: %v", err)
	}
}
