package modelcheck

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

func TestHelpersAcrossLoadedPackages(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "go.mod" {
			data = []byte(strings.Replace(string(data), "module "+frameworkinfo.ModulePath, "module foundry.test/helpers", 1) + "\nrequire " + frameworkinfo.ModulePath + " v0.0.0\nreplace " + frameworkinfo.ModulePath + " => " + root + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, source := range map[string]string{
		"dbutil/helper.go": `package dbutil
import "github.com/weiloon1234/Foundry-Go/database/orm"
func DefineTable[M any](name string) orm.Table[M,int64] {
 return orm.Define(name, func(b orm.Builder[M], m *M) orm.Table[M,int64] { return orm.Table[M,int64]{} })
}
`,
		"users/users.go": `package users
import "foundry.test/helpers/dbutil"
type User struct{ ID int64 }
var Users = dbutil.DefineTable[User]("users")
func runtimeName() string { return "runtime_users" }
var RuntimeUsers = dbutil.DefineTable[User](runtimeName())
`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		pattern string
		want    int
	}{{"./...", 1}, {"./users", 0}} {
		t.Run(tc.pattern, func(t *testing.T) {
			got, err := Check(t.Context(), dir, []string{tc.pattern})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("findings: %v; want %d", got, tc.want)
			}
			if len(got) != 0 && (got[0].Target != "model User" || got[0].Message != directiveReplacement || got[0].Position.Line != 4 || filepath.Base(got[0].Position.Filename) != "users.go") {
				t.Fatalf("wrong call site: %v", got)
			}
		})
	}
}

func TestHelperReturnedTableName(t *testing.T) {
	const pre = `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
func runtimeName() string { return "runtime_users" }
func declareDual(first, second string) orm.Table[User,int64] {
`
	const first = `orm.Define(first, func(b orm.Builder[User], m *User) orm.Table[User,int64] { return orm.Table[User,int64]{} })`
	const second = `orm.Define(second, func(b orm.Builder[User], m *User) orm.Table[User,int64] { return orm.Table[User,int64]{} })`
	for _, tc := range []struct {
		name, body, args string
		want             int
	}{
		{"returned computed name", "_ = " + first + "; return " + second, `"temporary", runtimeName()`, 0},
		{"returned constant name", "_ = " + first + "; return " + second, `runtimeName(), "users"`, 1},
		{"returned local computed name", "_ = " + first + "; table := " + second + "; return table", `"temporary", runtimeName()`, 0},
		{"ambiguous return keeps reporting safe direction", "table := " + first + "; if runtimeName() != \"\" { table = " + second + " }; return table", `"temporary", runtimeName()`, 1},
		{"multiple returns keep reporting safe direction", "if runtimeName() != \"\" { return " + first + " }; return " + second, `"temporary", runtimeName()`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, file := testAnalysis(t, pre+tc.body+"\n}\nvar Users = declareDual("+tc.args+")")
			got := a.CheckPackage([]*ast.File{file})
			if len(got) != tc.want {
				t.Fatalf("findings: %v; want %d", got, tc.want)
			}
		})
	}
}
