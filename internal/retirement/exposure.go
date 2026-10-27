package retirement

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func checkExposures(fset *token.FileSet, sources map[string]*sourcePackage, denied map[string][]string) error {
	importers, err := exposureImporters(fset, sources)
	if err != nil {
		return err
	}
	w := exposureWalker{fset: fset, denied: denied, deprecated: declarationDocs(fset, sources)}
	for _, key := range sortedKeys(sources) {
		source := sources[key]
		config := types.Config{
			Importer: importers[source.moduleDir], IgnoreFuncBodies: true, FakeImportC: true,
			// Compile-fail inputs and mutually exclusive build files are scanned
			// too. Keep their resolved declarations after ordinary typing errors;
			// compilation validity is owned by the build and compiler-case gates.
			Error: func(error) {},
		}
		info := &types.Info{}
		if len(w.deprecated) != 0 {
			info.Types = make(map[ast.Expr]types.TypeAndValue)
		}
		pkg, _ := config.Check(source.owner, fset, source.files, info)
		if pkg == nil {
			return safeerror.New(fmt.Sprintf("%s: no scope for exposure check", source.owner))
		}
		w.interfaceDocs = interfaceDocs(source.files, info)
		w.owner = source.owner
		for _, name := range pkg.Scope().Names() {
			obj := pkg.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			w.seen = make(map[types.Type]bool)
			w.reported = make(map[string]bool)
			w.object(obj, name)
		}
	}
	return safeerror.Join("operation failed", w.failures...)
}

type exposureWalker struct {
	fset          *token.FileSet
	denied        map[string][]string
	deprecated    map[declarationLocation]bool
	interfaceDocs map[types.Type][]token.Pos
	owner         string
	seen          map[types.Type]bool
	reported      map[string]bool
	failures      []error
}

func (w *exposureWalker) object(obj types.Object, selector string) {
	for _, denied := range w.denied[strings.TrimPrefix(w.owner, framework)] {
		if selector == denied {
			w.fail("retired public selector", selector, obj.Pos())
		}
	}
	w.doc(obj, selector)
	w.typ(obj.Type(), selector)
}

func (w *exposureWalker) doc(obj types.Object, selector string) {
	if w.deprecated[declarationAt(w.fset, obj.Pos(), obj.Name())] {
		w.fail("deprecated exposed declaration", selector, obj.Pos())
	}
}

func (w *exposureWalker) fail(reason, selector string, pos token.Pos) {
	key := reason + ":" + selector + ":" + w.fset.Position(pos).String()
	if !w.reported[key] {
		w.reported[key] = true
		w.failures = append(w.failures, safeerror.New(fmt.Sprintf("%s: %s %s.%s", w.fset.Position(pos), reason, w.owner, selector)))
	}
}

func (w *exposureWalker) typ(typ types.Type, selector string) {
	if typ == nil || w.seen[typ] {
		return
	}
	w.seen[typ] = true
	w.interfaceAnnotations(typ, selector)
	// Keep the alias chain long enough to inspect each declaration's docs.
	if alias, ok := typ.(*types.Alias); ok {
		w.doc(alias.Obj(), selector)
		w.typ(alias.Rhs(), selector)
		return
	}
	if named, ok := typ.(*types.Named); ok {
		w.doc(named.Obj(), selector)
		for i := 0; i < named.TypeParams().Len(); i++ {
			w.typ(named.TypeParams().At(i).Constraint(), selector+"[constraint]")
		}
		for i := 0; i < named.TypeArgs().Len(); i++ {
			w.typ(named.TypeArgs().At(i), selector+"[type argument]")
		}
	}
	// NewMethodSet includes interface methods and promoted methods. A variable
	// exposes method values too, so it enters through the same object walk.
	members := make(map[types.Object]bool)
	for _, receiver := range []types.Type{typ, types.NewPointer(typ)} {
		set := types.NewMethodSet(receiver)
		for i := 0; i < set.Len(); i++ {
			obj := set.At(i).Obj()
			if obj.Exported() && !members[obj] {
				members[obj] = true
				w.object(obj, selector+"."+obj.Name())
			}
		}
	}
	// Enumerate candidate field names through embeddings, then let go/types
	// resolve promotion, shadowing and ambiguity exactly as a selector would.
	fields := make(map[string]bool)
	embeddedFields(typ, fields, make(map[types.Type]bool))
	for _, name := range sortedKeys(fields) {
		obj, _, _ := types.LookupFieldOrMethod(typ, true, nil, name)
		if field, ok := obj.(*types.Var); ok && field.IsField() {
			w.object(field, selector+"."+name)
		}
	}
	// Follow public signature/container types and embedding declarations for
	// deprecation. Ordinary unexported fields are not part of the exposure.
	switch t := typ.Underlying().(type) {
	case *types.Pointer:
		w.typ(t.Elem(), selector)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			if field.Embedded() {
				w.doc(field, selector)
				w.embeddingDocs(field.Type(), selector, make(map[types.Type]bool))
			}
		}
	case *types.Signature:
		for i := 0; i < t.TypeParams().Len(); i++ {
			w.typ(t.TypeParams().At(i).Constraint(), selector+"[constraint]")
		}
		for _, tuple := range []*types.Tuple{t.Params(), t.Results()} {
			for i := 0; i < tuple.Len(); i++ {
				w.typ(tuple.At(i).Type(), selector+"[signature]")
			}
		}
	case *types.Array:
		w.typ(t.Elem(), selector+"[element]")
	case *types.Slice:
		w.typ(t.Elem(), selector+"[element]")
	case *types.Map:
		w.typ(t.Key(), selector+"[key]")
		w.typ(t.Elem(), selector+"[element]")
	case *types.Chan:
		w.typ(t.Elem(), selector+"[element]")
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			w.embeddingDocs(t.EmbeddedType(i), selector, make(map[types.Type]bool))
		}
	}
}

// Both the public surface walk and recursive embedding walk consult the same
// annotations, including those on the declaration behind a generic instance.
func (w *exposureWalker) interfaceAnnotations(typ types.Type, selector string) {
	for _, pos := range w.interfaceDocs[typ.Underlying()] {
		w.fail("deprecated exposed declaration", selector, pos)
	}
	if named, ok := types.Unalias(typ).(*types.Named); ok && named.Origin() != named {
		for _, pos := range w.interfaceDocs[named.Origin().Underlying()] {
			w.fail("deprecated exposed declaration", selector, pos)
		}
	}
}

// Promotion must not expose a hidden or ambiguous selector. This walk therefore
// follows only embedding docs; actual members are resolved above at the root.
func (w *exposureWalker) embeddingDocs(typ types.Type, selector string, seen map[types.Type]bool) {
	if typ == nil || seen[typ] {
		return
	}
	seen[typ] = true
	w.interfaceAnnotations(typ, selector)
	switch t := typ.(type) {
	case *types.Alias:
		w.doc(t.Obj(), selector)
		w.embeddingDocs(t.Rhs(), selector, seen)
	case *types.Named:
		w.doc(t.Obj(), selector)
		w.embeddingDocs(t.Underlying(), selector, seen)
	case *types.Pointer:
		w.embeddingDocs(t.Elem(), selector, seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			if field.Embedded() {
				w.doc(field, selector)
				w.embeddingDocs(field.Type(), selector, seen)
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			w.embeddingDocs(t.EmbeddedType(i), selector, seen)
		}
	}
}

func embeddedFields(typ types.Type, names map[string]bool, seen map[types.Type]bool) {
	typ = types.Unalias(typ)
	if seen[typ] {
		return
	}
	seen[typ] = true
	if ptr, ok := typ.(*types.Pointer); ok {
		embeddedFields(ptr.Elem(), names, seen)
		return
	}
	if s, ok := typ.Underlying().(*types.Struct); ok {
		for i := 0; i < s.NumFields(); i++ {
			field := s.Field(i)
			if field.Exported() {
				names[field.Name()] = true
			}
			if field.Embedded() {
				embeddedFields(field.Type(), names, seen)
			}
		}
	}
}
