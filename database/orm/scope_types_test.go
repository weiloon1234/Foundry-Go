package orm

import (
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRelationAndProjectionTypesRejectWrongOwners(t *testing.T) {
	command := exec.Command("go", "list", "-export", "-deps", "-json", "./rows")
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	exports := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for {
		var pkg struct{ ImportPath, Export string }
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		exports[pkg.ImportPath] = pkg.Export
	}
	const base = `package consumer
 import (
 "github.com/weiloon1234/Foundry-Go/database/orm"
 "github.com/weiloon1234/Foundry-Go/database/orm/expr"
 "github.com/weiloon1234/Foundry-Go/database/orm/rows"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/value"
 )
 type User struct{}
 type Order struct{}
 type L struct{}
 type R struct{}
 var uq orm.Query[User]
 var oq orm.Query[Order]
 var uid orm.Key[User,model.ID[User]]
 var oid orm.Key[Order,model.ID[Order]]
 var fk orm.Column[Order,model.ID[User]]
 var nfk orm.NullableColumn[Order,model.ID[User]]
 var bytes orm.Column[Order,[]byte]
 var _=value.Null[string]()
 var slot orm.One[Order,User]
 var text rows.Field[Order,string]
 var member=rows.As[L](uq,"m")
 var order=rows.As[R](oq,"o")
 var joined=rows.InnerJoin(member,order,rows.On(expr.At(rows.Scope(member),orm.KeyExpr(uid)),expr.At(rows.Scope(order),orm.ColumnExpr(fk))))
 var _=orm.BelongsToOf(slot,orm.ColumnExpr(fk),orm.KeyExpr(uid))
 var _=text
 var _=oid
 `
	for _, tc := range []struct {
		name, extra string
		invalid     bool
	}{
		{"nullable join", `var _=rows.On(expr.At(rows.Scope(order),orm.NullableColumnExpr(nfk)),expr.At(rows.Scope(member),orm.KeyExpr(uid)));var _=expr.At(rows.Scope(order),orm.NullableColumnExpr(nfk)).Eq(model.ID[User]{})`, false},
		{"nullable null comparison", `var _=expr.At(rows.Scope(order),orm.NullableColumnExpr(nfk)).Eq(value.Null[model.ID[User]]())`, true},
		{"noncomparable join", `var _=rows.On(expr.At(rows.Scope(order),orm.ColumnExpr(bytes)),expr.At(rows.Scope(order),orm.ColumnExpr(bytes)))`, true},
		{"joined right", `var _=rows.InnerJoin(member,joined,rows.JoinOn[rows.Alias[L,User],rows.Inner[rows.Alias[L,User],rows.Alias[R,Order]]]{})`, true},
		{"returned column values", `
 var table orm.Table[User,int]
 var texts orm.Table[User,string]
 var _ = orm.KeyEq(table.Key(), 1)
 var _ = orm.NumberKeyGt(orm.NumberKey(table.Key()), 1)
 var _ = orm.TextContains(orm.TextKey(texts.Key()), "x")
 var _ = orm.NumberSet((func()orm.Number[User,int]{ return orm.Number[User,int]{} })(), 1)
 var _ = orm.NullableTextSetNull((func()orm.NullableText[User,string]{ return orm.NullableText[User,string]{} })())
 var _ = orm.Lock(table.Query().Where(orm.KeyEq(table.Key(), 1)).Limit(1))
 `, false},
		{"column owner conversion", `var _ = orm.Number[Order,int](orm.Number[User,int]{})`, true},
		{"enum capability conversion", `var _ = orm.Number[User,int](orm.Enum[User,int]{})`, true},
		{"nullable capability conversion", `var _ = orm.NullableText[User,string](orm.NullableEnum[User,string]{})`, true},
		{"valid", `var _=expr.At(rows.RightScope(joined,rows.Scope(order)),orm.KeyExpr(oid))`, false},
		{"wrong model", `var _=expr.At(rows.Scope(member),orm.KeyExpr(oid))`, true},
		{"wrong side", `var _=rows.LeftScope(joined,rows.Scope(order))`, true},
		{"wrong row value", `var _=rows.Map(text,expr.At(rows.Scope(order),orm.KeyExpr(oid)))`, true},
		{"relation local model", `var _=orm.BelongsToOf(slot,orm.KeyExpr(uid),orm.KeyExpr(uid))`, true},
		{"relation foreign model", `var many orm.Many[User,Order]; var _=orm.HasManyOf(many,orm.KeyExpr(uid),orm.KeyExpr(uid))`, true},
		{"relation nullable model", `var local orm.NullableColumn[User,model.ID[User]]; var _=orm.BelongsToOf(slot,orm.NullableColumnExpr(local),orm.KeyExpr(uid))`, true},
		{"wrong relation key", `var _=orm.BelongsToOf(slot,orm.KeyExpr(oid),orm.KeyExpr(uid))`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "consumer.go", base+tc.extra, 0)
			if err != nil {
				t.Fatal(err)
			}
			config := types.Config{Importer: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })}
			_, err = config.Check("consumer", fset, []*ast.File{file}, nil)
			if strings.HasPrefix(tc.name, "relation ") && (err == nil || !strings.Contains(err.Error(), "does not match inferred type")) {
				t.Fatalf("expected model-owner inference diagnostic: %v", err)
			}
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected typecheck result: %v", err)
			}
		})
	}
}
