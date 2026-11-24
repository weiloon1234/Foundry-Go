package modelconvert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProofTableContract(t *testing.T) {
	const method = "func (t UserTable) Custom() int { return 1 }"
	for _, tc := range []struct {
		name, old, new, want string
		method               bool
	}{
		{name: "renamed member", old: ";Name orm.Text", new: ";Label orm.Text", want: ".name"},
		{name: "reordered members", old: "orm.Table[User, model.ID[User]];Name orm.Text[User,string]", new: "Name orm.Text[User,string];orm.Table[User, model.ID[User]]"},
		{name: "exported variable type", old: "UserTable", new: "OtherUserTable", want: "Users.variable_type"},
		{name: "removed table method", method: true, want: "Users.methods.Custom"},
		{name: "added table method", want: "Users.methods.Custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := string(read(t, "plain.before"))
			if tc.method {
				source += "\n" + method + "\n"
			}
			plan := analyze(t, fixture(t, source))
			if len(plan.Refused) > 0 {
				t.Fatal(plan.Refused)
			}
			generator := GeneratorFunc(func(_ context.Context, stage string) error {
				data := string(read(t, "plain.generated"))
				if tc.old != "" {
					data = strings.ReplaceAll(data, tc.old, tc.new)
				}
				if tc.name == "reordered members" {
					data = strings.Replace(data, "UserTable{orm.TableOf(b,&u.ID),orm.TextOf(b,&u.Name)}", "UserTable{orm.TextOf(b,&u.Name),orm.TableOf(b,&u.ID)}", 1)
				}
				if tc.name == "added table method" {
					data += "\n" + method + "\n"
				}
				if tc.method {
					path := filepath.Join(stage, "models.go")
					contents, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if err = os.WriteFile(path, []byte(strings.Replace(string(contents), method, "", 1)), 0600); err != nil {
						return err
					}
				}
				return os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), []byte(data), 0600)
			})
			_, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "models.go:") || !strings.Contains(err.Error(), "hand-written") || !strings.Contains(err.Error(), "generated") {
				t.Fatalf("want located difference and both values for %s, got %v", tc.want, err)
			}
		})
	}
}

func TestProofKeyCapabilities(t *testing.T) {
	const source = `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Record struct {ID int64}
type RecordTable struct {orm.Table[Record,int64]; ID orm.Key[Record,int64]}
var Records=orm.Define("records",func(b orm.Builder[Record],r *Record)RecordTable{table:=orm.TableOf(b,&r.ID);return RecordTable{table,table.Key()}})
`
	for _, tc := range []struct{ name, carrier, binding string }{
		{"primary", "orm.Primary[Record,int64]", "orm.PrimaryOf(b,table.Key())"},
		{"input", "orm.InputKey[Record,string,int64]", "orm.InputKeyOf(b,table.Key(),func(string)(int64,error){return 1,nil})"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := analyze(t, fixture(t, source))
			if len(plan.Refused) > 0 {
				t.Fatal(plan.Refused)
			}
			generator := GeneratorFunc(func(_ context.Context, stage string) error {
				generated := strings.Replace(source, "type Record struct {ID int64}\n", "", 1)
				generated = strings.Replace(generated, "orm.Key[Record,int64]", tc.carrier, 1)
				generated = strings.Replace(generated, "return RecordTable{table,table.Key()}", "return RecordTable{table,"+tc.binding+"}", 1)
				return os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), []byte(generated), 0600)
			})
			_, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), "key_capability") {
				t.Fatalf("changed lookup to %s: %v", tc.name, err)
			}
		})
	}
}

func TestNamingRefusalsAndBareTable(t *testing.T) {
	base := string(read(t, "plain.before"))
	for _, tc := range []struct{ name, source, want string }{
		{"table type", strings.ReplaceAll(base, "UserTable", "UserColumns"), "rename table type UserColumns to UserTable"},
		{"member", strings.Replace(base, " Name orm.Text", " Label orm.Text", 1), "rename table member UserTable.Label to UserTable.Name"},
		{"bare", `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ID int64}
var Users=orm.Define("users",func(b orm.Builder[User],u *User)orm.Table[User,int64]{return orm.TableOf(b,&u.ID)})`, "bare orm.Table"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := analyze(t, fixture(t, tc.source))
			if len(plan.Files) != 0 || len(plan.Refused) != 1 || !strings.Contains(plan.Refused[0].Reason, tc.want) {
				t.Fatalf("want %s: %+v", tc.want, plan.Refused)
			}
			if tc.name == "bare" && len(plan.BareTables) != 1 {
				t.Fatal(plan.BareTables)
			}
		})
	}
}

func TestUnboundGenericAndLocalTables(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"generic", `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Record[T any] struct{ID int64}
type RecordTable[T any] struct{orm.Table[Record[T],int64]}
func Records[T any]()RecordTable[T]{return orm.Define("records",func(b orm.Builder[Record[T]],r *Record[T])RecordTable[T]{return RecordTable[T]{orm.TableOf(b,&r.ID)}})}`, "outside the directive: generic model"},
		{"local", `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
func example(){type Record struct{ID int64};type RecordTable struct{orm.Table[Record,int64]};_ = orm.Define("records",func(b orm.Builder[Record],r *Record)RecordTable{return RecordTable{orm.TableOf(b,&r.ID)}})}`, "no package-level binding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := analyze(t, fixture(t, tc.source))
			if tc.name == "generic" {
				if len(plan.Refused) != 0 || len(plan.OutsideDirective) != 1 || !strings.Contains(plan.OutsideDirective[0].Reason, tc.want) {
					t.Fatalf("generic model not outside directive: %+v", plan)
				}
			} else if len(plan.Refused) != 1 || !strings.Contains(plan.Refused[0].Reason, tc.want) {
				t.Fatalf("unreported declaration: %+v", plan.Refused)
			}
		})
	}
}

func TestInferredDecimalKeyRole(t *testing.T) {
	source := `package models
import("github.com/weiloon1234/Foundry-Go/database/orm";"github.com/weiloon1234/Foundry-Go/decimal")
type Record struct{ID decimal.Decimal}
type RecordTable struct{orm.Table[Record,decimal.Decimal];ID orm.NumberLookupKey[Record,decimal.Decimal]}
var Records=orm.Define("records",func(b orm.Builder[Record],r *Record)RecordTable{table:=orm.TableOf(b,&r.ID);return RecordTable{table,orm.NumberKey(table.Key())}})`
	plan := analyze(t, fixture(t, source))
	if len(plan.Refused) > 0 || len(plan.Models) != 1 {
		t.Fatalf("refused: %+v", plan.Refused)
	}
	if plan.Models[0].Options["key-role"] != 0 {
		t.Fatalf("redundant inferred key role: %s", plan.Files[0].After)
	}
}

func TestCollisionPreservesExistingMember(t *testing.T) {
	for _, tc := range []struct{ field, member string }{
		{"Key", "KeyName"},
		{"Query", "QueryFields"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			source := `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Record struct{ID int64;FIELD string}
type RecordTable struct{orm.Table[Record,int64];MEMBER orm.Text[Record,string]}
var Records=orm.Define("records",func(b orm.Builder[Record],r *Record)RecordTable{return RecordTable{orm.TableOf(b,&r.ID),orm.TextOf(b,&r.FIELD)}})`
			source = strings.NewReplacer("FIELD", tc.field, "MEMBER", tc.member).Replace(source)
			plan := analyze(t, fixture(t, source))
			if len(plan.Refused) != 0 || len(plan.Models) != 1 {
				t.Fatal(plan.Refused)
			}
			if !strings.Contains(string(plan.Files[0].After), `foundry:"member=`+tc.member+`"`) {
				t.Fatalf("missing collision member: %s", plan.Files[0].After)
			}
		})
	}
}
