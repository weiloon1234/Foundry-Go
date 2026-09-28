package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const configurationSource = `package sample
import (
 "time"
 "net/netip"
 "github.com/weiloon1234/Foundry-Go/secret"
)
type Port uint16
type config bool
type Region struct { Name string }
func(r *Region)UnmarshalText(raw []byte)error{r.Name=string(raw);return nil}
//foundry:enum
type Mode string
const Live Mode = "live"
type Transport struct {
 Port Port
 Timeout time.Duration
}

//foundry:config
type Settings struct {
 HTTP Transport ` + "`config:\"http\"`" + `
 Mode Mode
 Region Region
 Credential secret.String
 SecretGroup struct { User string; Password string } ` + "`config:\"credentials,secret\"`" + `
 Networks []netip.Prefix
 Items []string
 Weights map[string]uint16
 Extra struct { Count int ` + "`json:\"count\"`" + ` } ` + "`config:\"extra,json\"`" + `
 Hidden int ` + "`config:\"-\"`" + `
}
var keys = SettingsConfigKeys()
`

func TestGeneratedConfigurationFreshDeterministicAndRuntime(t *testing.T) {
	dir := fixture(t, configurationSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fresh check: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("read-only check published output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{"SettingsConfigKeys()", "SettingsHTTPConfigKeySet", "config1.Key[Settings, Port]", "return &settings.HTTP.Port", "config1.Enum[Settings, Mode]", "config1.Text[Settings, Region, *Region]", "Sensitive()"} {
		if !strings.Contains(first["settings_foundry.gen.go"], want) {
			t.Fatalf("generated config missing %q", want)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("configuration generation changed on repeat")
	}
	write(t, dir, "config_runtime_test.go", `package sample_test
import (
 "strings"
 "testing"
 "time"
 sample "foundry.test/generator"
 "github.com/weiloon1234/Foundry-Go/config"
 "github.com/weiloon1234/Foundry-Go/config/toml"
)
func TestLoad(t *testing.T){
 schema,err:=sample.SettingsConfigSchema();if err!=nil{t.Fatal(err)}
 file,err:=toml.Decode(strings.NewReader("mode='live'\nregion='asia'\ncredential='hidden'\nitems=['a']\n[http]\nport=65535\ntimeout='3s'\n[credentials]\nuser='private'\npassword='hidden'\n[weights]\nlarge=65535\n[extra]\ncount=4\n"),schema,toml.Options{Name:"test"});if err!=nil{t.Fatal(err)}
 keys:=sample.SettingsConfigKeys()
 got,report,err:=schema.Load(sample.Settings{},config.Inputs[sample.Settings]{Files:[]config.Values{file},Overrides:[]config.Override[sample.Settings]{keys.HTTP.Timeout.Set(9*time.Second)}})
 if err!=nil{t.Fatal(err)}
 if got.HTTP.Port!=65535||got.HTTP.Timeout!=9*time.Second||got.Region.Name!="asia"||got.Credential.Reveal()!="hidden"||got.Extra.Count!=4||got.Weights["large"]!=65535{t.Fatal("generated config lost values/types")}
 secrets:=0;for _,entry:=range report.Entries(){if entry.Secret{secrets++}}
 if secrets!=3{t.Fatalf("lost secret provenance: %d",secrets)}
 for key,value:=range map[string]string{"http.port":"65536","mode":"other","weights":"{\"large\":65536}","extra":"{\"unknown\":1}"}{
   _,report,err:=schema.Load(sample.Settings{Mode:sample.Live},config.Inputs[sample.Settings]{Files:[]config.Values{{Name:"test",Data:map[string]string{key:value}}}})
   if err==nil||len(report.Entries())!=0{t.Fatalf("invalid %s accepted",key)}
 }
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-race", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated configuration runtime: %v\n%s", err, output)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "models.go", strings.Replace(configurationSource, `config:"http"`, `config:"server"`, 1))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("renamed configuration key not stale: %v", err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("stale check modified generated configuration")
	}
}

func TestConfigurationGenerationAcrossPackageGraph(t *testing.T) {
	dir := fixture(t, `package sample
import "foundry.test/generator/child"
//foundry:config
type Settings struct{ Child child.Options }
var options=child.OptionsConfigKeys()
var settings=SettingsConfigKeys()
`)
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, child, "settings.go", `package child
//foundry:enum
type Mode string
const Live Mode="live"
//foundry:config
type Options struct{ Mode Mode }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true, Check: true}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "settings_foundry.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "child.Mode") || !strings.Contains(string(content), "EnumDescriptor()") {
		t.Fatal("imported enum configuration lost its typed membership")
	}
}

func TestInvalidConfigurationDeclarationsDoNotPublish(t *testing.T) {
	for name, source := range map[string]string{
		"not a struct":           "//foundry:config\ntype Settings string",
		"generic":                "//foundry:config\ntype Settings[T any] struct{ Value T }",
		"alias":                  "type Original struct{}\n//foundry:config\ntype Settings = Original",
		"directive options":      "//foundry:config format=toml\ntype Settings struct{}",
		"private":                "//foundry:config\ntype Settings struct{ private string }",
		"embedded":               "type Child struct{Port int}\n//foundry:config\ntype Settings struct{Child}",
		"pointer":                "//foundry:config\ntype Settings struct{Value *int}",
		"interface":              "//foundry:config\ntype Settings struct{Value any}",
		"func":                   "//foundry:config\ntype Settings struct{Value func()}",
		"map key":                "//foundry:config\ntype Settings struct{Value map[int]string}",
		"map dynamic":            "//foundry:config\ntype Settings struct{Value map[string]any}",
		"invalid tag":            "//foundry:config\ntype Settings struct{Value string `config:\"bad-name\"`}",
		"invalid option":         "//foundry:config\ntype Settings struct{Value string `config:\"value,unknown\"`}",
		"duplicate option":       "//foundry:config\ntype Settings struct{Value string `config:\"value,secret,secret\"`}",
		"duplicate key":          "//foundry:config\ntype Settings struct{One string `config:\"same\"`;Two string `config:\"same\"`}",
		"environment collision":  "//foundry:config\ntype Settings struct{One string `config:\"a.b\"`;Two string `config:\"a__b\"`}",
		"namespace collision":    "//foundry:config\ntype Settings struct{One string `config:\"a\"`;Two string `config:\"a.b\"`}",
		"empty group":            "//foundry:config\ntype Settings struct{Group struct{}}",
		"group symbol collision": "//foundry:config\ntype Settings struct{AB struct{C int};A struct{B struct{C int}}}",
		"handwritten collision":  "//foundry:config\ntype Settings struct{}\nfunc SettingsConfigKeys(){}",
		"text decoder":           "type Code string\nfunc(*Code)UnmarshalText(string)error{return nil}\n//foundry:config\ntype Settings struct{Value Code}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil {
				t.Fatal("invalid configuration generated")
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid configuration published files")
			}
		})
	}
}
