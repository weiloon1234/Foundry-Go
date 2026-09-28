package generate

func (e *emitter) emitModelSourceWrites(m model, primary field) {
	e.emitModelSourceWriteBuilder(m, primary, true)
	e.emitModelSourceWriteBuilder(m, primary, false)
}

func (e *emitter) emitModelSourceWriteBuilder(m model, primary field, update bool) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	format, value := e.use("fmt"), e.use(framework+"/value")
	scope, receiver := e.localName("FoundryScope"), e.localName("p")
	source, destination, input := e.localName("source"), e.localName("destination"), e.localName("v")
	ctx, writer, limit := e.localName("ctx"), e.localName("writer"), e.localName("limit")
	state, verb := e.localName("state"), e.localName("verb")
	builder, constructor, runtime, create := m.name+"DeleteUsingBuilder", "Delete"+m.name+"Using", "DeleteSource", "DeleteUsing"
	if update {
		builder, constructor, runtime, create = m.name+"UpdateFromBuilder", "Update"+m.name+"From", "UpdateSource", "UpdateFrom"
	}
	e.line("// %s performs a typed set-based source write without per-model observers.", builder)
	if update {
		e.line("// SQL value mappings require one source match per affected model; ambiguity rolls back.")
		e.line("// Literal draft inputs retain normal setters and timestamps; SQL selectors cannot bypass them.")
	} else {
		e.line("// Ordinary deletion honors soft deletion; repeated source keys affect each destination once.")
	}
	e.line("type %s[%s any]struct{plan %s.%s[%s,%s];err error}", builder, scope, query, runtime, scope, m.name)
	e.line("// Format omits captured source parameters and draft inputs.")
	e.line("func(%s %s[%s])Format(%s %s.State,%s rune){%s.plan.Format(%s,%s)}", receiver, builder, scope, state, format, verb, receiver, state, verb)
	e.line("// %s preserves destination filters and the source's explicitly selected window.", constructor)
	e.line("// Call Match%s to connect the source identity to this model's primary key.", primary.name)
	e.line("func %s[%s any](%s %sQuery,%s %s.ProjectionSource[%s])%s[%s]{return %s[%s]{plan:%s.%s(%s,%s)}}", constructor, scope, destination, m.name, source, query, scope, builder, scope, builder, scope, query, create, destination, source)
	if !update && m.softDelete != "" {
		e.line("// ForceDelete%sUsing physically removes matching models with the destination's explicit visibility.", m.name)
		e.line("// Use WithTrashed on the destination to include already deleted models.")
		e.line("func ForceDelete%sUsing[%s any](%s %sQuery,%s %s.ProjectionSource[%s])%s[%s]{return %s[%s]{plan:%s.ForceDeleteUsing(%s,%s)}}", m.name, scope, destination, m.name, source, query, scope, builder, scope, builder, scope, query, destination, source)
	}
	typ := e.typeName(primary.typ)
	e.line("// Match%s replaces the stored source value matched to %s; it never assigns the key.", primary.name, primary.column)
	e.emitFieldBehavior(m.name, m.table, primary)
	e.line("func(%s %s[%s])Match%s(%s %s.Expression[%s,%s])%s[%s]{%s.plan=%s.plan.Match(%s.MatchSource(%sFields().%s,%s));return %s}", receiver, builder, scope, primary.name, input, query, scope, typ, builder, scope, receiver, receiver, query, m.name, primary.name, input, receiver)
	e.line("// MatchNullable%s accepts a nullable source key, such as an outer join. NULL matches nothing.", primary.name)
	e.emitFieldBehavior(m.name, m.table, primary)
	e.line("func(%s %s[%s])MatchNullable%s(%s %s.Expression[%s,%s.Nullable[%s]])%s[%s]{%s.plan=%s.plan.Match(%s.MatchNullableSource(%sFields().%s,%s));return %s}", receiver, builder, scope, primary.name, input, query, scope, value, typ, builder, scope, receiver, receiver, query, m.name, primary.name, input, receiver)
	if update {
		for _, f := range m.fields {
			if f.column == primary.column || f.mutator != "" || (m.timestamps[1] != "" && f.column == m.timestamps[1]) {
				continue
			}
			e.line("// Select%s assigns a stored source value to %s; Values cannot also supply it.", f.name, f.column)
			e.emitFieldBehavior(m.name, m.table, f)
			e.line("func(%s %s[%s])Select%s(%s %s.Expression[%s,%s])%s[%s]{%s.plan=%s.plan.Select(%s.MapUpdate(%sFields().%s,%s));return %s}", receiver, builder, scope, f.name, input, query, scope, e.typeName(f.typ), builder, scope, receiver, receiver, query, m.name, f.name, input, receiver)
		}
		draft, mutation, err := e.localName("draft"), e.localName("mutation"), e.localName("err")
		e.line("// Values replaces literal inputs for every affected model, normalized once inside the transaction.")
		e.line("// Primary keys cannot be changed. Fixed-value updates allow repeated source matches.")
		e.line("func(%s %s[%s])Values(%s %sDraft)%s[%s]{%s,%s:=%s.foundryMutation(false);%s.err=%s;%s.plan=%s.plan.Values(%s);return %s}", receiver, builder, scope, draft, m.name, builder, scope, mutation, err, draft, receiver, err, receiver, receiver, mutation, receiver)
	}
	e.line("// Exec returns the number of affected destination models without collecting them.")
	e.line("func(%s %s[%s])Exec(%s %s.Context,%s %s.Transactor)(int64,error){if %s.err!=nil{return 0,%s.err};return %s.plan.Exec(%s,%s)}", receiver, builder, scope, ctx, context, writer, database, receiver, receiver, receiver, ctx, writer)
	e.line("// Returning collects at most limit complete affected models; excess returned rows roll back the write.")
	e.line("// The bound limits retained models, not SQL work or field sizes; it never truncates the source or promises result order.")
	e.line("func(%s %s[%s])Returning(%s %s.Context,%s %s.Transactor,%s int)([]%s,error){if %s.err!=nil{return nil,%s.err};return %s.plan.Returning(%s,%s,%s)}", receiver, builder, scope, ctx, context, writer, database, limit, m.name, receiver, receiver, receiver, ctx, writer, limit)
}
