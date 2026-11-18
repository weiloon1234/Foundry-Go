package routestyle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

func TestEligibilityUsesDeclarationModuleAndDirective(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, source string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	module := func(path string) string {
		dir := t.TempDir()
		text := strings.Replace(string(mod), "module "+frameworkinfo.ModulePath, "module "+path, 1)
		text += "\nrequire " + frameworkinfo.ModulePath + " v0.0.0\nreplace " + frameworkinfo.ModulePath + " => " + root + "\n"
		write(dir, "go.mod", text)
		write(dir, "go.sum", string(sum))
		return dir
	}
	dependency := module("example/dependency")
	write(dependency, "dto.go", "package dependency\n//foundry:dto\ntype Outside struct {Name string}\n")
	if _, err := generate.Generate(t.Context(), generate.Options{Dir: dependency}); err != nil {
		t.Fatal(err)
	}
	appPath := frameworkinfo.ModulePath + "/fixture-app"
	app := module(appPath)
	appMod, err := os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	write(app, "go.mod", string(appMod)+"\nrequire example/dependency v0.0.0\nreplace example/dependency => "+dependency+"\n")
	write(app, "requests/dto.go", "package requests\n//foundry:dto\ntype SaveRequest struct {Name string}\n//foundry:message key=greeting\ntype Greeting struct {Name string}\n")
	if _, err := generate.Generate(t.Context(), generate.Options{Dir: app, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	write(app, "routes/routes.go", `package routes
 import (
  "`+appPath+`/requests"
  "example/dependency"
  h "`+frameworkinfo.ModulePath+`/http"
  "`+frameworkinfo.ModulePath+`/datatable"
 )
 func declarations(){
  _=h.JSONBody(requests.SaveRequestJSON())
  _=h.JSONBody(requests.GreetingJSON())
  _=h.JSONBody(dependency.OutsideJSON())
  _=h.JSONBody(datatable.RequestJSON())
 }
 `)
	for _, pattern := range []string{"./routes", "./..."} {
		got, err := Check(t.Context(), app, []string{pattern})
		if err != nil {
			t.Fatal(pattern, err)
		}
		if len(got) != 1 || got[0].Code != "route-helper-missing" || !strings.Contains(got[0].Message, "SaveBody") {
			t.Fatalf("%s: %v", pattern, got)
		}
	}
}
