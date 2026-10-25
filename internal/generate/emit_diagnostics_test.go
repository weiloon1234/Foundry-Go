package generate

import (
	"go/types"
	"strings"
	"testing"
)

func TestGeneratedDiagnosticOwnership(t *testing.T) {
	for _, test := range []struct {
		name, path string
		owned      bool
	}{
		{"framework model", framework + "/countries", true},
		{"framework internal", framework + "/internal/store", true},
		{"consumer", "example.test/app", false},
		{"similar module", framework + "-other/models", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newEmitter(&packageInput{path: test.path, name: "models", types: types.NewPackage(test.path, "models")})
			f, format := e.use(framework+"/fault"), e.use("fmt")
			e.line("// Check preserves its cause.\nfunc Check(err error) error { if err != nil { return %s.Errorf(\"compare Model.Field: %%w\", err) }; return %s.New(%s.Invalid, \"invalid model\") }", format, f, f)
			source, err := e.finish("model.go")
			if err != nil {
				t.Fatal(err)
			}
			text := string(source)
			if test.owned {
				if !strings.Contains(text, "foundrydiagnostic.NewFault(") || !strings.Contains(text, `foundrydiagnostic.Prefix("compare Model.Field", err)`) || strings.Contains(text, `"fmt"`) {
					t.Fatal(text)
				}
			} else if strings.Contains(text, "/internal/safeerror") || !strings.Contains(text, "fmt.Errorf(") || !strings.Contains(text, "foundryfault.New(") {
				t.Fatal(text)
			}
			if !strings.Contains(text, "// Check preserves its cause.") {
				t.Fatal("lost generated documentation")
			}
		})
	}
}
