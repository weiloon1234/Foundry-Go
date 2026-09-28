package generate

func (e *emitter) emitModelReference(m model, primary field) {
	modelPackage := e.use(framework + "/model")
	receiver := e.localName("m")
	e.line("// FoundryReference retains %s and its concrete stored primary key for framework integrations.", m.name)
	e.line("// It reads %s directly, never calls an accessor, and performs no database lookup.", primary.name)
	e.emitFieldBehavior(m.name, m.table, primary)
	e.line("func(%s %s)FoundryReference()%s.Reference[%s,%s]{return %s.NewReference[%s](%q,%s.%s,%s)}", receiver, m.name, modelPackage, m.name, e.typeName(primary.typ), modelPackage, m.name, m.table, receiver, primary.name, e.fieldCodec(primary, true))
	e.line("// FoundryIdentity snapshots only the stored model name and primary key for attribution and audit.")
	e.line("// This explicit serialization boundary does not establish authentication or authorization.")
	e.line("func(%s %s)FoundryIdentity()(%s.Identity,error){return %s.FoundryReference().Identity()}", receiver, m.name, modelPackage, receiver)
}
