package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"
)

const (
	httpPath              = "github.com/weiloon1234/Foundry-Go/http"
	paginationPath        = httpPath + "/pagination"
	datatablePath         = "github.com/weiloon1234/Foundry-Go/datatable"
	webhookCapturePath    = "github.com/weiloon1234/Foundry-Go/webhook/capture"
	httpclientCapturePath = "github.com/weiloon1234/Foundry-Go/httpclient/capture"
)

type callee struct{ pkg, name string }

// constructors maps every function that took optional access options to the
// number of arguments before them; the access rule now sits at that position.
var constructors = map[callee]int{
	{httpPath, "Authenticated"}:       2,
	{httpPath, "TicketAuthenticated"}: 3,
	{paginationPath, "Authenticated"}: 2,
	{datatablePath, "ReadAccess"}:     0,
	{datatablePath, "ExportAccess"}:   0,
	{webhookCapturePath, "Routes"}:    3,
	{httpclientCapturePath, "Routes"}: 3,
}

// removed maps the guard-taking constructors, which no longer exist, to the
// spelling that replaces them. They need a binding, which only the developer can
// supply, so every call is listed.
var removed = map[callee]string{
	{httpPath, "RequireAuthentication"}:       "take a binding from http.BindGuard(authentication, guard) once and write http.Authenticated(endpoint, binding, rule)",
	{httpPath, "RequireRouteAuthentication"}:  "take a binding from http.BindGuard(authentication, guard) once and write http.RouteAuthenticated(route, binding, rule)",
	{httpPath, "OptionalAuthentication"}:      "take a binding from http.BindGuard(authentication, guard) once and write http.OptionallyAuthenticated(endpoint, binding)",
	{httpPath, "OptionalRouteAuthentication"}: "take a binding from http.BindGuard(authentication, guard) once and write http.RouteOptionallyAuthenticated(route, binding)",
}

// folding constructors accept a later Configure call on the value they return.
var folding = map[callee]bool{
	{httpPath, "Authenticated"}:       true,
	{httpPath, "TicketAuthenticated"}: false,
	{paginationPath, "Authenticated"}: true,
}

var configurers = map[callee]bool{
	{httpPath, "ConfigureAuthenticated"}:       true,
	{httpPath, "ConfigureAuthenticatedRoute"}:  true,
	{paginationPath, "ConfigureAuthenticated"}: true,
}

type edit struct {
	start, end int
	text       string
}

type finding struct {
	file      string
	line, col int
	rewritten bool
	decl      bool // a declaration of an options slice or helper, not a call
	text      string
}

type optionParts struct {
	reqs    []ast.Expr
	anyOf   ast.Expr
	actors  []ast.Expr
	unknown ast.Expr
	// spread is an options slice, passed as name..., that becomes the rule itself.
	spread ast.Expr
}

func (o optionParts) hasAccess() bool { return len(o.reqs) > 0 || o.anyOf != nil || o.spread != nil }

// engine rewrites one source package at a time.
type engine struct {
	fset     *token.FileSet
	src      map[string][]byte
	edits    map[string]map[edit]bool
	findings map[finding]bool
	shapes   *shapes
}

type fileWalk struct {
	e       *engine
	pkg     sourcePackage
	file    *ast.File
	name    string
	src     []byte
	parents map[ast.Node]ast.Node
	folded  map[*ast.CallExpr]bool
	reasons map[*ast.CallExpr]string
	leftAt  map[*ast.CallExpr]string
}

func (e *engine) newWalk(pkg sourcePackage, file *ast.File) *fileWalk {
	name := e.fset.Position(file.Pos()).Filename
	w := &fileWalk{e: e, pkg: pkg, file: file, name: name, src: e.src[name], parents: map[ast.Node]ast.Node{}, folded: map[*ast.CallExpr]bool{}, reasons: map[*ast.CallExpr]string{}, leftAt: map[*ast.CallExpr]string{}}
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			w.parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return w
}

func (e *engine) visit(pkg sourcePackage, file *ast.File) {
	w := e.newWalk(pkg, file)
	w.rewriteShapes()
	var calls []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			calls = append(calls, c)
		}
		return true
	})
	for _, c := range calls {
		if fn, ok := w.callee(c); ok {
			if replacement, gone := removed[fn]; gone {
				w.left(c, fn.name+" no longer exists: "+replacement+"; the codemod cannot create the binding")
			}
			if fixed, isCtor := constructors[fn]; isCtor {
				w.constructor(c, fn, fixed)
			}
			if fn == (callee{datatablePath, "Routes"}) {
				w.datatableRoutes(c)
			}
		}
	}
	for _, c := range calls {
		fn, ok := w.callee(c)
		if !ok || !configurers[fn] || w.folded[c] {
			continue
		}
		parts := w.classifyCall(c, 1)
		if !parts.hasAccess() && parts.unknown == nil {
			continue
		}
		reason := w.reasons[c]
		if reason == "" {
			reason = "its value was not assigned from a constructor call in this function"
		}
		w.left(c, fn.name+" with access options: "+reason)
	}
	for c, reason := range w.leftAt {
		w.shapesLeft(c, reason)
	}
}

func (w *fileWalk) callee(c *ast.CallExpr) (callee, bool) {
	fun := c.Fun
	for {
		switch f := fun.(type) {
		case *ast.ParenExpr:
			fun = f.X
			continue
		case *ast.IndexExpr:
			fun = f.X
			continue
		case *ast.IndexListExpr:
			fun = f.X
			continue
		}
		break
	}
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return callee{}, false
	}
	fn, ok := w.pkg.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return callee{}, false
	}
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return callee{}, false
	}
	return callee{fn.Pkg().Path(), fn.Name()}, true
}

func (w *fileWalk) text(n ast.Node) string {
	return string(w.src[w.e.fset.Position(n.Pos()).Offset:w.e.fset.Position(n.End()).Offset])
}

func (w *fileWalk) offset(p token.Pos) int { return w.e.fset.Position(p).Offset }

func (w *fileWalk) add(start, end int, text string) {
	if w.e.edits[w.name] == nil {
		w.e.edits[w.name] = map[edit]bool{}
	}
	w.e.edits[w.name][edit{start, end, text}] = true
}

func (w *fileWalk) record(n ast.Node, rewritten bool, text string) {
	p := w.e.fset.Position(n.Pos())
	w.e.findings[finding{p.Filename, p.Line, p.Column, rewritten, false, text}] = true
}

func (w *fileWalk) left(n ast.Node, reason string) {
	if c, ok := n.(*ast.CallExpr); ok {
		w.leftAt[c] = reason
	}
	w.record(n, false, reason)
}

// migrated reports a constructor that already takes the rule, so a second run
// over a migrated module changes nothing.
func (w *fileWalk) migrated(c *ast.CallExpr, fixed int) bool {
	sig, ok := w.pkg.info.TypeOf(c.Fun).(*types.Signature)
	if !ok || sig.Params().Len() <= fixed {
		return false
	}
	named, ok := sig.Params().At(fixed).Type().(*types.Named)
	return ok && named.Obj().Name() == "AccessRule" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == httpPath
}

// classifyCall classifies the option arguments of c that follow its first fixed
// arguments, including an options slice passed as name... that the codemod turns
// into the rule.
func (w *fileWalk) classifyCall(c *ast.CallExpr, fixed int) optionParts {
	if c.Ellipsis.IsValid() && len(c.Args) == fixed+1 {
		if w.e.shapes.convertible(w.pkg, c.Args[fixed]) {
			return optionParts{spread: c.Args[fixed]}
		}
		return optionParts{unknown: c.Args[fixed]}
	}
	return w.classify(c.Args[fixed:])
}

func (w *fileWalk) classify(args []ast.Expr) optionParts {
	var parts optionParts
	for _, a := range args {
		switch w.kindOf(a) {
		case "requirement":
			parts.reqs = append(parts.reqs, a)
		case "anyof":
			if parts.anyOf != nil {
				parts.unknown = a
			}
			parts.anyOf = a
		case "actor":
			parts.actors = append(parts.actors, a)
		default:
			parts.unknown = a
		}
	}
	return parts
}

func (w *fileWalk) kindOf(a ast.Expr) string {
	if c, ok := a.(*ast.CallExpr); ok {
		if fn, ok := w.callee(c); ok && fn.pkg == httpPath {
			switch fn.name {
			case "Scopes", "Permissions":
				return "requirement"
			case "AnyOf":
				return "anyof"
			case "ActorMiddleware":
				return "actor"
			}
		}
	}
	if named, ok := w.pkg.info.TypeOf(a).(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == httpPath {
		switch named.Obj().Name() {
		case "RequirementOption":
			return "requirement"
		case "ActorOption":
			return "actor"
		}
	}
	return ""
}

func (w *fileWalk) qualifier() (string, bool) {
	if w.pkg.path == httpPath {
		return "", true
	}
	for _, spec := range w.file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || p != httpPath {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			return "", false
		}
		return name + ".", true
	}
	return "", false
}

// ruleText is the source of the access rule for parts, or a reason it cannot
// be built.
func (w *fileWalk) ruleText(parts optionParts) (string, string) {
	switch {
	case parts.spread != nil:
		return w.text(parts.spread), ""
	case parts.anyOf != nil && len(parts.reqs) > 0:
		return "", "AnyOf is mixed with Scopes/Permissions; the old API refused this combination"
	case parts.anyOf != nil:
		return w.text(parts.anyOf), ""
	}
	q, ok := w.qualifier()
	if !ok {
		return "", "the file does not import the http package under a usable name; write http.AllOf by hand"
	}
	texts := make([]string, len(parts.reqs))
	for i, r := range parts.reqs {
		texts[i] = w.text(r)
	}
	return q + "AllOf(" + strings.Join(texts, ", ") + ")", ""
}

func (w *fileWalk) constructor(c *ast.CallExpr, fn callee, fixed int) {
	if len(c.Args) < fixed || w.migrated(c, fixed) {
		return
	}
	parts := w.classifyCall(c, fixed)
	if c.Ellipsis.IsValid() && len(c.Args) > fixed && parts.spread == nil {
		w.left(c, fn.name+": access options are passed as a slice (...)"+w.e.shapes.whyNot(w.pkg, c.Args[len(c.Args)-1])+"; build the rule by hand")
		return
	}
	if parts.unknown != nil {
		w.left(c, fn.name+": an option is not a Scopes, Permissions, AnyOf or ActorMiddleware call (type "+types.TypeString(w.pkg.info.TypeOf(parts.unknown), nil)+"); build the rule by hand")
		return
	}
	if !folding[fn] {
		w.plainConstructor(c, fn, fixed, parts)
		return
	}
	k, reason := w.findConfigure(c)
	found := k
	var kparts optionParts
	if k != nil {
		kparts = w.classifyCall(k, 1)
		switch {
		case k.Ellipsis.IsValid() && kparts.spread == nil:
			reason, k = "options are passed as a slice (...)"+w.e.shapes.whyNot(w.pkg, k.Args[len(k.Args)-1]), nil
		case kparts.spread != nil && (parts.anyOf != nil || len(parts.reqs) > 0 || parts.spread != nil):
			reason, k = "the constructor and the configure call both declare access", nil
		case kparts.unknown != nil:
			reason, k = "an option is not a Scopes, Permissions, AnyOf or ActorMiddleware call", nil
		case kparts.anyOf != nil && (parts.anyOf != nil || len(parts.reqs) > 0), parts.anyOf != nil && len(kparts.reqs) > 0:
			reason, k = "AnyOf cannot be combined with other requirements", nil
		}
	}
	if k == nil && found != nil {
		w.reasons[found] = reason
	}
	combined := parts
	if k != nil {
		combined.reqs = append(append([]ast.Expr{}, parts.reqs...), kparts.reqs...)
		if kparts.anyOf != nil {
			combined.anyOf = kparts.anyOf
		}
		if kparts.spread != nil {
			combined.spread = kparts.spread
		}
		combined.actors = append(append([]ast.Expr{}, parts.actors...), kparts.actors...)
	}
	if !combined.hasAccess() {
		switch {
		case reason != "":
			w.left(c, fn.name+": no access rule and the later configure call could not be folded: "+reason)
		case w.returned(c):
			w.left(c, fn.name+": the result is returned from a helper, so the helper decides access; give it a rule parameter or write AnyActor() where it is open")
		default:
			w.left(c, fn.name+": no scopes or permissions; open to any authenticated actor? write binding.AnyActor(), or add the rule")
		}
		return
	}
	rule, why := w.ruleText(combined)
	if why != "" {
		w.left(c, fn.name+": "+why)
		return
	}
	w.setRule(c, fixed, rule, parts.actors)
	summary := "wrapped the access options in the rule"
	if parts.anyOf != nil {
		summary = "kept AnyOf as the rule"
	}
	if combined.spread != nil {
		summary = "the options slice is now the rule"
	}
	if k != nil {
		w.fold(c, k, kparts)
		summary += "; folded " + w.calleeName(k) + " at line " + strconv.Itoa(w.e.fset.Position(k.Pos()).Line)
	}
	w.record(c, true, fn.name+": "+summary)
}

// datatableRoutes lists a table mount with no ReadAccess: the read rule is now a
// required argument, and "any actor" is a decision for a person.
func (w *fileWalk) datatableRoutes(c *ast.CallExpr) {
	const fixed = 3
	if len(c.Args) < fixed || w.migrated(c, fixed) {
		return
	}
	if c.Ellipsis.IsValid() {
		w.left(c, "Routes: access options are passed as a slice (...); pass datatable.ReadAccess(rule) and datatable.ExportAccess(rule)")
		return
	}
	for _, a := range c.Args[fixed:] {
		if call, ok := a.(*ast.CallExpr); ok {
			if fn, ok := w.callee(call); ok && fn == (callee{datatablePath, "ReadAccess"}) {
				return
			}
		}
	}
	w.left(c, "Routes: ReadAccess is now required after the config; write datatable.ReadAccess(rule), or datatable.ReadAccess(config.Guard.AnyActor()) for a table every actor may read")
}

func (w *fileWalk) calleeName(c *ast.CallExpr) string {
	if fn, ok := w.callee(c); ok {
		return fn.name
	}
	return "the call"
}

// plainConstructor rewrites a constructor that has no later Configure form:
// ticket constructors and the datatable and capture mounts.
func (w *fileWalk) plainConstructor(c *ast.CallExpr, fn callee, fixed int, parts optionParts) {
	if !parts.hasAccess() {
		w.left(c, fn.name+": no scopes or permissions; open to any authenticated actor? write binding.AnyActor(), or add the rule")
		return
	}
	rule, why := w.ruleText(parts)
	if why != "" {
		w.left(c, fn.name+": "+why)
		return
	}
	if (fn.pkg == datatablePath || fn.pkg == webhookCapturePath || fn.pkg == httpclientCapturePath) && len(parts.actors) > 0 {
		w.left(c, fn.name+": actor middleware is not accepted here; move it to the route that needs it")
		return
	}
	w.setRule(c, fixed, rule, parts.actors)
	summary := "wrapped the access options in the rule"
	if parts.anyOf != nil {
		summary = "kept AnyOf as the rule"
	}
	if parts.spread != nil {
		summary = "the options slice is now the rule"
	}
	w.record(c, true, fn.name+": "+summary)
}

// setRule replaces the option arguments of c with the rule followed by the actor options.
func (w *fileWalk) setRule(c *ast.CallExpr, fixed int, rule string, actors []ast.Expr) {
	tail := rule
	for _, a := range actors {
		tail += ", " + w.text(a)
	}
	if len(c.Args) > fixed {
		end := w.offset(c.Args[len(c.Args)-1].End())
		if c.Ellipsis.IsValid() {
			end = w.offset(c.Ellipsis) + len("...")
		}
		w.add(w.offset(c.Args[fixed].Pos()), end, tail)
		return
	}
	rparen := w.offset(c.Rparen)
	if fixed == 0 {
		w.add(rparen, rparen, tail)
		return
	}
	lastEnd := w.offset(c.Args[fixed-1].End())
	if i := strings.IndexByte(string(w.src[lastEnd:rparen]), ','); i >= 0 {
		w.add(lastEnd+i+1, lastEnd+i+1, " "+tail+",")
		return
	}
	w.add(rparen, rparen, ", "+tail)
}

// returned reports a constructor call that is the operand of a return statement.
func (w *fileWalk) returned(c *ast.CallExpr) bool {
	var n ast.Node = c
	for {
		p := w.parents[n]
		switch p := p.(type) {
		case *ast.ParenExpr:
			n = p
			continue
		case *ast.ReturnStmt:
			return true
		}
		return false
	}
}

// configureOf returns the Configure call that wraps c directly.
func (w *fileWalk) configureOf(c *ast.CallExpr) *ast.CallExpr {
	var n ast.Node = c
	for {
		p := w.parents[n]
		if paren, ok := p.(*ast.ParenExpr); ok {
			n = paren
			continue
		}
		if k, ok := p.(*ast.CallExpr); ok && len(k.Args) > 0 && k.Args[0] == n {
			if fn, ok := w.callee(k); ok && configurers[fn] {
				return k
			}
		}
		return nil
	}
}

func (w *fileWalk) enclosingFunc(n ast.Node) ast.Node {
	for p := w.parents[n]; p != nil; p = w.parents[p] {
		switch p.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return p
		}
	}
	return nil
}

// findConfigure locates the single Configure call that can fold into c: the call
// that wraps c, or the one applied to the local variable c was assigned to. When
// it cannot be folded safely the second result says why.
func (w *fileWalk) findConfigure(c *ast.CallExpr) (*ast.CallExpr, string) {
	if k := w.configureOf(c); k != nil {
		return k, ""
	}
	ident, ok := w.assignedIdent(c)
	if !ok {
		return nil, ""
	}
	obj := w.pkg.info.ObjectOf(ident)
	if obj == nil {
		return nil, ""
	}
	if v, ok := obj.(*types.Var); !ok || v.IsField() || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
		return nil, "the value is not a local variable"
	}
	fn := w.enclosingFunc(c)
	if fn == nil {
		return nil, ""
	}
	var configures []*ast.CallExpr
	var uses []*ast.Ident
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if n != ident && w.pkg.info.ObjectOf(n) == obj {
				uses = append(uses, n)
			}
		case *ast.CallExpr:
			if f, ok := w.callee(n); ok && configurers[f] && len(n.Args) > 0 {
				if id, ok := n.Args[0].(*ast.Ident); ok && w.pkg.info.ObjectOf(id) == obj {
					configures = append(configures, n)
				}
			}
		}
		return true
	})
	switch len(configures) {
	case 0:
		return nil, ""
	case 1:
	default:
		for _, k := range configures {
			w.reasons[k] = "the value is configured more than once"
		}
		return nil, "the value is configured more than once"
	}
	k := configures[0]
	reject := func(reason string) (*ast.CallExpr, string) {
		w.reasons[k] = reason
		return nil, reason
	}
	if w.enclosingFunc(k) != w.enclosingFunc(c) {
		return reject("it is configured inside a function literal")
	}
	if k.Pos() < c.End() {
		return reject("it is configured before it is assigned")
	}
	if w.conditional(k, c) {
		return reject("it is configured conditionally")
	}
	selfAssign := false
	if as, ok := w.parents[k].(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 && as.Rhs[0] == k {
		if id, ok := as.Lhs[0].(*ast.Ident); ok && w.pkg.info.ObjectOf(id) == obj {
			selfAssign = true
		}
	}
	for _, u := range uses {
		inK := u.Pos() >= k.Pos() && u.End() <= k.End()
		if inK {
			continue
		}
		if selfAssign && w.parents[u] == w.parents[k] {
			continue // the assignment target
		}
		if u.Pos() > c.End() && u.Pos() < k.Pos() {
			return reject("the value is used before it is configured")
		}
		if u.Pos() > k.End() && !selfAssign {
			return reject("the value is also used unconfigured after the configure call")
		}
	}
	return k, ""
}

// assignedIdent returns the variable that c initialises or is assigned to.
func (w *fileWalk) assignedIdent(c *ast.CallExpr) (*ast.Ident, bool) {
	switch p := w.parents[c].(type) {
	case *ast.AssignStmt:
		if len(p.Lhs) == 1 && len(p.Rhs) == 1 && p.Rhs[0] == c {
			id, ok := p.Lhs[0].(*ast.Ident)
			return id, ok
		}
	case *ast.ValueSpec:
		if len(p.Names) == 1 && len(p.Values) == 1 && p.Values[0] == c {
			return p.Names[0], true
		}
	}
	return nil, false
}

// conditional reports whether k runs under a branch or loop that the
// constructor statement does not share, which folding would make unconditional.
func (w *fileWalk) conditional(k, c *ast.CallExpr) bool {
	var home ast.Node
	for p := w.parents[c]; p != nil; p = w.parents[p] {
		if b, ok := p.(*ast.BlockStmt); ok {
			home = b
			break
		}
	}
	for p := w.parents[k]; p != nil && p != home; p = w.parents[p] {
		switch p.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.CaseClause, *ast.CommClause, *ast.FuncLit:
			return true
		case *ast.BinaryExpr:
			if b := p.(*ast.BinaryExpr); b.Op == token.LAND || b.Op == token.LOR {
				return true
			}
		}
	}
	return false
}

// fold removes the access options of k, and k itself when nothing remains.
func (w *fileWalk) fold(c, k *ast.CallExpr, parts optionParts) {
	w.folded[k] = true
	if len(parts.actors) > 0 {
		var texts []string
		for _, a := range parts.actors {
			texts = append(texts, w.text(a))
		}
		w.add(w.offset(k.Args[1].Pos()), w.offset(k.Args[len(k.Args)-1].End()), strings.Join(texts, ", "))
		return
	}
	if as, ok := w.parents[k].(*ast.AssignStmt); ok && len(as.Lhs) == 1 && as.Rhs[0] == k {
		if id, ok := as.Lhs[0].(*ast.Ident); ok {
			if arg, ok := k.Args[0].(*ast.Ident); ok && arg.Name == id.Name {
				w.deleteStatement(as)
				return
			}
		}
	}
	w.add(w.offset(k.Pos()), w.offset(k.Args[0].Pos()), "")
	w.add(w.offset(k.Args[0].End()), w.offset(k.End()), "")
}

func (w *fileWalk) deleteStatement(s ast.Stmt) {
	start, end := w.offset(s.Pos()), w.offset(s.End())
	lineStart := start
	for lineStart > 0 && (w.src[lineStart-1] == ' ' || w.src[lineStart-1] == '\t') {
		lineStart--
	}
	if lineStart == 0 || w.src[lineStart-1] == '\n' {
		start = lineStart
	}
	rest := end
	for rest < len(w.src) && w.src[rest] != '\n' {
		rest++
	}
	tail := strings.TrimSpace(string(w.src[end:rest]))
	if tail == "" || strings.HasPrefix(tail, "//") {
		end = rest
		if end < len(w.src) {
			end++
		}
	}
	w.add(start, end, "")
}

func (f finding) String() string {
	kind := "left"
	if f.rewritten {
		kind = "rewritten"
	}
	if f.decl {
		kind += " declaration"
	}
	return fmt.Sprintf("%s:%d:%d: %s: %s", f.file, f.line, f.col, kind, f.text)
}
