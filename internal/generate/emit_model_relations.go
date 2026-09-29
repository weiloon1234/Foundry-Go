package generate

func (e *emitter) emitModelRelations(m model) {
	if len(m.relations) == 0 {
		return
	}
	query, relation := e.use(framework+"/database/query"), e.use(framework+"/database/relation")
	e.line("// %sRelationSet requires typed descriptors matching every declared relation slot.", m.name)
	e.line("type %sRelationSet struct{", m.name)
	for _, r := range m.relations {
		args := e.typeName(r.target)
		if r.pivot != nil {
			args += "," + e.typeName(r.pivot)
		}
		e.line("%s %s.%sRelation[%s,%s]", r.name, query, r.kind, m.name, args)
	}
	e.line("}")
	e.line("var foundry%sRelations %s.Memo[%sRelationSet]", m.name, query, m.name)
	e.line("func foundry%sBuildRelations()%sRelationSet{", m.name, m.name)
	e.line("relations:=(%s{}).DefineRelations()", m.name)
	for _, r := range m.relations {
		target := e.typeName(r.target)
		args, pivotQuery := target, ""
		if r.pivot != nil {
			pivot := e.typeName(r.pivot)
			args += "," + pivot
			pivotQuery = "(" + pivot + "{}).FoundryQuery(),"
		}
		e.line("relations.%s=relations.%s.Bind(%q,%s().Query,(%s{}).FoundryQuery(),%sfunc(m %s)%s.%s[%s]{return m.%s},func(m %s,loaded %s.%s[%s])%s{m.%s=loaded;return m})", r.name, r.name, r.name, m.query, target, pivotQuery, m.name, relation, r.kind, args, r.name, m.name, relation, r.kind, args, m.name, r.name)
	}
	e.line("return relations}")
	e.line("// %sRelations binds handwritten key declarations to generated model metadata and typed slots.", m.name)
	e.line("// DefineRelations runs once per process; its immutable descriptors are shared.")
	e.line("func %sRelations()%sRelationSet{return foundry%sRelations.Get(foundry%sBuildRelations)}", m.name, m.name, m.name, m.name)
}
