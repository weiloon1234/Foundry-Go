package generate

// emitModelSetWrites adds Laravel-style mass writes backed by one statement.
// They never run per-model hooks; the runtime rejects them on hooked models
// unless the query acknowledges WithoutModelHooks.
func (e *emitter) emitModelSetWrites(m model) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	fault := e.use(framework + "/fault")
	e.line("// WithoutModelHooks acknowledges that set-based writes on this query skip per-model hooks and observers; per-model writes on it are rejected.")
	e.line("func(q %sQuery)WithoutModelHooks()%sQuery{q.Query=q.Query.WithoutModelHooks();return q}", m.name, m.name)
	e.line("// UpdateAll assigns the draft to every model matching this query (filters, global scopes and soft-delete visibility) in one UPDATE and returns the affected count. Managed update timestamps and field mutators apply once; per-model hooks, observers and change capture do not run.")
	e.line("func(q %sQuery)UpdateAll(ctx %s.Context,writer %s.Transactor,draft %sDraft)(int64,error){mutation,err:=draft.foundryMutation(false);if err!=nil{return 0,err};return q.Query.PatchAll(ctx,writer,mutation)}", m.name, context, database, m.name)
	if m.softDelete != "" {
		e.line("// DeleteAll soft-deletes every active model matching this query in one statement and returns the affected count. Per-model hooks do not run.")
	} else {
		e.line("// DeleteAll physically deletes every model matching this query in one statement and returns the affected count. Per-model hooks do not run.")
	}
	e.line("func(q %sQuery)DeleteAll(ctx %s.Context,writer %s.Transactor)(int64,error){return q.Query.RemoveAll(ctx,writer)}", m.name, context, database)
	if m.softDelete != "" {
		e.line("// ForceDeleteAll physically deletes every model matching this query's current visibility in one statement. WithTrashed includes deleted models.")
		e.line("func(q %sQuery)ForceDeleteAll(ctx %s.Context,writer %s.Transactor)(int64,error){return q.Query.ForceRemoveAll(ctx,writer)}", m.name, context, database)
	}
	for _, method := range []struct {
		name     string
		subtract bool
	}{{"Increment", false}, {"Decrement", true}} {
		e.line("// %s adjusts one numeric field of every matching model by a typed delta, such as %sFields().Field.By(1), with an optional extra draft, in one UPDATE. SQL NULL stays NULL. Per-model hooks do not run.", method.name, m.name)
		e.line("func(q %sQuery)%s(ctx %s.Context,writer %s.Transactor,adjustment %s.Adjustment[%s],extra ...%sDraft)(int64,error){", m.name, method.name, context, database, query, m.name, m.name)
		e.line("if len(extra)>1{return 0,%s.New(%s.Invalid,\"numeric adjustment accepts at most one extra draft\")};var mutation %s.Mutation[%s];if len(extra)==1{var err error;if mutation,err=extra[0].foundryMutation(false);err!=nil{return 0,err}}", fault, fault, query, m.name)
		e.line("return q.Query.AdjustAll(ctx,writer,adjustment,%t,mutation)}", method.subtract)
	}
	e.line("// FoundryUpdateMutation supplies the typed update-draft adapter for framework integrations such as pivot updates. Omitted fields stay unchanged; no mutators or database calls run here.")
	e.line("func(d %sDraft)FoundryUpdateMutation()(%s.Mutation[%s],error){return d.foundryMutation(false)}", m.name, query, m.name)
}
