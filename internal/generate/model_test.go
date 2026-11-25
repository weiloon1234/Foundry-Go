package generate

import (
	"bytes"
	"flag"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateModelGoldens = flag.Bool("update-model-goldens", false, "refresh generated model declaration goldens")

func TestModelGoldens(t *testing.T) {
	for _, name := range []string{"basic", "exceptions", "keys", "inputs", "constructors"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", "models", name+".go.txt"))
			if err != nil {
				t.Fatal(err)
			}
			dir := fixture(t, string(source))
			if err := os.Rename(filepath.Join(dir, "models.go"), filepath.Join(dir, name+".go")); err != nil {
				t.Fatal(err)
			}
			if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			got := []byte(generatedSnapshot(t, dir)[name+"_foundry.gen.go"])
			golden := filepath.Join("testdata", "models", name+".golden")
			if *updateModelGoldens {
				if err := os.WriteFile(golden, got, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("generated model differs from %s; refresh with -update-model-goldens", golden)
			}
			if report, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil || len(report.Written) != 0 {
				t.Fatalf("repeat: %+v %v", report, err)
			}
			if _, err := generateFixture(t.Context(), Options{Dir: dir, Check: true}); err != nil {
				t.Fatal(err)
			}
			variables, err := modelGoldenVariables(string(source))
			if err != nil {
				t.Fatal(err)
			}
			write(t, dir, "declaration_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/orm")
func TestGeneratedModelDeclarations(t *testing.T){if err:=orm.Check(`+strings.Join(variables, ",")+`);err!=nil{t.Fatal(err)}}`)
			cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-run", "^TestGeneratedModelDeclarations$", "-p", "4", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated model compilation: %v\n%s", err, output)
			}
		})
	}
}

const simpleModel = `package sample
//foundry:model table=accounts var=Accounts key=ID
type Account struct { ID int64; Name string }
`

func TestModelFreshRepeatAndStale(t *testing.T) {
	dir := fixture(t, simpleModel)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"column=label\"`", 1))
	if _, err := generateFixture(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "generated output is stale") {
		t.Fatalf("tag-only stale generation: %v", err)
	}
	if got := generatedSnapshot(t, dir); got["models_foundry.gen.go"] != before["models_foundry.gen.go"] {
		t.Fatal("check changed generated output")
	}
}

func TestModelShapeCompileFailures(t *testing.T) {
	dir := fixture(t, simpleModel)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"added", "removed", "renamed", "retyped", "reordered"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", "models", "compilefail", name+".go.txt"))
			if err != nil {
				t.Fatal(err)
			}
			write(t, dir, "models.go", string(source))
			variables, err := modelGoldenVariables(simpleModel)
			if err != nil {
				t.Fatal(err)
			}
			write(t, dir, "declaration_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/orm")
func TestGeneratedModelDeclarations(t *testing.T){if err:=orm.Check(`+strings.Join(variables, ",")+`);err!=nil{t.Fatal(err)}}`)
			cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-run", "^TestGeneratedModelDeclarations$", "-p", "4", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "models_foundry.gen.go") || !strings.Contains(string(output), "cannot convert Account{}") || !strings.Contains(string(output), "foundryAccountFields") {
				t.Fatalf("wrong compile failure: %v\n%s", err, output)
			}
		})
	}
}

func TestModelTestFileRefusal(t *testing.T) {
	for _, pkg := range []string{"sample", "sample_test"} {
		t.Run(pkg, func(t *testing.T) {
			dir := fixture(t, "package sample\n")
			write(t, dir, "model_test.go", strings.Replace(simpleModel, "package sample", "package "+pkg, 1))
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "model Account: model declarations in _test.go files are not supported; move the model into a non-test Go file") {
				t.Fatalf("test-only directive: %v", err)
			}
		})
	}
}

// declarationFinding is the shape the guide promises for a model-declaration error:
// a position, the generate-declaration rule, a model subject, a problem, a correction
// after a semicolon and the guide reference, on one line.
var declarationFinding = regexp.MustCompile(`^[^\n]+:\d+:\d+: \[generate-declaration\] model [A-Za-z0-9_]+(\.[A-Za-z0-9_]+)?: [^\n]+; [^\n]+\. Foundry guide: model-declarations#diagnose-declarations$`)

func requireDeclarationFinding(t *testing.T, err error) {
	t.Helper()
	if !declarationFinding.MatchString(err.Error()) {
		t.Fatalf("not a complete declaration finding (position, rule, model subject, correction, guide): %q", err.Error())
	}
}

func TestModelRejectsInvalidDeclarations(t *testing.T) {
	fixtures := newRejectionFixtures(t)
	for _, tc := range []struct{ name, source, want string }{
		{"alias", strings.Replace(simpleModel, "type Account struct", "type Account = struct", 1), "model Account: requires a defined struct type"},
		{"generic", strings.Replace(simpleModel, "type Account struct", "type Account[T any] struct", 1), "model Account: generic models are outside the directive; build the table in a generic function using the low-level ORM functions"},
		{"nonstruct", strings.Replace(simpleModel, "struct { ID int64; Name string }", "int", 1), "model Account: requires a struct"},
		{"no mapping", "package sample\n//foundry:model\ntype Account struct{ID int64}\n", "table requires an explicit SQL identifier"},
		{"role on key", strings.Replace(simpleModel, "ID int64", "ID int64 `foundry:\"role=number\"`", 1), "role applies only to non-key fields; remove it"},
		{"input on lookup key", strings.Replace(simpleModel, "ID int64", "ID int64 `foundry:\"input=int,member=ID\"`", 1), "input on a primary key requires key-role=input"},
		{"unknown directive", strings.Replace(simpleModel, "key=ID", "key=ID wrong=x", 1), "model Account: unknown directive option wrong"},
		{"table", strings.Replace(simpleModel, "table=accounts", "table=a-b", 1), "table requires an explicit SQL identifier"},
		{"var", strings.Replace(simpleModel, "var=Accounts", "var=_", 1), "var requires a Go identifier"},
		{"type", strings.Replace(simpleModel, "key=ID", "key=ID type=_", 1), "unknown directive option type"},
		{"key option", strings.Replace(simpleModel, "key=ID", "key=_", 1), "key requires a struct field name"},
		{"key missing", strings.Replace(simpleModel, "key=ID", "key=Other", 1), "key field Other does not exist or is ignored"},
		{"identity", strings.Replace(simpleModel, "key=ID", "key=ID identity=yes", 1), "identity requires false to opt out"},
		{"identity var", strings.Replace(simpleModel, "key=ID", "key=ID identity=false identity-var=ref", 1), "unknown directive option identity-var"},
		{"timestamps", strings.Replace(simpleModel, "key=ID", "key=ID timestamps=yes", 1), "timestamps requires none"},
		{"softdeletes", strings.Replace(simpleModel, "key=ID", "key=ID softdeletes=yes", 1), "softdeletes requires none"},
		{"soft name", strings.Replace(simpleModel, "key=ID", "key=ID soft=_", 1), "unknown directive option soft"},
		{"soft field", strings.Replace(simpleModel, "key=ID", "key=ID soft=Soft", 1), "unknown directive option soft"},
		{"members", strings.Replace(simpleModel, "key=ID", "key=ID members=Table", 1), "unknown directive option members"},
		{"duplicate member", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"member=Table\"`", 1), "member=Table is redundant without a Table method collision; remove it"},
		{"unknown tag", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"what=x\"`", 1), "model Account.Name: unknown foundry option what"},
		{"near miss", strings.Replace(simpleModel, "Name string", "Name string `foudry:\"column=label\"`", 1), "model Account.Name: unknown struct tag foudry; use foundry"},
		{"malformed tag", strings.Replace(simpleModel, "Name string", "Name string `foundry:role=text`", 1), "model Account.Name: struct tag values must be quoted"},
		{"wrong role", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"role=number\"`", 1), "model Account.Name: role=number does not apply to string"},
		{"unsupported", strings.Replace(simpleModel, "Name string", "Name []string", 1), "unsupported stored type []string; declare codec"},
		{"unexported", strings.Replace(simpleModel, "Name string", "name string", 1), "model Account.name: persisted fields must be exported"},
		{"input type", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"input=Missing\"`", 1), "input requires a Go type"},
		{"input method", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"input=string\"`", 1), "input requires MutateName or mutate"},
		{"codec function", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"codec=missing\"`", 1), "codec requires a named function"},
		{"key role nonkey", strings.Replace(simpleModel, "Name string", "Name string `foundry:\"key-role=primary\"`", 1), "key-role applies only to the primary key"},
		{"key role member", strings.Replace(simpleModel, "ID int64", "ID int64 `foundry:\"key-role=primary\"`", 1), "key-role requires member"},
		{"key role type", strings.Replace(simpleModel, "ID int64", "ID int64 `foundry:\"key-role=text,member=ID\"`", 1), "key-role=text does not apply to int64"},
		{"key input", strings.Replace(simpleModel, "ID int64", "ID int64 `foundry:\"key-role=input,member=ID\"`", 1), "key-role=input requires input"},
		{"hooks missing", strings.Replace(simpleModel, "key=ID", "key=ID hooks=missing", 1), "hooks requires a named function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixtures.fixture(t, tc.source)
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "models.go:") {
				t.Fatalf("got %v; want source location and %s", err, tc.want)
			}
			requireDeclarationFinding(t, err)
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid model published output")
			}
		})
	}
}

func TestModelRejectsFieldFamilies(t *testing.T) {
	prefix := `package sample
import (
 "github.com/weiloon1234/Foundry-Go/temporal"
 "github.com/weiloon1234/Foundry-Go/decimal"
 "github.com/weiloon1234/Foundry-Go/value"
 "github.com/weiloon1234/Foundry-Go/database/orm"
 "github.com/weiloon1234/Foundry-Go/database/codec"
 "github.com/weiloon1234/Foundry-Go/database/relation"
 "github.com/weiloon1234/Foundry-Go/database/encrypted"
 "github.com/weiloon1234/Foundry-Go/attachments"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/auth/password"
)
var _ temporal.DateTime
var _ decimal.Decimal
var _ value.Nullable[string]
var _ orm.Hooks[Account]
var _ codec.Codec[string]
var _ relation.One[Account]
var _ encrypted.Text
var _ attachments.One[Account]
var _ model.ID[Account]
var _ password.Hash
`
	fixtures := newRejectionFixtures(t)
	for _, tc := range []struct{ name, field, options, extra, want string }{
		{"decimal", "Amount decimal.Decimal", "", "", "decimal field requires role=column or role=number"},
		{"instant redundant", "At temporal.DateTime `foundry:\"role=time\"`", "", "", "role=time is inferred from the field type; remove it"},
		{"date redundant", "At temporal.Date `foundry:\"role=time\"`", "", "", "role=time is inferred from the field type; remove it"},
		{"managed date", "At temporal.Date `foundry:\"role=created-at\"`", "", "", "role=created-at does not apply to"},
		{"nullable created", "At value.Nullable[temporal.DateTime] `foundry:\"role=created-at\"`", "", "", "role=created-at does not apply to"},
		{"required delete", "At temporal.DateTime `foundry:\"role=soft-delete\"`", "", "", "role=soft-delete does not apply to"},
		{"timestamps disabled", "At temporal.DateTime `foundry:\"role=created-at\"`", "timestamps=none", "", "timestamps=none conflicts with managed timestamps"},
		{"softdelete disabled", "At value.Nullable[temporal.DateTime] `foundry:\"role=soft-delete\"`", "softdeletes=none", "", "softdeletes=none conflicts with soft-delete"},
		{"optional", "Name value.Optional[string]", "", "", "Optional is a write/transport state"},
		{"nested nullable", "Name value.Nullable[value.Nullable[string]]", "", "", "nested Nullable is not a persisted column"},
		{"nullable slot", "Files value.Nullable[attachments.Many[Account]]", "", "", "Nullable does not apply to a slot"},
		{"nullable password input", "Secret value.Nullable[password.Hash] `foundry:\"input=string\"`", "", "", "password input requires codec"},
		{"password input", "Secret password.Hash `foundry:\"input=string\"`", "", "", "password input requires codec"},
		{"plain password input needs codec", "Secret password.Hash `foundry:\"role=column,input=string\"`", "", "func (Account) MutateSecret(s string)(password.Hash,error){return password.Hash{},nil}", "password input requires codec"},
		{"nullable plain password input needs codec", "Secret value.Nullable[password.Hash] `foundry:\"role=column,input=string\"`", "", "func (Account) MutateSecret(s string)(password.Hash,error){return password.Hash{},nil}", "password input requires codec"},
		{"plain password needs codec", "Secret password.Hash `foundry:\"role=column\"`", "", "", "role=column requires codec for password.Hash"},
		{"plain JSON needs codec", "Secret value.JSON[string] `foundry:\"role=column\"`", "", "", "role=column requires codec for value.JSON"},
		{"plain bytes needs codec", "Secret []byte `foundry:\"role=column\"`", "", "", "role=column requires codec for bytes"},
		{"password codec missing", "Secret password.Hash `foundry:\"codec=missing\"`", "", "", "codec requires a named function"},
		{"unsupported relation", "Links relation.Link[int,int]", "", "", "unsupported relation slot type"},
		{"two soft deletes", "First value.Nullable[temporal.DateTime] `foundry:\"role=soft-delete\"`; Second value.Nullable[temporal.DateTime] `foundry:\"role=soft-delete\"`", "", "", "declare only one soft-delete field"},
		{"relation flag", "Parent relation.One[Account] `foundry:\"slot\"`", "", "", "slot conflicts with relation type"},
		{"slot column", "Files attachments.Many[Account] `foundry:\"column=files\"`", "", "", "column does not apply to a slot"},
		{"slot default", "Files attachments.Many[Account] `foundry:\"default=database\"`", "", "", "default does not apply to a slot"},
		{"slot role", "Files attachments.Many[Account] `foundry:\"role=column\"`", "", "", "role does not apply to a slot"},
		{"slot input", "Files attachments.Many[Account] `foundry:\"input=string\"`", "", "", "input does not apply to a slot"},
		{"slot codec", "Files attachments.Many[Account] `foundry:\"codec=missing\"`", "", "", "codec does not apply to a slot"},
		{"attachment owner", "Files attachments.Many[Other]", "", "type Other struct{}", "attachment slot belongs to a different model"},
		{"encrypted input", "Secret encrypted.Text `foundry:\"input=string\"`", "", "", "input does not apply to Encrypted"},
		{"encrypted codec", "Secret encrypted.Text `foundry:\"codec=missing\"`", "", "", "encrypted fields require their envelope constructor"},
		{"bad mutate signature", "Name string `foundry:\"input=string,mutate=normalize\"`", "", "func normalize(v int)(string,error){return \"\",nil}", "mutate has an incompatible signature"},
		{"bad method signature", "Name string `foundry:\"input=string\"`", "", "func (Account) MutateName(v int)(string,error){return \"\",nil}", "MutateName has an incompatible signature"},
		{"missing input", "Name string", "", "func (Account) MutateName(v string)(string,error){return v,nil}", "MutateName requires input"},
		{"conflicting mutator", "Name string `foundry:\"input=string,mutate=normalize\"`", "", "func normalize(v string)(string,error){return v,nil};func (Account) MutateName(v string)(string,error){return v,nil}", "mutate conflicts with MutateName"},
		{"bad codec signature", "Name string `foundry:\"codec=badCodec\"`", "", "func badCodec() codec.Codec[int]{return codec.Signed[int]()}", "codec has an incompatible signature"},
		{"bad hook signature", "Name string", "hooks=badHooks", "func badHooks() string{return \"\"}", "hooks has an incompatible signature"},
		{"bad retrieval signature", "Name string", "retrieval=badHooks", "func badHooks() orm.Hooks[Account]{return orm.Hooks[Account]{}}", "retrieval has an incompatible signature"},
		{"bad scope signature", "Name string", "scopes=badScopes", "func badScopes() []orm.GlobalScope[Account]{return nil}", "scopes has an incompatible signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := prefix + "\n//foundry:model table=accounts var=Accounts key=ID identity=false " + tc.options + "\ntype Account struct{ID int64;" + tc.field + "}\n" + tc.extra
			dir := fixtures.fixture(t, source)
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "models.go:") || !strings.Contains(err.Error(), "model Account") {
				t.Fatalf("got %v; want model source location and %s", err, tc.want)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid model published output")
			}
		})
	}
}

func TestModelRejectsPrimaryKeyTypes(t *testing.T) {
	prefix := `package sample
import (
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
)
var _ model.ID[Account]
var _ value.Nullable[int]
type Other struct{}
//foundry:model table=accounts var=Accounts key=ID identity=false
`
	fixtures := newRejectionFixtures(t)
	for _, tc := range []struct{ field, want string }{
		{"ID value.Nullable[int]", "unsupported primary key type"},
		{"ID []byte", "unsupported primary key type"},
		{"ID model.ID[Other]", "primary model.ID must belong to this model"},
		{"ID int `foundry:\"-\"`", "key field ID does not exist or is ignored"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			dir := fixtures.fixture(t, prefix+"type Account struct {"+tc.field+"}\n")
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "models.go:") || !strings.Contains(err.Error(), "model Account") {
				t.Fatalf("got %v; want model source location and %s", err, tc.want)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid model published output")
			}
		})
	}
}

func TestGeneratedModelCannotBecomeDTO(t *testing.T) {
	for _, field := range []string{"Account Account", "Lookup map[Account]string"} {
		t.Run(field, func(t *testing.T) {
			dir := fixture(t, simpleModel+"\n//foundry:dto\ntype Response struct{"+field+"}\n")
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "persistence model") {
				t.Fatalf("generated model entered DTO: %v", err)
			}
		})
	}
}

func TestModelPrivateNamesAndImportCollisions(t *testing.T) {
	dir := fixture(t, `package sample
//foundry:model table=records var=orm key=ID
type model struct {ID int64; Name string}
`)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["models_foundry.gen.go"]
	if !strings.Contains(output, "orm1.Define") || !strings.Contains(output, "model1.Reference") {
		t.Fatalf("generated imports shadow names:\n%s", output)
	}
}

func TestModelFilenameCollisionKeepsOwnedOutput(t *testing.T) {
	dir := fixture(t, simpleModel+"\n//foundry:dto\ntype Models struct {Name string}\n")
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "produces models_foundry.gen.go") {
		t.Fatalf("filename collision: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("collision published output")
	}
}

func modelGoldenVariables(source string) ([]string, error) {
	var variables []string
	for _, match := range regexp.MustCompile(`var=(\w+)`).FindAllStringSubmatch(source, -1) {
		variables = append(variables, match[1])
	}
	if len(variables) == 0 {
		return nil, safeerror.New("model golden declares no table variables to check")
	}
	return variables, nil
}
func TestModelGoldenRequiresTables(t *testing.T) {
	if _, err := modelGoldenVariables("package sample"); err == nil {
		t.Fatal("empty golden accepted")
	}
}
