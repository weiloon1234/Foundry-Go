package generate

func (e *emitter) emitModelEach(m model) {
	database, context := e.use(framework+"/database"), e.use("context")
	e.line("// CreateEach creates each input through normal model hooks and mutators inside one bounded atomic operation. Results preserve input order; CreateMany has separate set-based semantics.")
	e.line("func(q %sQuery)CreateEach(ctx %s.Context,writer %s.Transactor,drafts []%sDraft)([]%s,error){mutations,err:=q.foundryCreateMutations(drafts);if err!=nil{return nil,err};return q.Query.InsertEach(ctx,writer,mutations)}", m.name, context, database, m.name, m.name)
	e.line("// UpdateEach derives a concrete draft for each locked model and runs its normal lifecycle. The complete matching set must fit limit before any callback. Results follow primary-key order and all selected writes are atomic; read ordering, pagination and eager-loading options are rejected.")
	e.line("func(q %sQuery)UpdateEach(ctx %s.Context,writer %s.Transactor,limit int,draft func(%s.Context,*%s.Tx,%s)(%sDraft,error))([]%s,error){", m.name, context, database, context, database, m.name, m.name, m.name)
	e.emitModelUpdateCallback(m)
	e.line("return q.Query.PatchEach(ctx,writer,limit,callback)}")
	e.line("// DeleteEach deletes each selected active model through normal lifecycle behavior. Soft-delete models remain stored; other models are physically removed. The positive limit bounds the complete atomic candidate set before hooks run.")
	e.line("func(q %sQuery)DeleteEach(ctx %s.Context,writer %s.Transactor,limit int)([]%s,error){return q.Query.RemoveEach(ctx,writer,limit)}", m.name, context, database, m.name)
	if m.softDelete != "" {
		e.line("// RestoreEach restores each deleted model matching the explicit predicates through its normal lifecycle. The positive limit bounds the complete atomic candidate set.")
		e.line("func(q %sQuery)RestoreEach(ctx %s.Context,writer %s.Transactor,limit int)([]%s,error){return q.Query.RestoreEach(ctx,writer,limit)}", m.name, context, database, m.name)
		e.line("// ForceDeleteEach physically removes each matching soft-delete model through its normal force-delete lifecycle. Visibility is retained; the positive limit bounds the complete atomic candidate set.")
		e.line("func(q %sQuery)ForceDeleteEach(ctx %s.Context,writer %s.Transactor,limit int)([]%s,error){return q.Query.ForceRemoveEach(ctx,writer,limit)}", m.name, context, database, m.name)
	}
}
