package generate

import (
	"go/ast"
	"go/parser"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const closureGuardSource = `package sample

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/translations"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=users primary=ID
type User struct {
	ID         int
	ParentID   value.Nullable[int]
	Parent     relation.One[User]
	Children   relation.Many[User]
	Roles      relation.Through[Role, UserRole]
	ChildCount relation.Value[int64]
	Title      translations.Text
}

//foundry:model table=roles primary=ID
type Role struct {
	ID    int
	Users relation.Through[User, UserRole]
}

//foundry:model table=user_roles primary=ID
type UserRole struct {
	ID     int
	UserID int
	RoleID int
}

func defineUserRelations() UserRelationSet {
	u, r, ur := UserFields(), RoleFields(), UserRoleFields()
	return UserRelationSet{
		Parent:   query.BelongsTo(u.ParentID, u.ID),
		Children: query.HasMany(u.ID, u.ParentID),
		Roles:    query.ManyToMany(u.ID, ur.UserID, ur.RoleID, r.ID),
	}
}

func defineRoleRelations() RoleRelationSet {
	u, r, ur := UserFields(), RoleFields(), UserRoleFields()
	return RoleRelationSet{
		Users: query.ManyToMany(r.ID, ur.RoleID, ur.UserID, u.ID),
	}
}

func defineUserAggregates() UserAggregateSet {
	return UserAggregateSet{
		ChildCount: query.Related(UserRelations().Children, query.Count[User]()),
	}
}

func defineUserExtensions() UserExtensionSet {
	return UserExtensionSet{
		Title: translations.Options{MaxBytes: 256},
	}
}

func defineUserGlobalScopes() []query.GlobalScope[User] {
	return []query.GlobalScope[User]{
		query.NewGlobalScope("active", UserFields().ID.Gt(0)),
	}
}
`

const closureGuardLeakSource = closureGuardSource + `
func (User) Leak() query.Query[User] {
	return QueryUsers().Query
}
`

type closureWalker struct {
	visitedNamed map[*types.Named]bool
	violations   []string
}

func (w *closureWalker) walk(typ types.Type, path string) {
	if typ == nil {
		return
	}
	typ = types.Unalias(typ)
	switch t := typ.(type) {
	case *types.Named:
		pkg := t.Obj().Pkg()
		if pkg != nil && pkg.Path() == framework+"/database/query" {
			if t.Obj().Name() == "ModelRef" {
				return
			}
			if t.TypeArgs() != nil && t.TypeArgs().Len() > 0 {
				w.violations = append(w.violations, path+" -> "+t.String())
			}
			return
		}
		if pkg == nil || !strings.Contains(pkg.Path(), ".") {
			return
		}
		if w.visitedNamed[t] {
			return
		}
		w.visitedNamed[t] = true

		msetVal := types.NewMethodSet(t)
		for i := 0; i < msetVal.Len(); i++ {
			method := msetVal.At(i).Obj()
			if sig, ok := method.Type().(*types.Signature); ok {
				w.walkSignature(sig, path+" -> "+t.Obj().Name()+"."+method.Name()+"()")
			}
		}
		msetPtr := types.NewMethodSet(types.NewPointer(t))
		for i := 0; i < msetPtr.Len(); i++ {
			method := msetPtr.At(i).Obj()
			if sig, ok := method.Type().(*types.Signature); ok {
				w.walkSignature(sig, path+" -> (*"+t.Obj().Name()+")."+method.Name()+"()")
			}
		}
		for i := 0; i < t.NumMethods(); i++ {
			method := t.Method(i)
			if sig, ok := method.Type().(*types.Signature); ok {
				w.walkSignature(sig, path+" -> "+t.Obj().Name()+"."+method.Name()+"()")
			}
		}
		if t.TypeArgs() != nil {
			for i := 0; i < t.TypeArgs().Len(); i++ {
				w.walk(t.TypeArgs().At(i), path+" -> "+t.Obj().Name()+" typearg")
			}
		}
		w.walk(t.Underlying(), path+" -> "+t.Obj().Name())
	case *types.Pointer:
		w.walk(t.Elem(), path+" -> *")
	case *types.Slice:
		w.walk(t.Elem(), path+" -> []")
	case *types.Array:
		w.walk(t.Elem(), path+" -> [N]")
	case *types.Map:
		w.walk(t.Key(), path+" -> map key")
		w.walk(t.Elem(), path+" -> map elem")
	case *types.Chan:
		w.walk(t.Elem(), path+" -> chan")
	case *types.Signature:
		w.walkSignature(t, path)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			w.walk(f.Type(), path+" -> "+f.Name())
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			method := t.Method(i)
			if sig, ok := method.Type().(*types.Signature); ok {
				w.walkSignature(sig, path+" -> "+method.Name()+"()")
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			w.walk(t.EmbeddedType(i), path+" -> embedded")
		}
	}
}

func (w *closureWalker) walkSignature(sig *types.Signature, path string) {
	if sig.Params() != nil {
		for i := 0; i < sig.Params().Len(); i++ {
			w.walk(sig.Params().At(i).Type(), path+" param "+sig.Params().At(i).Name())
		}
	}
	if sig.Results() != nil {
		for i := 0; i < sig.Results().Len(); i++ {
			w.walk(sig.Results().At(i).Type(), path+" result")
		}
	}
}

func typeCheckGeneratedPackage(t *testing.T, dir string) (*packageGraph, *types.Package) {
	t.Helper()
	g, err := loadGraph(t.Context(), dir, false, nil, nil)
	if err != nil {
		t.Fatalf("loadGraph: %v", err)
	}
	var syntaxFiles []*ast.File
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			path := filepath.Join(dir, entry.Name())
			f, err := parser.ParseFile(g.fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", entry.Name(), err)
			}
			syntaxFiles = append(syntaxFiles, f)
		}
	}
	var pkgPath string
	for path := range g.packages {
		pkgPath = path
		break
	}
	config := types.Config{Importer: g}
	pkg, err := config.Check(pkgPath, g.fset, syntaxFiles, nil)
	if err != nil {
		t.Fatalf("types.Config.Check: %v", err)
	}
	return g, pkg
}

func checkPackageClosures(t *testing.T, pkg *types.Package) []string {
	t.Helper()
	var violations []string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}

		hasFoundryModel := false
		if types.NewMethodSet(named).Lookup(nil, "FoundryModel") != nil || types.NewMethodSet(types.NewPointer(named)).Lookup(nil, "FoundryModel") != nil {
			hasFoundryModel = true
		}
		if !hasFoundryModel {
			for i := 0; i < named.NumMethods(); i++ {
				if named.Method(i).Name() == "FoundryModel" {
					hasFoundryModel = true
					break
				}
			}
		}
		if !hasFoundryModel {
			continue
		}

		w := &closureWalker{visitedNamed: make(map[*types.Named]bool)}
		walkMethods := func(mset *types.MethodSet, receiverDesc string) {
			for i := 0; i < mset.Len(); i++ {
				method := mset.At(i).Obj()
				if sig, ok := method.Type().(*types.Signature); ok {
					w.walkSignature(sig, receiverDesc+"."+method.Name()+"()")
				}
			}
		}
		walkMethods(types.NewMethodSet(named), named.Obj().Name())
		walkMethods(types.NewMethodSet(types.NewPointer(named)), "(*"+named.Obj().Name()+")")
		for i := 0; i < named.NumMethods(); i++ {
			method := named.Method(i)
			if sig, ok := method.Type().(*types.Signature); ok {
				w.walkSignature(sig, named.Obj().Name()+"."+method.Name()+"()")
			}
		}

		if str, ok := named.Underlying().(*types.Struct); ok {
			for i := 0; i < str.NumFields(); i++ {
				f := str.Field(i)
				w.walk(f.Type(), named.Obj().Name()+"."+f.Name())
			}
		}

		violations = append(violations, w.violations...)
	}
	return violations
}

func TestModelClosureGuard(t *testing.T) {
	dir := fixture(t, closureGuardSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}

	g, pkg := typeCheckGeneratedPackage(t, dir)
	violations := checkPackageClosures(t, pkg)
	if len(violations) > 0 {
		for _, v := range violations {
			t.Errorf("closure violation detected: %s", v)
		}
		t.Fatalf("model closure guard detected %d generic query engine reachability violations", len(violations))
	}

	// Second assertion: query.ModelRef[T] has no methods, value or pointer receiver, exported or unexported.
	queryPkg, err := g.Import(framework + "/database/query")
	if err != nil {
		t.Fatalf("import database/query: %v", err)
	}
	modelRefObj := queryPkg.Scope().Lookup("ModelRef")
	if modelRefObj == nil {
		t.Fatal("query.ModelRef not found in database/query scope")
	}
	baseModelRef, ok := modelRefObj.(*types.TypeName).Type().(*types.Named)
	if !ok {
		t.Fatalf("query.ModelRef is not a named type: %T", modelRefObj)
	}
	if m := types.NewMethodSet(baseModelRef); m.Len() != 0 {
		t.Fatalf("base ModelRef value method set is not empty: %d methods", m.Len())
	}
	if m := types.NewMethodSet(types.NewPointer(baseModelRef)); m.Len() != 0 {
		t.Fatalf("base ModelRef pointer method set is not empty: %d methods", m.Len())
	}
	if n := baseModelRef.NumMethods(); n != 0 {
		t.Fatalf("base ModelRef declared methods count is not zero: %d", n)
	}

	// Also check instantiated ModelRef[T] on each generated model.
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}
		mset := types.NewMethodSet(types.NewPointer(named))
		fm := mset.Lookup(nil, "FoundryModel")
		if fm == nil {
			continue
		}
		sig := fm.Type().(*types.Signature)
		if sig.Results().Len() != 1 {
			t.Fatalf("%s.FoundryModel return count %d != 1", name, sig.Results().Len())
		}
		instModelRef, ok := sig.Results().At(0).Type().(*types.Named)
		if !ok || instModelRef.Obj().Name() != "ModelRef" {
			t.Fatalf("%s.FoundryModel does not return query.ModelRef: %v", name, sig.Results().At(0).Type())
		}
		if m := types.NewMethodSet(instModelRef); m.Len() != 0 {
			t.Fatalf("ModelRef[%s] value method set is not empty: %d methods", name, m.Len())
		}
		if m := types.NewMethodSet(types.NewPointer(instModelRef)); m.Len() != 0 {
			t.Fatalf("ModelRef[%s] pointer method set is not empty: %d methods", name, m.Len())
		}
		if n := instModelRef.NumMethods(); n != 0 {
			t.Fatalf("ModelRef[%s] declared methods count is not zero: %d", name, n)
		}
	}
}

func TestModelClosureGuardNegative(t *testing.T) {
	dir := fixture(t, closureGuardLeakSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}

	_, pkg := typeCheckGeneratedPackage(t, dir)
	violations := checkPackageClosures(t, pkg)
	if len(violations) == 0 {
		t.Fatal("expected closure violations for Leak() method, got none")
	}
	found := false
	for _, v := range violations {
		if strings.Contains(v, "User.Leak()") && strings.Contains(v, "query.Query[") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected violation path containing User.Leak() -> query.Query[...], got: %v", violations)
	}
}
