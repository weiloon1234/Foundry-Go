package contractname

import (
	"regexp"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestSchemaNamesAreStableAndKeepFullIdentity(t *testing.T) {
	valid := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	for _, id := range []string{"", "123", "type", "pkg.Model[other.User]", "pkg/a.User", "pkg/b.User", "x.\u2028\""} {
		if !valid.MatchString(Symbol(id)) {
			t.Fatal("invalid target name", id)
		}
	}
	first, err := Schemas([]contract.Type{{ID: "pkg/a.User"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Schemas([]contract.Type{{ID: "pkg/b.User"}, {ID: "pkg/a.User"}})
	if err != nil {
		t.Fatal(err)
	}
	if first["pkg/a.User"] != second["pkg/a.User"] || second["pkg/a.User"] == second["pkg/b.User"] {
		t.Fatal("names depend on catalogue order or lose identity")
	}
}
