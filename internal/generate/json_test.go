package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFreshJSONGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Preferences struct { Theme string; Tags []string }
//foundry:model table=accounts
type Account struct { ID model.ID[Account]; Settings value.JSON[Preferences]; Backup value.Nullable[value.JSON[Preferences]] }
//foundry:projection
type Snapshot struct { Settings value.JSON[Preferences]; Kind query.JSONKind; Backup value.Nullable[value.JSON[Preferences]] }
func selections(input value.JSON[Preferences]) {
 q,f:=QueryAccounts(),AccountFields()
 _ = AccountDraft{}.SetSettings(input).SetBackup(input).ClearBackup()
 _ = q.Where(f.Settings.Contains(input),f.Backup.IsJSONNull())
 _ = q.Where(f.Settings.Properties().Theme.Scalar().Like("%go%"),f.Settings.Properties().Tags.At(-1).Scalar().Eq("go"))
 _ = ProjectSnapshot(q).SelectSettings(f.Settings.Value()).SelectKind(f.Settings.Kind().Value()).SelectBackup(f.Backup.Value())
}

`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("JSON generation changed")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestFreshNestedJSONPathsGeneration(t *testing.T) {
	source := `package sample
import (
 "encoding/json"
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:enum
type Status string
const Active Status = "active"
type Embedded struct { Promoted string }
type Payload struct { Embedded; Next *Payload; Scores [2]int; Map map[int]string; State Status; Count int64 TAG; Maybe *value.Optional[string]; Opaque Opaque }
type Opaque struct { PrivateShape string }
func(Opaque) MarshalJSON()([]byte,error){return json.Marshal("opaque")}
func(*Opaque) UnmarshalJSON([]byte)error{return nil}
//foundry:model table=accounts
type Account struct { ID model.ID[Account]; Settings value.JSON[Payload] }
//foundry:projection
type Snapshot struct { Settings value.JSON[Payload] }
type snapshotAlias struct{}
func usage() {
 f:=AccountFields();p:=f.Settings.Properties()
 _=QueryAccounts().Where(p.Promoted.Scalar().Eq("go"),p.Next.Properties().Promoted.Scalar().Eq("nested"),p.Scores.At(-1).Scalar().Gt(0),p.Map.At(2).Scalar().Eq("go"),p.State.Scalar().Eq(value.Of(Active)),p.Count.Scalar().Eq(42))
 var _ query.RowExpression[Account,value.Nullable[value.JSON[*value.Optional[string]]]]=p.Maybe.JSON()
 selected:=ProjectSnapshot(QueryAccounts()).SelectSettings(f.Settings.Value()).Query()
 source:=query.As[snapshotAlias](selected,"snap")
 _=SnapshotFieldsAt(source.Scope()).Settings.Properties().Promoted.Scalar().Eq("go")
}
`
	source = strings.ReplaceAll(source, "TAG", "`json:\"count,string\"`")
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("nested JSON generation changed")
	}
	output := first["account_foundry.gen.go"]
	if strings.Contains(output, "PrivateShape") || !strings.Contains(output, "Validated(Status.Validate)") {
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "PrivateShape") || strings.Contains(line, "Status") {
				t.Log(line)
			}
		}
		t.Fatal("custom schema or enum ownership lost")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestJSONPathGeneratedLocalsDoNotShadowPayloadTypes(t *testing.T) {
	dir := fixture(t, `package sample
import (
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
type key int
type p struct{ Entries map[key]string }
type f struct{ Value p }
type field struct{ Next f }
type path struct{ Root field }
//foundry:model table=accounts
type Account struct{ ID model.ID[Account]; Settings value.JSON[f]; Data value.JSON[path] }
func usage(){ _=AccountFields().Settings.Properties().Value.Properties().Entries.At(key(1)).Scalar().Eq("go") }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
}

func TestJSONPathRejectsMalformedTagName(t *testing.T) {
	source := `package sample
import (
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Payload struct{ Name string TAG }
//foundry:model table=accounts
type Account struct{ ID model.ID[Account]; Settings value.JSON[Payload] }
`
	source = strings.ReplaceAll(source, "TAG", "`json:\"odd'path\"`")
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "JSON field declaration") {
		t.Fatal(err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("invalid JSON schema published output")
	}
}

func TestJSONPathNamesSeparateFlatAndNestedFields(t *testing.T) {
	dir := fixture(t, `package sample
import (
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Nested struct{ B int }
type Payload struct{ A Nested; AB string; Array []uint16; ArrayElement bool; Map map[int]float32; MapEntry int64 }
//foundry:model table=accounts
type Account struct{ ID model.ID[Account]; Settings value.JSON[Payload] }
func usage(){
 p:=AccountFields().Settings.Properties()
 _=QueryAccounts().Where(p.A.Properties().B.Scalar().Eq(1),p.AB.Scalar().Eq("x"),p.Array.At(0).Scalar().Eq(uint16(2)),p.ArrayElement.Scalar().Eq(value.Of(true)),p.Map.At(1).Scalar().Eq(float32(3)),p.MapEntry.Scalar().Eq(int64(4)))
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("path names changed")
	}
}

func TestJSONPathPropertyTypeSurvivesUnrelatedAddedField(t *testing.T) {
	source := `package sample
import (
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
type Payload struct{ Name string }
//foundry:model table=accounts
type Account struct{ ID model.ID[Account]; Settings value.JSON[Payload] }
func path() Account_Settings_NameJSONPath[Account] {return AccountFields().Settings.Properties().Name}
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	source = strings.Replace(source, "Name string", "A string; Name string", 1)
	if err := os.WriteFile(filepath.Join(dir, "models.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
}
