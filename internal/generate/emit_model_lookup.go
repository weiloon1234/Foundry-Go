package generate

func (e *emitter) emitModelLookup(m model) {
	database, context := e.use(framework+"/database"), e.use("context")
	e.line("// FirstOrCreate returns the first primary-ordered matching model or creates one through its normal lifecycle. The draft is prepared only when no model matches; omitted fields default from the query's top-level equality filters and global scopes, and the stored creation must satisfy the query. Internal lookup skips Retrieved. Concurrent absence relies on database unique constraints and reports ordinary conflicts without hidden retries.")
	e.line("func(q %sQuery)FirstOrCreate(ctx %s.Context,writer %s.Transactor,create %sDraft)(%s,error){return q.Query.FirstOrInsert(ctx,writer,create)}", m.name, context, database, m.name, m.name)
	e.line("// CreateOrFirst creates a model through its normal lifecycle and, when a unique constraint rejects it, returns the first primary-ordered model matching the query instead. The insert runs in its own savepoint, so a concurrent creation of the same unique key is resolved under READ COMMITTED rather than reported. The query must select the row holding the conflicting key; a created model must satisfy it.")
	e.line("func(q %sQuery)CreateOrFirst(ctx %s.Context,writer %s.Transactor,create %sDraft)(%s,error){return q.Query.InsertOrFirst(ctx,writer,create)}", m.name, context, database, m.name, m.name)
	e.line("// UpdateOrCreate runs a concrete draft callback for the first primary-ordered matching model or creates from the separate creation draft. Only the chosen branch prepares its draft; ordinary lifecycle and transaction ownership remain in force. Read ordering, pagination and eager-loading options are rejected.")
	e.line("func(q %sQuery)UpdateOrCreate(ctx %s.Context,writer %s.Transactor,create %sDraft,draft func(%s.Context,*%s.Tx,%s)(%sDraft,error))(%s,error){", m.name, context, database, m.name, context, database, m.name, m.name, m.name)
	e.emitModelUpdateCallback(m)
	e.line("return q.Query.PatchOrInsert(ctx,writer,create,callback)}")
}

// Concrete generated update callbacks share one conversion to the runtime
// mutation contract, preserving the generated draft's existing preparation.
func (e *emitter) emitModelUpdateCallback(m model) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	e.line("var callback func(%s.Context,*%s.Tx,%s)(%s.Mutation[%s],error);if draft!=nil{callback=func(ctx %s.Context,tx *%s.Tx,current %s)(%s.Mutation[%s],error){d,err:=draft(ctx,tx,current);if err!=nil{return %s.Mutation[%s]{},err};return d.foundryMutation(false)}}", context, database, m.name, query, m.name, context, database, m.name, query, m.name, query, m.name)
}
