package generate

import "go/types"

func (e *emitter) emitModelMutation(m model, primary field) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	value, fault := e.use(framework+"/value"), e.use(framework+"/fault")
	e.line("// Create inserts a complete model from explicit draft fields. Omitted UUID primary keys are generated unless the database owns their default or a distinct mutation input is required.")
	e.line("func(q %sQuery)Create(ctx %s.Context,writer %s.Transactor,draft %sDraft)(%s,error){mutation,err:=draft.foundryMutation(true);if err!=nil{return %s{},err};return q.Query.Insert(ctx,writer,mutation)}", m.name, context, database, m.name, m.name, m.name)
	e.line("// Upsert inserts or applies an explicit typed conflict policy. Skipped conflicts return an omitted result; database constraints still apply.")
	e.line("func(q %sQuery)Upsert(ctx %s.Context,writer %s.Transactor,draft %sDraft,conflict %s.Conflict[%s])(%s.Optional[%s],error){mutation,err:=draft.foundryMutation(true);if err!=nil{return %s.Optional[%s]{},err};return q.Query.InsertOnConflict(ctx,writer,mutation,conflict)}", m.name, context, database, m.name, query, m.name, value, m.name, value, m.name)
	e.line("// CreateMany inserts a bounded batch atomically. A valid empty batch performs no database work. This is a set-based write.")
	e.line("func(q %sQuery)CreateMany(ctx %s.Context,writer %s.Transactor,drafts []%sDraft)([]%s,error){return q.foundryInsertMany(ctx,writer,drafts,nil)}", m.name, context, database, m.name, m.name)
	e.line("// UpsertMany applies one typed conflict policy to a bounded atomic batch. Skipped conflicts have no returned model; result order is not an input-row mapping.")
	e.line("func(q %sQuery)UpsertMany(ctx %s.Context,writer %s.Transactor,drafts []%sDraft,conflict %s.Conflict[%s])([]%s,error){return q.foundryInsertMany(ctx,writer,drafts,&conflict)}", m.name, context, database, m.name, query, m.name, m.name)
	e.line("func(q %sQuery)foundryInsertMany(ctx %s.Context,writer %s.Transactor,drafts []%sDraft,conflict *%s.Conflict[%s])([]%s,error){", m.name, context, database, m.name, query, m.name, m.name)
	e.line("mutations,err:=q.foundryCreateMutations(drafts);if err!=nil{return nil,err};if conflict==nil{return q.Query.InsertMany(ctx,writer,mutations)};return q.Query.InsertManyOnConflict(ctx,writer,mutations,*conflict)}")
	e.line("func(q %sQuery)foundryCreateMutations(drafts []%sDraft)([]%s.Mutation[%s],error){", m.name, m.name, query, m.name)
	e.line("if len(drafts)>%s.MaxInsertRows{return nil,%s.New(%s.Invalid,\"model insert exceeds its row bound\")};mutations:=make([]%s.Mutation[%s],len(drafts));for i,draft:=range drafts{m,err:=draft.foundryMutation(true);if err!=nil{return nil,err};mutations[i]=m};return mutations,nil}", query, fault, fault, query, m.name)
	e.line("// Update patches one typed primary key within this query's filters. Omitted fields stay unchanged; primary-key mutation is rejected.")
	e.line("func(q %sQuery)Update(ctx %s.Context,writer %s.Transactor,key %s,draft %sDraft)(%s,error){mutation,err:=draft.foundryMutation(false);if err!=nil{return %s{},err};return q.Where(%sFields().%s.Eq(key)).Query.Patch(ctx,writer,mutation)}", m.name, context, database, e.typeName(primary.base), m.name, m.name, m.name, m.name, primary.name)
	if m.softDelete != "" {
		e.line("// Delete soft-deletes one active typed primary key within this query's filters and returns its stored result.")
	} else {
		e.line("// Delete removes one typed primary key within this query's filters and returns its complete pre-deletion model.")
	}
	e.line("func(q %sQuery)Delete(ctx %s.Context,writer %s.Transactor,key %s)(%s,error){return q.Where(%sFields().%s.Eq(key)).Query.Remove(ctx,writer)}", m.name, context, database, e.typeName(primary.base), m.name, m.name, primary.name)
	if m.softDelete != "" {
		e.line("// Restore restores one deleted typed primary key, retaining explicit filters and returning the stored result.")
		e.line("func(q %sQuery)Restore(ctx %s.Context,writer %s.Transactor,key %s)(%s,error){return q.Where(%sFields().%s.Eq(key)).Query.Restore(ctx,writer)}", m.name, context, database, e.typeName(primary.base), m.name, m.name, primary.name)
		e.line("// ForceDelete physically removes one typed primary key within the current visibility and filters. WithTrashed includes deleted models.")
		e.line("func(q %sQuery)ForceDelete(ctx %s.Context,writer %s.Transactor,key %s)(%s,error){return q.Where(%sFields().%s.Eq(key)).Query.ForceRemove(ctx,writer)}", m.name, context, database, e.typeName(primary.base), m.name, m.name, primary.name)
	}

	e.emitModelEach(m)
	e.emitModelLookup(m)
	e.emitModelInsertSelect(m)
	e.emitModelSourceWrites(m, primary)

	draftName, defaultsName := e.localName("d"), e.localName("defaults")
	valuesName, errName, inputName := e.localName("values"), e.localName("err"), e.localName("v")
	e.line("// FoundryCreateMutation supplies the typed draft adapter for framework integrations. Defaults fill omitted fields before UUID preparation; no mutators or database calls run here.")
	e.line("func(%s %sDraft)FoundryCreateMutation(%s %s.Mutation[%s])(%s.Mutation[%s],error){%s,%s:=%s.ReadCreateDefaults((%s{}).FoundryQuery(),%s);if %s!=nil{return %s.Mutation[%s]{},%s}", draftName, m.name, defaultsName, query, m.name, query, m.name, valuesName, errName, query, m.name, defaultsName, errName, query, m.name, errName)
	for _, f := range m.fields {
		e.line("if !%s.field%s.IsSet(){%s,%s:=%s.MutationValue[%s,%s](%s,%q,%q);if %s!=nil{return %s.Mutation[%s]{},%s};%s.field%s=%s}", draftName, f.name, inputName, errName, query, m.name, e.fieldInputType(f, true), valuesName, m.table, f.column, errName, query, m.name, errName, draftName, f.name, inputName)
	}
	e.line("return %s.foundryMutation(true)}", draftName)
	e.line("func(d %sDraft)foundryMutation(create bool)(%s.Mutation[%s],error){", m.name, query, m.name)
	if id, ok := types.Unalias(primary.base).(*types.Named); ok && isNamed(id, framework+"/model", "ID") && !primary.databaseDefault && primary.input == nil {
		modelPackage := e.use(framework + "/model")
		e.line("if create&&!d.field%s.IsSet(){id,err:=%s.NewID[%s]();if err!=nil{return %s.Mutation[%s]{},err};d=d.Set%s(id)}", primary.name, modelPackage, m.name, query, m.name, primary.name)
	}
	e.line("var assignments []%s.Assignment[%s]", query, m.name)
	for _, f := range m.fields {
		if f.input != nil {
			if c := e.binaryInputCodec(f, true); c != "" {
				e.line("if v,set:=d.field%s.Get();set{assignments=append(assignments,%s.AssignClonedInput[%s](%q,%q,%s,v))}", f.name, query, m.name, m.table, f.column, c)
			} else {
				e.line("if v,set:=d.field%s.Get();set{assignments=append(assignments,%s.AssignInput[%s](%q,%q,v))}", f.name, query, m.name, m.table, f.column)
			}
		} else {
			e.line("if v,set:=d.field%s.Get();set{assignments=append(assignments,%s.Assign[%s](%q,%q,%s,v))}", f.name, query, m.name, m.table, f.column, e.fieldCodec(f, true))
		}
	}
	e.line("return %s.Change(assignments...),nil}", query)
}
