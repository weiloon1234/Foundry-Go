package generate

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestModelTagFingerprint(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"unchanged", `foundry:"column=label"`, `foundry:"column=label"`},
		{"changed", `foundry:"column=label"`, `foundry:"column=other"`},
		{"added", "", `foundry:"member=none"`},
		{"removed", `foundry:"column=label"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := func(tag string) string {
				if tag != "" {
					tag = " `" + tag + "`"
				}
				return strings.Replace(simpleModel, "Name string", "Name string"+tag, 1)
			}
			dir := fixture(t, source(tc.before))
			if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			write(t, dir, "models.go", source(tc.after))
			want := "model changed since generation; run foundry generate"
			assertion := `if err==nil||!strings.Contains(err.Error(),` + `"` + want + `"` + `){t.Fatalf("stale guard: %v",err)}`
			if tc.name == "unchanged" {
				assertion = `if err!=nil{t.Fatal(err)};_ = strings.Contains`
			}
			write(t, dir, "guard_test.go", `package sample
import("testing";"strings";"github.com/weiloon1234/Foundry-Go/database/orm")
func TestGuard(t *testing.T){err:=orm.Check(Accounts);`+assertion+`}`)
			cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-p", "4", "-run", "^TestGuard$", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
