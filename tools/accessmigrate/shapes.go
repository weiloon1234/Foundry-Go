package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
)

// Applications build access options in two shapes before calling a constructor:
// a local or package variable holding []http.GuardOption[M] and spread with name...,
// and a helper function returning such a slice. Both become one http.AccessRule when
// every part of the shape can be decided; the rest is listed with its reason.

// shape is one variable or helper function that holds or returns an options slice.
type shape struct {
	key    string
	name   string
	isFunc bool
	pos    token.Pos
	reason string // why it cannot become a rule; empty when it can
	deps   []string
	helper string // for a variable assigned from a helper: the helper's key
}

type shapes struct {
	fset     *token.FileSet
	items    map[string]*shape
	excluded map[string]string // reasons found by a previous pass: a use was left
	pending  map[string]string
}

func newShapes(fset *token.FileSet) *shapes {
	return &shapes{fset: fset, items: map[string]*shape{}, excluded: map[string]string{}, pending: map[string]string{}}
}

// objectKey names an object identically in every type-checked copy of its package.
func (s *shapes) objectKey(obj types.Object) string {
	if obj == nil {
		return ""
	}
	if fn, ok := obj.(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
			t := sig.Recv().Type()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			if named, ok := t.(*types.Named); ok && fn.Pkg() != nil {
				return fn.Pkg().Path() + "." + named.Obj().Name() + "." + fn.Name()
			}
		}
	}
	if obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
		return obj.Pkg().Path() + "." + obj.Name()
	}
	p := s.fset.Position(obj.Pos())
	return p.Filename + ":" + strconv.Itoa(p.Offset)
}

func isOptionSlice(t types.Type) bool {
	slice, ok := types.Unalias(t).(*types.Slice)
	if !ok {
		return false
	}
	named, ok := types.Unalias(slice.Elem()).(*types.Named)
	return ok && named.Obj().Name() == "GuardOption" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == httpPath
}

// accessRuleType writes the type of an options slice, []http.GuardOption[M], as
// http.AccessRule[M].
func (w *fileWalk) accessRuleType(t ast.Expr) (string, bool) {
	array, ok := t.(*ast.ArrayType)
	if !ok || array.Len != nil {
		return "", false
	}
	index, ok := array.Elt.(*ast.IndexExpr)
	if !ok {
		return "", false
	}
	sel, ok := index.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "GuardOption" {
		return "", false
	}
	return w.text(sel.X) + ".AccessRule[" + w.text(index.Index) + "]", true
}

// literalRule is the rule that a slice literal of access options stands for.
func (w *fileWalk) literalRule(e ast.Expr) (string, string) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || !isOptionSlice(w.pkg.info.TypeOf(lit)) {
		return "", "is not a slice literal of Scopes, Permissions or AnyOf"
	}
	var parts optionParts
	for _, el := range lit.Elts {
		switch w.kindOf(el) {
		case "requirement":
			parts.reqs = append(parts.reqs, el)
		case "anyof":
			if parts.anyOf != nil {
				return "", "holds more than one AnyOf"
			}
			parts.anyOf = el
		case "actor":
			return "", "holds actor middleware, which stays an actor option of the constructor"
		default:
			return "", "holds an element that is not a Scopes, Permissions or AnyOf call"
		}
	}
	if !parts.hasAccess() {
		return "", "is empty, which is the open case: decide AnyActor() or a capability"
	}
	text, why := w.ruleText(parts)
	return text, why
}

func (w *fileWalk) line(n ast.Node) string {
	p := w.e.fset.Position(n.Pos())
	return filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)
}

// analyze finds every options slice variable and helper in the module and decides
// which of them can become a rule.
func (e *engine) analyze(packages []sourcePackage) {
	s := e.shapes
	s.items = map[string]*shape{}
	walks := map[*ast.File]*fileWalk{}
	var order []*fileWalk
	for _, pkg := range packages {
		for _, file := range pkg.files {
			if _, ok := e.src[e.fset.Position(file.Pos()).Filename]; !ok {
				continue
			}
			w := e.newWalk(pkg, file)
			walks[file] = w
			order = append(order, w)
		}
	}
	for _, w := range order {
		w.findShapes()
	}
	for _, w := range order {
		w.checkShapeUses()
	}
	for key, reason := range s.excluded {
		if it := s.items[key]; it != nil && it.reason == "" {
			it.reason = reason
		}
	}
	for changed := true; changed; {
		changed = false
		for _, it := range s.items {
			if it.reason != "" {
				continue
			}
			if it.helper != "" {
				if h := s.items[it.helper]; h == nil || h.reason != "" {
					it.reason = "the helper " + helperName(h, it.helper) + " cannot become a rule"
					changed = true
				}
			}
			for _, dep := range it.deps {
				if d := s.items[dep]; d != nil && d.reason != "" {
					it.reason = "the options slice " + d.name + " that receives its result cannot become a rule"
					changed = true
					break
				}
			}
		}
	}
}

func helperName(h *shape, key string) string {
	if h != nil {
		return h.name
	}
	return key
}

func (s *shapes) item(w *fileWalk, id *ast.Ident, fn bool) *shape {
	obj := w.pkg.info.ObjectOf(id)
	key := s.objectKey(obj)
	if key == "" {
		return nil
	}
	it := s.items[key]
	if it == nil {
		it = &shape{key: key, name: id.Name, isFunc: fn, pos: id.Pos()}
		s.items[key] = it
	}
	return it
}

func (it *shape) refuse(reason string) {
	if it.reason == "" {
		it.reason = reason
	}
}

// findShapes records the declarations in one file.
func (w *fileWalk) findShapes() {
	s := w.e.shapes
	ast.Inspect(w.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			obj, ok := w.pkg.info.Defs[n.Name].(*types.Func)
			if !ok || n.Body == nil {
				return true
			}
			sig := obj.Type().(*types.Signature)
			if sig.Results().Len() == 0 || !isOptionSlice(sig.Results().At(0).Type()) {
				return true
			}
			it := s.item(w, n.Name, true)
			if it == nil {
				return true
			}
			first := n.Type.Results.List[0]
			if _, ok := w.accessRuleType(first.Type); !ok {
				it.refuse("its result type is not written as []http.GuardOption[M]")
			}
			if len(first.Names) > 1 {
				it.refuse("its first result shares a declaration with another name")
			}
			returns := 0
			ast.Inspect(n.Body, func(m ast.Node) bool {
				switch m := m.(type) {
				case *ast.FuncLit:
					return false
				case *ast.ReturnStmt:
					returns++
					if len(m.Results) == 0 {
						it.refuse("it has a bare return at " + w.line(m))
						return true
					}
					if len(m.Results) == 1 && sig.Results().Len() > 1 {
						it.refuse("it returns the results of another call at " + w.line(m))
						return true
					}
					if _, why := w.literalRule(m.Results[0]); why != "" {
						text := w.text(m.Results[0])
						if len(text) > 40 {
							text = text[:40] + "..."
						}
						if id, ok := m.Results[0].(*ast.Ident); ok && id.Name == "nil" {
							it.refuse("it returns nil at " + w.line(m) + ": returning no options is the open case, a decision for a person (AnyActor() or a capability)")
						} else {
							it.refuse("the value it returns at " + w.line(m) + " (" + text + ") " + why)
						}
					}
				}
				return true
			})
			if returns == 0 {
				it.refuse("it has no return statement")
			}
		case *ast.AssignStmt:
			w.findVars(n.Lhs, n.Rhs, nil)
		case *ast.ValueSpec:
			w.findVars(identExprs(n.Names), n.Values, n.Type)
		}
		return true
	})
}

func identExprs(ids []*ast.Ident) []ast.Expr {
	out := make([]ast.Expr, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func (w *fileWalk) findVars(lhs, rhs []ast.Expr, declared ast.Expr) {
	s := w.e.shapes
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := w.pkg.info.Defs[id].(*types.Var)
		if !ok || v.IsField() || !isOptionSlice(v.Type()) {
			continue
		}
		it := s.item(w, id, false)
		if it == nil {
			continue
		}
		if declared != nil {
			it.refuse("it is declared with an explicit type")
		}
		var value ast.Expr
		switch {
		case len(rhs) == len(lhs):
			value = rhs[i]
		case len(rhs) == 1 && i == 0:
			value = rhs[0]
		}
		if value == nil {
			it.refuse("it is declared without a value the codemod can read")
			continue
		}
		if call, ok := value.(*ast.CallExpr); ok {
			if fn, ok := w.callFunc(call); ok {
				it.helper = s.objectKey(fn)
				continue
			}
		}
		if len(rhs) != len(lhs) {
			it.refuse("it is assigned from a multi-value expression")
			continue
		}
		if _, why := w.literalRule(value); why != "" {
			it.refuse("the value assigned at " + w.line(value) + " " + why)
		}
	}
}

// callFunc returns the declared function or method that call invokes.
func (w *fileWalk) callFunc(c *ast.CallExpr) (*types.Func, bool) {
	var id *ast.Ident
	switch f := c.Fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil, false
	}
	fn, ok := w.pkg.info.Uses[id].(*types.Func)
	return fn, ok
}

// recognized reports the position of the options argument of a call the codemod
// rewrites, so a shape is used only where its rewrite is defined.
func (w *fileWalk) recognized(c *ast.CallExpr, arg ast.Expr) bool {
	if !c.Ellipsis.IsValid() || len(c.Args) == 0 || c.Args[len(c.Args)-1] != arg {
		return false
	}
	fn, ok := w.callee(c)
	if !ok {
		return false
	}
	if fixed, ok := constructors[fn]; ok {
		return len(c.Args) == fixed+1
	}
	return configurers[fn] && len(c.Args) == 2
}

// checkShapeUses refuses the shapes that are used where the rewrite is undefined.
func (w *fileWalk) checkShapeUses() {
	s := w.e.shapes
	ast.Inspect(w.file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := w.pkg.info.Uses[id]
		if obj == nil {
			return true
		}
		it := s.items[s.objectKey(obj)]
		if it == nil {
			return true
		}
		if it.isFunc {
			w.checkHelperUse(it, id)
		} else {
			w.checkVarUse(it, id)
		}
		return true
	})
}

func (w *fileWalk) checkVarUse(it *shape, id *ast.Ident) {
	if as, ok := w.parents[id].(*ast.AssignStmt); ok {
		for _, l := range as.Lhs {
			if l == id {
				it.refuse("it is reassigned at " + w.line(id))
				return
			}
		}
	}
	if c, ok := w.parents[id].(*ast.CallExpr); ok && w.recognized(c, id) {
		return
	}
	it.refuse("it is used at " + w.line(id) + " other than as the single options argument of an access constructor or configure call")
}

func (w *fileWalk) checkHelperUse(it *shape, id *ast.Ident) {
	var call *ast.CallExpr
	var node ast.Node = id
	if sel, ok := w.parents[id].(*ast.SelectorExpr); ok && sel.Sel == id {
		node = sel
	}
	if c, ok := w.parents[node].(*ast.CallExpr); ok && c.Fun == node {
		call = c
	}
	if call == nil {
		it.refuse("it is used as a value at " + w.line(id))
		return
	}
	switch p := w.parents[call].(type) {
	case *ast.AssignStmt:
		if len(p.Rhs) == 1 && p.Rhs[0] == call {
			if v, ok := p.Lhs[0].(*ast.Ident); ok {
				if obj := w.pkg.info.ObjectOf(v); obj != nil {
					it.deps = append(it.deps, w.e.shapes.objectKey(obj))
					return
				}
			}
		}
	case *ast.ValueSpec:
		if len(p.Values) == 1 && p.Values[0] == call && len(p.Names) > 0 {
			if obj := w.pkg.info.ObjectOf(p.Names[0]); obj != nil {
				it.deps = append(it.deps, w.e.shapes.objectKey(obj))
				return
			}
		}
	case *ast.CallExpr:
		if w.recognized(p, call) {
			if fn, ok := w.pkg.info.Uses[id].(*types.Func); ok && fn.Type().(*types.Signature).Results().Len() == 1 {
				return
			}
		}
	}
	it.refuse("its result is used at " + w.line(call) + " other than as an options slice variable or the single options argument of an access constructor")
}

// convertible reports an options slice, or a call to a helper returning one, that
// this run turns into the rule.
func (s *shapes) convertible(pkg sourcePackage, e ast.Expr) bool {
	it := s.lookup(pkg, e)
	return it != nil && it.reason == ""
}

// whyNot explains, for a message about a slice that was passed as name..., why
// the slice was not converted.
func (s *shapes) whyNot(pkg sourcePackage, e ast.Expr) string {
	if it := s.lookup(pkg, e); it != nil && it.reason != "" {
		return "; " + it.name + " was not converted: " + it.reason
	}
	return ""
}

func (s *shapes) lookup(pkg sourcePackage, e ast.Expr) *shape {
	switch e := e.(type) {
	case *ast.Ident:
		return s.items[s.objectKey(pkg.info.ObjectOf(e))]
	case *ast.CallExpr:
		var id *ast.Ident
		switch f := e.Fun.(type) {
		case *ast.Ident:
			id = f
		case *ast.SelectorExpr:
			id = f.Sel
		}
		if id == nil {
			return nil
		}
		fn, ok := pkg.info.Uses[id].(*types.Func)
		if !ok {
			return nil
		}
		if fn.Type().(*types.Signature).Results().Len() != 1 {
			return nil
		}
		return s.items[s.objectKey(fn)]
	}
	return nil
}

// shapesLeft excludes the shape behind an options slice whose use was left, so the
// next pass leaves the declaration alone as well.
func (w *fileWalk) shapesLeft(c *ast.CallExpr, reason string) {
	if !c.Ellipsis.IsValid() || len(c.Args) == 0 {
		return
	}
	arg := c.Args[len(c.Args)-1]
	it := w.e.shapes.lookup(w.pkg, arg)
	if it == nil || it.reason != "" {
		return
	}
	w.e.shapes.pending[it.key] = "its use at " + w.line(c) + " was left: " + reason
}

// rewriteShapes turns the convertible declarations of this file into rules.
func (w *fileWalk) rewriteShapes() {
	s := w.e.shapes
	ast.Inspect(w.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			obj, ok := w.pkg.info.Defs[n.Name].(*types.Func)
			if !ok {
				return true
			}
			if it := s.items[s.objectKey(obj)]; it != nil && it.isFunc && it.reason == "" {
				w.rewriteHelper(n)
			}
		case *ast.AssignStmt:
			w.rewriteVars(n.Lhs, n.Rhs)
		case *ast.ValueSpec:
			w.rewriteVars(identExprs(n.Names), n.Values)
		}
		return true
	})
}

func (w *fileWalk) rewriteHelper(fd *ast.FuncDecl) {
	first := fd.Type.Results.List[0]
	text, ok := w.accessRuleType(first.Type)
	if !ok {
		return
	}
	w.add(w.offset(first.Type.Pos()), w.offset(first.Type.End()), text)
	ast.Inspect(fd.Body, func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if rule, why := w.literalRule(m.Results[0]); why == "" {
				w.add(w.offset(m.Results[0].Pos()), w.offset(m.Results[0].End()), rule)
			}
		}
		return true
	})
}

func (w *fileWalk) rewriteVars(lhs, rhs []ast.Expr) {
	s := w.e.shapes
	for i, l := range lhs {
		id, ok := l.(*ast.Ident)
		if !ok || len(rhs) != len(lhs) {
			continue
		}
		v, ok := w.pkg.info.Defs[id].(*types.Var)
		if !ok || !isOptionSlice(v.Type()) {
			continue
		}
		it := s.items[s.objectKey(v)]
		if it == nil || it.reason != "" || it.helper != "" {
			continue
		}
		if rule, why := w.literalRule(rhs[i]); why == "" {
			w.add(w.offset(rhs[i].Pos()), w.offset(rhs[i].End()), rule)
			w.e.findings[finding{w.e.fset.Position(rhs[i].Pos()).Filename, w.e.fset.Position(rhs[i].Pos()).Line, w.e.fset.Position(rhs[i].Pos()).Column, true, true, it.name + ": the options slice is now the rule"}] = true
		}
	}
}

// reportShapes lists the declarations that stayed options slices.
func (e *engine) reportShapes() {
	for _, it := range e.shapes.items {
		if it.reason == "" {
			continue
		}
		p := e.fset.Position(it.pos)
		if _, ok := e.src[p.Filename]; !ok {
			continue
		}
		kind := "the options slice "
		recipe := "; build it with http.AllOf(...), http.AnyOf(...) or the binding's AnyActor() as an http.AccessRule"
		if it.isFunc {
			kind = "the helper "
			recipe = "; make it return an http.AccessRule: http.AllOf(...) for one rule, the binding's AnyActor() for the open case"
		}
		e.findings[finding{p.Filename, p.Line, p.Column, false, true, kind + it.name + " was not converted: " + it.reason + recipe}] = true
	}
}
