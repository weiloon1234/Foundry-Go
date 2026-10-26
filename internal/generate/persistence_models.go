package generate

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
)

// ormPackage owns persistence declarations. Define is their only entry point,
// and its callback receives the Builder that every table, column and relation
// constructor requires.
const ormPackage = framework + "/database/orm"

// Persistence models are recognized after every package in a run passed its
// complete overlay check, so recognition never depends on which sibling
// package finished first or on the declaration pass's syntactic subset:
//
//  1. Each source-checked package that can reach database/orm keeps the type
//     roots and generic instances of that complete check, including function
//     bodies, initializers and methods (persistenceSource).
//  2. Export-data dependencies loaded by those packages contribute their
//     package-scope declarations; their initializers and bodies are not
//     available.
//  3. resolveBearers finds every generic in those packages that mentions one
//     of its type parameters as the model argument of a persistence generic
//     or of another such generic (a bearer), in a body, signature, field or
//     constraint.
//  4. One structural walk records the model argument of every Table and
//     Builder handle, every Define/TableOf instantiation and every bearer
//     instantiation. Generic instances are walked with their substituted
//     members, so a handle written with a type parameter is recognized where
//     its generic is instantiated.
//
// Discovered DTO, union and multipart graphs are then validated against the
// run-wide set before anything is published.

// persistenceGeneric reports the database/orm generics whose first type
// parameter is, by construction, a declared persistence model: Define with its
// Builder, and TableOf with the Table handle it returns. Other orm generics
// (Query, Predicate, columns) are only obtained from these declarations, and
// raw fragments such as WhereRaw[M] accept any M.
func persistenceGeneric(object types.Object) bool {
	if object == nil || object.Pkg() == nil || object.Pkg().Path() != ormPackage {
		return false
	}
	switch object.Name() {
	case "Define", "Builder", "TableOf", "Table":
		return true
	}
	return false
}

// persistenceModelKey identifies a package-level model independently of the
// declaration-pass, complete-overlay and export-data objects representing it.
type persistenceModelKey struct{ path, name string }

// persistenceModelSet holds recognized model origins. A generic model is
// recognized for every instantiation of its origin.
type persistenceModelSet map[persistenceModelKey]bool

// modelKey returns the identity of a package-level named type. Function-local
// types cannot be named by a DTO declaration, so they have none.
func modelKey(named *types.Named) (persistenceModelKey, bool) {
	object := named.Origin().Obj()
	if object.Pkg() == nil || object.Parent() != object.Pkg().Scope() {
		return persistenceModelKey{}, false
	}
	return persistenceModelKey{object.Pkg().Path(), object.Name()}, true
}

func (s persistenceModelSet) contains(named *types.Named) bool {
	key, ok := modelKey(named)
	return ok && s[key]
}

// persistenceSource is the part of one complete overlay check that
// recognition inspects once the whole run is checked.
type persistenceSource struct {
	pkg       *types.Package
	instances []persistenceInstance
	roots     []persistenceRoot
	// owners maps each type parameter this package declares to its generic
	// declaration and position; a method's receiver type parameters belong
	// to the receiver's generic type.
	owners map[*types.TypeParam]persistenceBearer
}

// persistenceBearer is one type-parameter position of a generic declaration:
// a function, type or alias. It bears a persistence model when the
// declaration passes that type parameter to a persistence generic or to
// another bearer, even if no instance member exposes the resulting handle.
type persistenceBearer struct {
	owner types.Object
	index int
}

// persistenceInstance is one explicit or inferred generic instantiation.
type persistenceInstance struct {
	pos      token.Pos
	name     string
	generic  types.Object
	instance types.Instance
}

// persistenceRoot is one distinct type recorded by the complete check, at its
// first source position.
type persistenceRoot struct {
	pos, end token.Pos
	name     string
	typ      types.Type
}

func (r persistenceRoot) before(other persistenceRoot) bool {
	if r.pos != other.pos {
		return r.pos < other.pos
	}
	if r.end != other.end {
		return r.end < other.end
	}
	if r.name != other.name {
		return r.name < other.name
	}
	return r.typ.String() < other.typ.String()
}

// collectPersistenceSource reduces a complete check's information to its
// instances and distinct types in source order, so retained state stays small
// and the later walk visits them deterministically.
func collectPersistenceSource(pkg *types.Package, info *types.Info) *persistenceSource {
	source := &persistenceSource{pkg: pkg, owners: make(map[*types.TypeParam]persistenceBearer)}
	for id, instance := range info.Instances {
		source.instances = append(source.instances, persistenceInstance{pos: id.Pos(), name: id.Name, generic: info.Uses[id], instance: instance})
	}
	sort.SliceStable(source.instances, func(i, j int) bool {
		left, right := source.instances[i], source.instances[j]
		if left.pos != right.pos {
			return left.pos < right.pos
		}
		if left.name != right.name {
			return left.name < right.name
		}
		return left.instance.Type.String() < right.instance.Type.String()
	})
	own := func(owner types.Object, params *types.TypeParamList) {
		for i := 0; i < params.Len(); i++ {
			source.owners[params.At(i)] = persistenceBearer{owner, i}
		}
	}
	for _, object := range info.Defs {
		switch object := object.(type) {
		case *types.Func:
			signature := object.Signature()
			own(object, signature.TypeParams())
			if receiver := signature.Recv(); receiver != nil {
				typ := types.Unalias(receiver.Type())
				if pointer, ok := typ.(*types.Pointer); ok {
					typ = types.Unalias(pointer.Elem())
				}
				if named, ok := typ.(*types.Named); ok {
					own(named.Origin().Obj(), signature.RecvTypeParams())
				}
			}
		case *types.TypeName:
			switch typ := object.Type().(type) {
			case *types.Named:
				own(object, typ.TypeParams())
			case *types.Alias:
				own(object, typ.TypeParams())
			}
		}
	}
	first := make(map[types.Type]persistenceRoot)
	add := func(node ast.Node, name string, typ types.Type) {
		if typ == nil {
			return
		}
		root := persistenceRoot{pos: node.Pos(), end: node.End(), name: name, typ: typ}
		if previous, seen := first[typ]; seen && !root.before(previous) {
			return
		}
		first[typ] = root
	}
	for id, object := range info.Defs {
		if object != nil {
			add(id, id.Name, object.Type())
		}
	}
	for id, object := range info.Uses {
		add(id, id.Name, object.Type())
	}
	for expression, value := range info.Types {
		add(expression, "", value.Type)
	}
	source.roots = make([]persistenceRoot, 0, len(first))
	for _, root := range first {
		source.roots = append(source.roots, root)
	}
	sort.SliceStable(source.roots, func(i, j int) bool { return source.roots[i].before(source.roots[j]) })
	return source
}

// importsPersistence reports whether a package's own imports can reach
// database/orm. Only such a package can name or instantiate a table handle.
func (g *packageGraph) importsPersistence(files []source) bool {
	for _, file := range files {
		for _, spec := range file.syntax.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err == nil && (path == ormPackage || g.persistenceDependents[path]) {
				return true
			}
		}
	}
	return false
}

// rejectPersistenceModels is the authoritative persistence-model check. It
// runs after every package in the run passed its complete overlay check and
// before any output is published.
func (g *packageGraph) rejectPersistenceModels(results []preparedPackage) error {
	transport := false
	var sources []*persistenceSource
	for i := range results {
		declared := results[i].metadata
		transport = transport || len(declared.dtos)+len(declared.unions)+len(declared.multipart) != 0
		if results[i].input.persistence != nil {
			sources = append(sources, results[i].input.persistence)
		}
	}
	if !transport {
		// No transport graph exists that a model could reach.
		return nil
	}
	models, err := g.persistenceModels(sources)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	for i := range results {
		if err := results[i].input.rejectPersistenceModels(results[i].metadata, models); err != nil {
			return err
		}
	}
	return nil
}

// persistenceModels recognizes every model visible from sources: their own
// declarations and package-scope declarations of the export-data packages
// they load.
func (g *packageGraph) persistenceModels(sources []*persistenceSource) (persistenceModelSet, error) {
	recognizer := newPersistenceRecognizer(g.fset)
	recognizer.resolveBearers(sources)
	var imports []*types.Package
	for _, source := range sources {
		if err := recognizer.source(source); err != nil {
			return nil, err
		}
		imports = append(imports, source.pkg.Imports()...)
	}
	exports, err := g.persistenceExports(imports)
	if err != nil {
		return nil, err
	}
	for _, pkg := range exports {
		if err := recognizer.packageScope(pkg); err != nil {
			return nil, err
		}
	}
	return recognizer.models, nil
}

// discoveryFailure retains a package whose discovery failed, with its partial
// transport graphs, so the reported diagnostic can name a persistence model.
type discoveryFailure struct {
	input    *packageInput
	metadata metadata
	err      error
}

func (f *discoveryFailure) Error() string { return f.err.Error() }
func (f *discoveryFailure) Unwrap() error { return f.err }

// refineDiscoveryFailure reports a persistence-model diagnostic in place of a
// discovery failure when a partial graph already reaches a model, such as a
// model map key that would otherwise fail as an unsupported key. It only
// rewrites an existing failure, never accepts a package, and uses the failed
// package's own sources and its completed import closure, so the reported
// error does not depend on which unrelated packages finished.
func (g *packageGraph) refineDiscoveryFailure(results []preparedPackage, index map[string]int, err error) error {
	var failure *discoveryFailure
	if !errors.As(err, &failure) {
		return err
	}
	p := failure.input
	if !p.persistenceDependent {
		return failure.err
	}
	// Generated output does not exist yet; missing generated symbols only
	// leave their own expressions invalid in this best-effort check.
	files := make([]*ast.File, len(p.files))
	for i := range p.files {
		files[i] = p.files[i].syntax
	}
	info := persistenceInfo()
	config := types.Config{Importer: p.importer, Error: func(error) {}}
	checked, _ := config.Check(p.path, p.fset, files, info)
	if checked == nil {
		return failure.err
	}
	sources := []*persistenceSource{collectPersistenceSource(checked, info)}
	seen := make(map[string]bool)
	queue := checked.Imports()
	for len(queue) != 0 {
		dependency := queue[0]
		queue = queue[1:]
		position, selected := index[dependency.Path()]
		if !selected || seen[dependency.Path()] || results[position].input == nil {
			continue
		}
		seen[dependency.Path()] = true
		if source := results[position].input.persistence; source != nil {
			sources = append(sources, source)
		}
		queue = append(queue, results[position].input.checked.Imports()...)
	}
	models, recognitionErr := g.persistenceModels(sources)
	if recognitionErr != nil || len(models) == 0 {
		return failure.err
	}
	if refined := p.rejectPersistenceModels(failure.metadata, models); refined != nil {
		// The original failure is the generator's own diagnostic, which the
		// run would otherwise report unchanged.
		return fmt.Errorf("%w (discovery also failed: %s)", refined, failure.err)
	}
	return failure.err
}

// persistenceInfo requests the type information persistence recognition
// inspects.
func persistenceInfo() *types.Info {
	return &types.Info{
		Types:     make(map[ast.Expr]types.TypeAndValue),
		Defs:      make(map[*ast.Ident]types.Object),
		Uses:      make(map[*ast.Ident]types.Object),
		Instances: make(map[*ast.Ident]types.Instance),
	}
}

// persistenceExports lists, by path, the loaded export-data packages outside
// the run that can declare tables. Go's dependency listing is transitive, so a
// package without a database/orm dependency cannot lead to one either.
func (g *packageGraph) persistenceExports(queue []*types.Package) ([]*types.Package, error) {
	seen := make(map[string]bool)
	var result []*types.Package
	for len(queue) != 0 {
		pkg := queue[0]
		queue = queue[1:]
		path := pkg.Path()
		if seen[path] {
			continue
		}
		seen[path] = true
		if _, selected := g.packages[path]; selected || !g.persistenceDependents[path] {
			continue
		}
		if !pkg.Complete() {
			// A package referenced only through another package's export data
			// holds just those objects until its own export data is read.
			g.compiledMu.Lock()
			complete, err := g.compiled.Import(path)
			g.compiledMu.Unlock()
			if err != nil {
				return nil, fmt.Errorf("persistence-model recognition: load export data for %s: %w", path, err)
			}
			pkg = complete
		}
		result = append(result, pkg)
		queue = append(queue, pkg.Imports()...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path() < result[j].Path() })
	return result, nil
}

// rejectPersistenceModels validates the graphs discovery enumerated for this
// package. Discovery already rejected types with a model identity method and
// recorded every reachable node, so only the recognized-model rules remain.
func (p *packageInput) rejectPersistenceModels(result metadata, models persistenceModelSet) error {
	for i := range result.unions {
		if err := p.rejectPersistenceGraph(&result.unions[i].dtoGraph, models); err != nil {
			return err
		}
	}
	for i := range result.multipart {
		for j := range result.multipart[i].fields {
			if err := p.rejectPersistenceGraph(&result.multipart[i].fields[j].graph, models); err != nil {
				return err
			}
		}
	}
	for i := range result.dtos {
		if err := p.rejectPersistenceGraph(&result.dtos[i].dtoGraph, models); err != nil {
			return err
		}
	}
	return nil
}

// rejectPersistenceGraph applies resolveJSONGraph's field, embedding and map
// key rules to each node. Object nodes are exactly the structs whose fields
// discovery flattened, so codec, enum and wrapper nodes keep their meaning.
func (p *packageInput) rejectPersistenceGraph(graph *dtoGraph, models persistenceModelSet) error {
	for _, node := range graph.nodes {
		if node.typ == nil || node.wire.Kind == contract.UnionKind {
			continue
		}
		if named, ok := types.Unalias(node.typ).(*types.Named); ok && models.contains(named) {
			return p.diagnostic(node.position, persistenceFieldDiagnostic)
		}
		if node.wire.Kind == contract.ObjectKind {
			embedded, err := dtoEmbedsModel(node.typ, models.contains)
			if err != nil {
				return p.diagnostic(node.position, err.Error())
			}
			if embedded {
				return p.diagnostic(node.position, persistenceEmbeddingDiagnostic)
			}
		}
		if node.key != nil {
			if named, ok := types.Unalias(node.key.typ).(*types.Named); ok && models.contains(named) {
				return p.diagnostic(node.position, persistenceMapKeyDiagnostic)
			}
		}
	}
	return nil
}

// persistenceInstanceKey deduplicates generic instances, which go/types does
// not keep pointer-canonical across lazy expansion. The origin separates
// generic types whose instances print alike. Same-named type parameters of
// different owners can still print alike; their constraints are visited
// through each owner's own type parameters.
type persistenceInstanceKey struct {
	origin *types.Named
	text   string
}

// persistenceRecognizer is one run's structural walk. A single goroutine uses
// it after every package worker finished.
type persistenceRecognizer struct {
	fset        *token.FileSet
	models      persistenceModelSet
	seen        map[types.Type]bool
	instances   map[persistenceInstanceKey]bool
	typeParams  map[*types.Named]bool
	aliasParams map[*types.Alias]bool
	bearers     map[persistenceBearer]bool
	// pos, pkg and object locate the declaration or expression being walked.
	pos    token.Pos
	pkg    string
	object string
	err    error
}

func newPersistenceRecognizer(fset *token.FileSet) *persistenceRecognizer {
	return &persistenceRecognizer{
		fset:        fset,
		models:      make(persistenceModelSet),
		seen:        make(map[types.Type]bool),
		instances:   make(map[persistenceInstanceKey]bool),
		typeParams:  make(map[*types.Named]bool),
		aliasParams: make(map[*types.Alias]bool),
		bearers:     make(map[persistenceBearer]bool),
	}
}

// bearing reports whether the type argument at index of an instance of
// generic names a persistence model.
func (r *persistenceRecognizer) bearing(generic types.Object, index int) bool {
	return generic != nil && ((index == 0 && persistenceGeneric(generic)) || r.bearers[persistenceBearer{generic, index}])
}

// resolveBearers finds every generic that passes one of its own type
// parameters to a persistence generic or, transitively, to another bearer.
// Type parameters are lexically scoped, so each instance's type-parameter
// argument belongs to a declaration of the package that holds the instance.
// The result is a fixpoint, so it does not depend on visiting order.
func (r *persistenceRecognizer) resolveBearers(sources []*persistenceSource) {
	for changed := true; changed; {
		changed = false
		for _, source := range sources {
			for _, use := range source.instances {
				for i := 0; i < use.instance.TypeArgs.Len(); i++ {
					if !r.bearing(use.generic, i) {
						continue
					}
					parameter, ok := types.Unalias(use.instance.TypeArgs.At(i)).(*types.TypeParam)
					if !ok {
						continue
					}
					if owner, ok := source.owners[parameter]; ok && !r.bearers[owner] {
						r.bearers[owner] = true
						changed = true
					}
				}
			}
		}
	}
}

func (r *persistenceRecognizer) at(pos token.Pos, pkg, object string) {
	r.pos, r.pkg, r.object = pos, pkg, object
	if r.object == "" {
		r.object = "expression"
	}
}

func (r *persistenceRecognizer) fail(message string) {
	if r.err != nil {
		return
	}
	position := "-"
	if r.fset != nil && r.pos.IsValid() {
		position = r.fset.Position(r.pos).String()
	}
	r.err = fmt.Errorf("%s: persistence-model recognition in %s (%s): %s", position, r.pkg, r.object, message)
}

// source walks one source-checked package's complete check.
func (r *persistenceRecognizer) source(source *persistenceSource) error {
	for _, use := range source.instances {
		r.at(use.pos, source.pkg.Path(), use.name)
		for i := 0; i < use.instance.TypeArgs.Len(); i++ {
			if r.bearing(use.generic, i) {
				r.model(use.instance.TypeArgs.At(i), use.generic)
			}
			r.visit(use.instance.TypeArgs.At(i))
		}
		r.visit(use.instance.Type)
		if r.err != nil {
			return r.err
		}
	}
	for _, root := range source.roots {
		r.at(root.pos, source.pkg.Path(), root.name)
		r.visit(root.typ)
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

// packageScope walks the package-level declarations of a package, the only
// part of export data that describes its declarations.
func (r *persistenceRecognizer) packageScope(pkg *types.Package) error {
	for _, name := range pkg.Scope().Names() {
		object := pkg.Scope().Lookup(name)
		r.at(object.Pos(), pkg.Path(), name)
		r.visit(object.Type())
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

// model records the model argument of a persistence generic or bearer. Type
// parameters are resolved by resolveBearers and recognized where their
// generic declaration is instantiated.
func (r *persistenceRecognizer) model(argument types.Type, generic types.Object) {
	switch model := types.Unalias(argument).(type) {
	case *types.TypeParam:
		return
	case *types.Named:
		if key, ok := modelKey(model); ok {
			r.models[key] = true
		}
		return
	case *types.Basic:
		if model.Kind() == types.Invalid || !persistenceGeneric(generic) {
			// Only a failed package's best-effort refinement check has invalid
			// arguments. A bearer is an inferred wrapper; a basic argument
			// cannot be a DTO model, so it records nothing.
			return
		}
	case *types.Interface:
		if !persistenceGeneric(generic) {
			// An interface argument of an inferred wrapper is not a model.
			return
		}
	}
	r.fail(fmt.Sprintf("%s.%s model type argument %s is not a named type; a persistence model argument must be a named struct type", generic.Pkg().Name(), generic.Name(), argument))
}

// markNamed reports whether a named type is visited for the first time. A
// non-instantiated named type is unique per declaration.
func (r *persistenceRecognizer) markNamed(named *types.Named) bool {
	if !r.mark(named) {
		return false
	}
	if named.TypeArgs().Len() == 0 {
		return true
	}
	key := persistenceInstanceKey{named.Origin(), named.String()}
	if r.instances[key] {
		return false
	}
	r.instances[key] = true
	return true
}

func (r *persistenceRecognizer) mark(typ types.Type) bool {
	if r.seen[typ] {
		return false
	}
	r.seen[typ] = true
	return true
}

// visit records persistence handles reachable from typ. Every standard
// go/types kind is listed; any other kind fails closed.
func (r *persistenceRecognizer) visit(typ types.Type) {
	if typ == nil || r.err != nil {
		return
	}
	if alias, ok := typ.(*types.Alias); ok {
		if typeParams := alias.Origin().TypeParams(); typeParams != nil && !r.aliasParams[alias.Origin()] {
			r.aliasParams[alias.Origin()] = true
			for i := 0; i < typeParams.Len(); i++ {
				r.visit(typeParams.At(i))
			}
		}
		if typeArgs := alias.TypeArgs(); typeArgs != nil {
			for i := 0; i < typeArgs.Len(); i++ {
				if r.bearing(alias.Origin().Obj(), i) {
					r.model(typeArgs.At(i), alias.Origin().Obj())
				}
				r.visit(typeArgs.At(i))
			}
		}
	}
	switch typ := types.Unalias(typ).(type) {
	case *types.Named:
		if typ.TypeArgs().Len() > 0 && persistenceGeneric(typ.Obj()) {
			r.model(typ.TypeArgs().At(0), typ.Obj())
			return
		}
		if !r.markNamed(typ) {
			return
		}
		for i := 0; i < typ.TypeArgs().Len(); i++ {
			if r.bearing(typ.Origin().Obj(), i) {
				r.model(typ.TypeArgs().At(i), typ.Origin().Obj())
			}
			r.visit(typ.TypeArgs().At(i))
		}
		if typeParams := typ.Origin().TypeParams(); typeParams != nil && !r.typeParams[typ.Origin()] {
			r.typeParams[typ.Origin()] = true
			for i := 0; i < typeParams.Len(); i++ {
				r.visit(typeParams.At(i))
			}
		}
		// An instance's methods and underlying type are substituted, so a
		// handle written with a type parameter yields its concrete model here.
		for i := 0; i < typ.NumMethods(); i++ {
			r.visit(typ.Method(i).Type())
		}
		r.visit(typ.Underlying())
	case *types.TypeParam:
		if r.mark(typ) {
			r.visit(typ.Constraint())
		}
	case *types.Basic:
		// Basic types do not contain persistence models.
	case *types.Alias:
		// Unaliased before the switch.
	case *types.Pointer:
		if r.mark(typ) {
			r.visit(typ.Elem())
		}
	case *types.Slice:
		if r.mark(typ) {
			r.visit(typ.Elem())
		}
	case *types.Array:
		if r.mark(typ) {
			r.visit(typ.Elem())
		}
	case *types.Chan:
		if r.mark(typ) {
			r.visit(typ.Elem())
		}
	case *types.Map:
		if r.mark(typ) {
			r.visit(typ.Key())
			r.visit(typ.Elem())
		}
	case *types.Struct:
		if r.mark(typ) {
			for i := 0; i < typ.NumFields(); i++ {
				r.visit(typ.Field(i).Type())
			}
		}
	case *types.Interface:
		if r.mark(typ) {
			for i := 0; i < typ.NumExplicitMethods(); i++ {
				r.visit(typ.ExplicitMethod(i).Type())
			}
			for i := 0; i < typ.NumEmbeddeds(); i++ {
				r.visit(typ.EmbeddedType(i))
			}
		}
	case *types.Union:
		if r.mark(typ) {
			for i := 0; i < typ.Len(); i++ {
				r.visit(typ.Term(i).Type())
			}
		}
	case *types.Signature:
		if r.mark(typ) {
			if typ.Recv() != nil {
				r.visit(typ.Recv().Type())
			}
			if typeParams := typ.TypeParams(); typeParams != nil {
				for i := 0; i < typeParams.Len(); i++ {
					r.visit(typeParams.At(i))
				}
			}
			r.visit(typ.Params())
			r.visit(typ.Results())
		}
	case *types.Tuple:
		if r.mark(typ) {
			for i := 0; i < typ.Len(); i++ {
				r.visit(typ.At(i).Type())
			}
		}
	default:
		r.fail(fmt.Sprintf("unsupported go/types kind %T", typ))
	}
}
