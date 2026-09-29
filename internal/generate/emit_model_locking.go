package generate

func (e *emitter) emitModelLocking(m model, primary field) {
	query, database := e.use(framework+"/database/query"), e.use(framework+"/database")
	context, value := e.use("context"), e.use(framework+"/value")
	e.line("// %sLockedQuery preserves typed keys while requiring transaction-scoped reads.", m.name)
	e.line("type %sLockedQuery struct{%s.LockedQuery[%s]}", m.name, query, m.name)
	for _, method := range []string{"ForUpdate", "ForNoKeyUpdate", "ForShare", "ForKeyShare"} {
		e.line("// %s derives a transaction-scoped row-locking query; locks last until transaction end.", method)
		e.line("func(q %sQuery)%s()%sLockedQuery{return %sLockedQuery{q.Query.%s()}}", m.name, method, m.name, m.name, method)
	}
	methods := append(modelQueryMethods(query, context, m.name), queryMethod{"NoWait", "", ""}, queryMethod{"SkipLocked", "", ""}, queryMethod{"Wait", "", ""})
	for _, method := range methods {
		e.line("// %s derives a locked %s query without changing its source.", method.name, m.name)
		e.line("func(q %sLockedQuery)%s(%s)%sLockedQuery{q.LockedQuery=q.LockedQuery.%s(%s);return q}", m.name, method.name, method.argument, m.name, method.name, method.forward)
	}
	e.line("// Find locks a model selected by its typed primary key, preserving filters and pagination.")
	e.line("func(q %sLockedQuery)Find(ctx %s.Context,tx *%s.Tx,key %s)(%s.Optional[%s],error){return q.Where(%sFields().%s.Eq(key)).First(ctx,tx)}", m.name, context, database, e.typeName(primary.base), value, m.name, m.name, primary.name)
	e.line("// RequireFind reports database.NotFound when no model can be selected and locked.")
	e.line("func(q %sLockedQuery)RequireFind(ctx %s.Context,tx *%s.Tx,key %s)(%s,error){return q.Where(%sFields().%s.Eq(key)).RequireFirst(ctx,tx)}", m.name, context, database, e.typeName(primary.base), m.name, m.name, primary.name)
}
