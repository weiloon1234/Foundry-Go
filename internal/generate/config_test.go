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
		"not a struct":                    "//foundry:config\ntype Settings string",
		"generic":                         "//foundry:config\ntype Settings[T any] struct{ Value T }",
		"alias":                           "type Original struct{}\n//foundry:config\ntype Settings = Original",
		"directive options":               "//foundry:config format=toml\ntype Settings struct{}",
		"private":                         "//foundry:config\ntype Settings struct{ private string }",
		"embedded":                        "type Child struct{Port int}\n//foundry:config\ntype Settings struct{Child}",
		"pointer":                         "//foundry:config\ntype Settings struct{Value *int}",
		"interface":                       "//foundry:config\ntype Settings struct{Value any}",
		"func":                            "//foundry:config\ntype Settings struct{Value func()}",
		"map key":                         "//foundry:config\ntype Settings struct{Value map[int]string}",
		"map dynamic":                     "//foundry:config\ntype Settings struct{Value map[string]any}",
		"invalid tag":                     "//foundry:config\ntype Settings struct{Value string `config:\"bad-name\"`}",
		"invalid option":                  "//foundry:config\ntype Settings struct{Value string `config:\"value,unknown\"`}",
		"duplicate option":                "//foundry:config\ntype Settings struct{Value string `config:\"value,secret,secret\"`}",
		"duplicate key":                   "//foundry:config\ntype Settings struct{One string `config:\"same\"`;Two string `config:\"same\"`}",
		"environment collision":           "//foundry:config\ntype Settings struct{One string `config:\"a.b\"`;Two string `config:\"a__b\"`}",
		"namespace collision":             "//foundry:config\ntype Settings struct{One string `config:\"a\"`;Two string `config:\"a.b\"`}",
		"empty group":                     "//foundry:config\ntype Settings struct{Group struct{}}",
		"group symbol collision":          "//foundry:config\ntype Settings struct{AB struct{C int};A struct{B struct{C int}}}",
		"handwritten collision":           "//foundry:config\ntype Settings struct{}\nfunc SettingsConfigKeys(){}",
		"text decoder":                    "type Code string\nfunc(*Code)UnmarshalText(string)error{return nil}\n//foundry:config\ntype Settings struct{Value Code}",
		"public secret tag":               "//foundry:config\ntype Settings struct{ Password string `config:\"pass,public,secret\" client:\"env=PASS\"` }",
		"public missing env":              "//foundry:config\ntype Settings struct{ Name string `config:\"name,public\"` }",
		"public non-scalar":               "//foundry:config\ntype Settings struct{ List []string `config:\"list,public\" client:\"env=LIST\"` }",
		"public unknown kind":             "//foundry:config\ntype Settings struct{ Name string `config:\"name,public\" client:\"env=NAME,kind=unknown\"` }",
		"public enum non-enum":            "//foundry:config\ntype Settings struct{ Name string `config:\"name,public\" client:\"env=NAME,kind=enum\"` }",
		"public locale non-string":        "//foundry:config\ntype Settings struct{ Count int `config:\"count,public\" client:\"env=COUNT,kind=locale\"` }",
		"public format locale non-string": "//foundry:config\ntype Settings struct{ Count int `config:\"count,public\" client:\"env=COUNT,kind=format_locale\"` }",
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

func TestPublicConfigGeneration(t *testing.T) {
	src := `package sample

//foundry:config
type Settings struct {
	// Product name shown in portal chrome.
	AppName string ` + "`config:\"app_name,public\" client:\"env=VITE_APP_NAME,fallback=Foundry,max=60\"`" + `
	// Display locale.
	Locale string ` + "`config:\"locale,public\" client:\"env=VITE_LOCALE,kind=locale\"`" + `
	// Formatting locale independent of the catalog.
	FormatLocale string ` + "`config:\"format_locale,public\" client:\"env=VITE_FORMAT_LOCALE,kind=format_locale,optional\"`" + `
	// Default currency.
	Currency string ` + "`config:\"currency,public\" client:\"env=VITE_CURRENCY,fallback=USD,kind=currency\"`" + `
	// Display time zone.
	TimeZone string ` + "`config:\"time_zone,public\" client:\"env=VITE_TIME_ZONE,kind=time_zone,optional\"`" + `
}
`
	dir := fixture(t, src)
	report, err := Generate(t.Context(), Options{Dir: dir})
	if err != nil {
		t.Fatalf("unexpected generation error: %v", err)
	}
	if len(report.Written) == 0 {
		t.Fatal("no files written")
	}
	content := string(generatedSnapshot(t, dir)["settings_foundry.gen.go"])
	for _, want := range []string{
		"SettingsPublicConfig() []config.PublicSetting",
		`Name: "appName"`, `Env: "VITE_APP_NAME"`, `Description: "Product name shown in portal chrome."`, `Fallback: "Foundry"`, `MaxLength: 60`, `Kind: config.PublicString`,
		`Name: "locale"`, `Env: "VITE_LOCALE"`, `Kind: config.PublicLocale`,
		`Name: "formatLocale", Env: "VITE_FORMAT_LOCALE", Description: "Formatting locale independent of the catalog.", Fallback: "", Optional: true, Kind: config.PublicFormatLocale`,
		`Name: "currency"`, `Env: "VITE_CURRENCY"`, `Fallback: "USD"`, `Kind: config.PublicCurrency`,
		`Name: "timeZone", Env: "VITE_TIME_ZONE", Description: "Display time zone.", Fallback: "", Optional: true, Kind: config.PublicTimeZone`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("generated settings missing %q\nFull content:\n%s", want, content)
		}
	}
}

func TestPublicConfigRejectsInvalidClientFlags(t *testing.T) {
	for _, test := range []struct {
		tag  string
		want string
	}{
		{"env=VITE_FORMAT_LOCALE,kind=format_locale,optional,fallback=en-MY", "optional public config field cannot declare a fallback"},
		{"env=VITE_ZONE,kind=time_zone,optional,fallback=UTC", "optional public config field cannot declare a fallback"},
		{"env=VITE_ZONE,fallback=UTC,optional", "optional public config field cannot declare a fallback"},
		{"env=VITE_ZONE,unknown", "invalid client tag option"},
		{"env=VITE_ZONE,unknown=true", "unknown client tag option"},
		{"env=VITE_ZONE,optional=true", "unknown client tag option"},
	} {
		t.Run(test.tag, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:config\ntype Settings struct { Zone string `config:\"zone,public\" client:\""+test.tag+"\"` }\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid client flag published output")
			}
		})
	}
}

func TestPublicConfigUsesAuthoritativeEnumCases(t *testing.T) {
	dir := fixture(t, `package sample
import "foundry.test/generator/shared"
//foundry:enum
type Mode string
const (
 Live Mode = "li\"ve"
 Alias = Live
 //foundry:ignore
 Ignored Mode = "ignored"
)
//foundry:config
type Settings struct {
 Local Mode `+"`config:\"local,public\" client:\"env=VITE_LOCAL\"`"+`
 Imported shared.Mode `+"`config:\"imported,public\" client:\"env=VITE_IMPORTED,kind=enum\"`"+`
 Wide shared.Wide `+"`config:\"wide,public\" client:\"env=VITE_WIDE\"`"+`
}
`)
	child := filepath.Join(dir, "shared")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, child, "mode.go", `package shared
//foundry:enum
type Mode string
const (
 Live Mode = "li\"ve"
 Alias = Live
 //foundry:ignore
 Ignored Mode = "ignored"
)
//foundry:enum
type Wide uint64
const Large Wide = 18446744073709551615
`)
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "public_runtime_test.go", `package sample
import("testing";"reflect";"github.com/weiloon1234/Foundry-Go/config")
func TestPublicEnum(t *testing.T){
 settings:=SettingsPublicConfig()
 for _,setting:=range settings {
  want:=[]string{"li\"ve"};if setting.Name=="wide"{want=[]string{"18446744073709551615"}}
  if setting.Kind!=config.PublicEnum||!reflect.DeepEqual(setting.EnumCases,want){t.Errorf("%s: kind=%s cases=%q, want enum %q",setting.Name,setting.Kind,setting.EnumCases,want)}
 }
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-run", "^TestPublicEnum$", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("public enum config: %v\n%s", err, output)
	}
}
