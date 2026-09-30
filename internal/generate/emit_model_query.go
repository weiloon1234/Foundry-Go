package generate

import (
	"fmt"
	"go/types"
	"strings"
)

// fieldCodec is the single emission path for predicate binding and hydration.
// nullable is false for field operators, whose Eq/Set values are non-null T.
func (e *emitter) fieldCodec(f field, nullable bool) string {
	// Hash-only projections need no database/codec import unless nullable.
	if passwordHash(f.base) {
		expression := e.use(framework+"/auth/password") + ".Codec()"
		if nullable && f.nullable {
			expression = e.use(framework+"/database/codec") + ".Nullable(" + expression + ")"
		}
		return expression
	}
	codec := e.use(framework + "/database/codec")
	base := types.Unalias(f.base)
	typeName := e.typeName(f.base)
	var expression string
	if binaryType(f.base) {
		expression = fmt.Sprintf("%s.Bytes[%s]()", codec, typeName)
	}
	if named, ok := base.(*types.Named); ok {
		switch {
		case isNamed(named, framework+"/value", "JSON"):
			expression = fmt.Sprintf("%s.JSON[%s]()", codec, e.typeName(named.TypeArgs().At(0)))
		case isNamed(named, framework+"/model", "ID"):
			expression = fmt.Sprintf("%s.ID[%s]()", codec, e.typeName(named.TypeArgs().At(0)))
		case isNamed(named, framework+"/decimal", "Decimal"):
			expression = codec + ".Decimal()"
		case isNamed(named, "time", "Time"):
			expression = codec + ".Time()"
		case named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == framework+"/temporal":
			name := named.Obj().Name()
			if name == "Time" {
				name = "WallTime"
			}
			expression = codec + "." + name + "()"
		}
	}
	if expression == "" {
		scalar := base.Underlying().(*types.Basic) // discovery rejects other types
		class := "Signed"
		switch {
		case scalar.Kind() == types.String:
			class = "String"
		case scalar.Kind() == types.Bool:
			class = "Bool"
		case scalar.Info()&types.IsUnsigned != 0:
			class = "Unsigned"
		case scalar.Info()&types.IsFloat != 0:
			class = "Float"
		}
		expression = fmt.Sprintf("%s.%s[%s]()", codec, class, typeName)
	}
	if f.enum {
		expression += ".Validated(" + typeName + ".Validate)"
	}
	if nullable && f.nullable {
		expression = codec + ".Nullable(" + expression + ")"
	}
	return expression
}

func (e *emitter) emitModelQuery(m model) {
	query, database := e.use(framework+"/database/query"), e.use(framework+"/database")
	context, value := e.use("context"), e.use(framework+"/value")
	rowVar, itemVar := e.localName("row"), e.localName("item")
	var primary field
	columns := make([]string, len(m.fields))
	for i, f := range m.fields {
		columns[i] = fmt.Sprintf("%s.Column{Name:%q,Nullable:%t,DatabaseDefault:%t}", query, f.column, f.nullable, f.databaseDefault)
		if f.primary {
			primary = f
		}
	}
	// Codecs are built once with the declaration, not once per hydrated row.
	codecs := make([]string, len(m.fields))
	for i := range m.fields {
		codecs[i] = e.localName(fmt.Sprintf("foundryCodec%d", i))
	}
	e.line("// %sQuery preserves model-specific keys through fluent query derivation.", m.name)
	e.line("type %sQuery struct{%s.Query[%s]}", m.name, query, m.name)
	e.line("// foundry%sModelQuery holds the immutable declaration, hydration codecs and field metadata, built once on first use.", m.name)
	e.line("var foundry%sModelQuery %s.Memo[%s.Query[%s]]", m.name, query, query, m.name)
	e.line("func foundry%sBuildQuery()%s.Query[%s]{", m.name, query, m.name)
	for i, f := range m.fields {
		e.line("%s:=%s", codecs[i], e.fieldCodec(f, true))
	}
	e.line("return %s.ForModel(%s.Define[%s](%q,%q,[]%s.Column{%s},func(%s %s.Row)(%s,error){", query, query, m.name, m.table, primary.column, query, strings.Join(columns, ","), rowVar, database, m.name)
	e.line("var %s %s", itemVar, m.name)
	e.line("if err:=%s.Scan(", rowVar)
	for i, f := range m.fields {
		e.line("%s.Scan(&%s.%s),", codecs[i], itemVar, f.name)
	}
	e.line(");err!=nil{return %s{},err};return %s,nil", m.name, itemVar)
	e.line("},")
	for i, f := range m.fields {
		if f.mutator == "" {
			e.line("%s.NewModelField(%q,%s,func(item %s)%s{return item.%s}),", query, f.column, codecs[i], m.name, e.typeName(f.typ), f.name)
			continue
		}
		factory := "NewMutatedModelField"
		if f.nullable {
			factory = "NewNullableMutatedModelField"
		}
		if f.input != nil {
			factory = "NewInputModelField"
			if f.nullable {
				factory = "NewNullableInputModelField"
			}
		}
		e.line("%s.%s(%q,%s,func(item %s)%s{return item.%s},(%s{}).%s),", query, factory, f.column, e.fieldCodec(f, false), m.name, e.typeName(f.typ), f.name, m.name, f.mutator)
	}
	behavior := ""
	if m.timestamps[0] != "" {
		behavior = fmt.Sprintf(".WithTimestamps(%q,%q)", m.timestamps[0], m.timestamps[1])
	}
	if m.softDelete != "" {
		behavior += fmt.Sprintf(".WithSoftDeletes(%q)", m.softDelete)
	}
	// A handwritten value-receiver DefineGlobalScopes method declares the
	// model's default scopes (discovery validates its receiver and signature).
	// The method value is evaluated lazily at first use, after this query
	// declaration is memoized, so scopes may reference the model's relations.
	if _, declared := e.pkg.methods[m.name]["DefineGlobalScopes"]; declared {
		behavior += fmt.Sprintf(".WithGlobalScopeSource((%s{}).DefineGlobalScopes)", m.name)
	}
	e.line(")%s.WithObserverHooks((%sHooks{}).foundryWriteHooks,%t).WithRetrievalHooks((%sRetrievalHooks{}).foundryRetrievalHooks,%t))}", behavior, m.name, m.hooks != "", m.name, m.retrieval != "")
	e.line("// %s starts a complete-model query for %s with generated hydration.", m.query, m.table)
	e.line("func %s()%sQuery{return %sQuery{foundry%sModelQuery.Get(foundry%sBuildQuery)}}", m.query, m.name, m.name, m.name, m.name)
	e.line("// FoundryQuery returns this model's generated metadata query for framework integrations.")
	e.line("func(%s)FoundryQuery()%s.Query[%s]{return %s().Query}", m.name, query, m.name, m.query)
	e.emitModelReference(m, primary)
	for _, method := range modelQueryMethods(query, context, m.name) {
		e.line("// %s derives a new %s query without changing its source.", method.name, m.name)
		e.line("func(q %sQuery)%s(%s)%sQuery{q.Query=q.Query.%s(%s);return q}", m.name, method.name, method.argument, m.name, method.name, method.forward)
	}
	e.line("// Find applies a typed primary key to this query, preserving filters and pagination.")
	e.line("func(q %sQuery)Find(ctx %s.Context, executor %s.Executor, key %s)(%s.Optional[%s],error){return q.Where(%sFields().%s.Eq(key)).First(ctx,executor)}", m.name, context, database, e.typeName(primary.base), value, m.name, m.name, primary.name)
	e.line("// RequireFind reports database.NotFound when the typed key does not match this query.")
	e.line("func(q %sQuery)RequireFind(ctx %s.Context, executor %s.Executor, key %s)(%s,error){return q.Where(%sFields().%s.Eq(key)).RequireFirst(ctx,executor)}", m.name, context, database, e.typeName(primary.base), m.name, m.name, primary.name)
	e.emitModelLocking(m, primary)
	e.emitModelMutation(m, primary)
	e.emitModelRelations(m)
	e.emitModelAggregates(m)
	e.emitModelExtensions(m, primary)
}

type queryMethod struct{ name, argument, forward string }

func modelQueryMethods(query, context, name string) []queryMethod {
	return []queryMethod{
		{"WithTrashed", "", ""},
		{"OnlyTrashed", "", ""},
		{"WithoutTrashed", "", ""},
		{"WithoutGlobalScope", "scopes ..." + query + ".GlobalScope[" + name + "]", "scopes..."},
		{"WithoutGlobalScopes", "", ""},
		{"WithScopeContext", "ctx " + context + ".Context", "ctx"},
		{"Where", "predicates ..." + query + ".Predicate[" + name + "]", "predicates..."},
		{"WhereHas", "relation " + query + ".ExistenceRelation[" + name + "]", "relation"},
		{"WhereDoesntHave", "relation " + query + ".ExistenceRelation[" + name + "]", "relation"},
		{"OrderBy", "orders ..." + query + ".Order[" + name + "]", "orders..."},
		{"Limit", "count int", "count"},
		{"Offset", "count int", "count"},
		{"With", "relations ..." + query + ".Relation[" + name + "]", "relations..."},
		{"WithRelationLimits", "limits " + query + ".RelationLimits", "limits"},
	}
}
