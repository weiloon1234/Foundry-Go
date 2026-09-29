package generate

func (e *emitter) emitModelAggregates(m model) {
	if len(m.aggregates) == 0 {
		return
	}
	query, relation := e.use(framework+"/database/query"), e.use(framework+"/database/relation")
	e.line("// %sAggregateSet checks the source model and result type of every computed slot.", m.name)
	e.line("type %sAggregateSet struct{", m.name)
	for _, a := range m.aggregates {
		e.line("%s %s.AggregateRelation[%s,%s]", a.name, query, m.name, e.typeName(a.typ))
	}
	e.line("}")
	e.line("var foundry%sAggregates %s.Memo[%sAggregateSet]", m.name, query, m.name)
	e.line("func foundry%sBuildAggregates()%sAggregateSet{", m.name, m.name)
	e.line("aggregates:=(%s{}).DefineAggregates()", m.name)
	for _, a := range m.aggregates {
		e.line("aggregates.%s=aggregates.%s.Bind(%q,%s().Query,func(m %s)%s.Value[%s]{return m.%s},func(m %s,computed %s.Value[%s])%s{m.%s=computed;return m})", a.name, a.name, a.name, m.query, m.name, relation, e.typeName(a.typ), a.name, m.name, relation, e.typeName(a.typ), m.name, a.name)
	}
	e.line("return aggregates}")
	e.line("// %sAggregates binds handwritten computations to generated typed result slots.", m.name)
	e.line("// DefineAggregates runs once per process; its immutable descriptors are shared.")
	e.line("func %sAggregates()%sAggregateSet{return foundry%sAggregates.Get(foundry%sBuildAggregates)}", m.name, m.name, m.name, m.name)
}
