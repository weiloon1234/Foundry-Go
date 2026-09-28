package generate

func (e *emitter) emitModelInsertSelect(m model) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	scope, receiver, source, input := e.localName("FoundryScope"), e.localName("p"), e.localName("source"), e.localName("v")
	ctx, writer, limit := e.localName("ctx"), e.localName("writer"), e.localName("limit")
	draft, mutation, err := e.localName("draft"), e.localName("mutation"), e.localName("err")
	builder := m.name + "InsertFromBuilder"
	e.line("// %s selects stored SQL values into %s through a typed source scope.", builder, m.name)
	e.line("// This is a set-based write without per-model observers. Values uses ordinary draft setters.")
	e.line("// Columns with Go mutators have no Select method; use their literal draft inputs instead.")
	if m.timestamps[1] != "" {
		e.line("// The framework supplies the managed update timestamp; it has no Select method.")
	}
	e.line("type %s[%s any]struct{plan %s.InsertSelect[%s,%s];err error}", builder, scope, query, scope, m.name)
	format, state, verb := e.use("fmt"), e.localName("state"), e.localName("verb")
	e.line("// Format omits captured source parameters and literal draft inputs from diagnostics.")
	e.line("func(%s %s[%s])Format(%s %s.State,%s rune){%s.plan.Format(%s,%s)}", receiver, builder, scope, state, format, verb, receiver, state, verb)
	e.line("// Insert%sFrom starts a typed INSERT SELECT. Omitted columns retain database defaults or NULL.", m.name)
	e.line("// UUID primary keys require a typed source value or a declared database default; no per-row Go IDs are generated.")
	e.line("func Insert%sFrom[%s any](%s %s.ProjectionSource[%s])%s[%s]{return %s[%s]{plan:%s.InsertFrom(%s,(%s{}).FoundryQuery())}}", m.name, scope, source, query, scope, builder, scope, builder, scope, query, source, m.name)
	for _, f := range m.fields {
		if f.mutator != "" || (m.timestamps[1] != "" && f.column == m.timestamps[1]) {
			continue
		}
		typ := e.typeName(f.typ)
		e.line("// Select%s supplies the stored SQL value for %s. It cannot also be supplied by Values.", f.name, f.column)
		e.emitFieldBehavior(m.name, m.table, f)
		e.line("func(%s %s[%s])Select%s(%s %s.Expression[%s,%s])%s[%s]{%s.plan=%s.plan.Select(%s.MapInsert(%sFields().%s,%s));return %s}", receiver, builder, scope, f.name, input, query, scope, typ, builder, scope, receiver, receiver, query, m.name, f.name, input, receiver)
	}
	e.line("// Values replaces literal draft inputs for every selected row, normalizing them once inside the transaction.")
	e.line("func(%s %s[%s])Values(%s %sDraft)%s[%s]{%s,%s:=%s.foundryMutation(false);%s.err=%s;%s.plan=%s.plan.Values(%s);return %s}", receiver, builder, scope, draft, m.name, builder, scope, mutation, err, draft, receiver, err, receiver, receiver, mutation, receiver)
	e.line("// Exec inserts the selected window and returns an affected row count without collecting models.")
	e.line("func(%s %s[%s])Exec(%s %s.Context,%s %s.Transactor)(int64,error){if %s.err!=nil{return 0,%s.err};return %s.plan.Exec(%s,%s)}", receiver, builder, scope, ctx, context, writer, database, receiver, receiver, receiver, ctx, writer)
	e.line("// Returning collects at most limit complete models; excess returned rows roll back the entire write.")
	e.line("// The bound limits retained models, not SQL work or field sizes. It never truncates the source; result order is not source order.")
	e.line("func(%s %s[%s])Returning(%s %s.Context,%s %s.Transactor,%s int)([]%s,error){if %s.err!=nil{return nil,%s.err};return %s.plan.Returning(%s,%s,%s)}", receiver, builder, scope, ctx, context, writer, database, limit, m.name, receiver, receiver, receiver, ctx, writer, limit)
}
