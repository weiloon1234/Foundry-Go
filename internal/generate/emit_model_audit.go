package generate

func (e *emitter) emitModelAudit(m model) {
	record := e.use(framework + "/audit/record")
	value, fault := e.use(framework+"/value"), e.use(framework+"/fault")
	context, database, foundation := e.use("context"), e.use(framework+"/database"), e.use(framework+"/foundation")
	var primary field
	for _, f := range m.fields {
		if f.primary {
			primary = f
			break
		}
	}
	key := e.typeName(primary.typ)
	modelType := record + ".Model[" + m.name + "," + key + "]"
	changes, policy, operation := e.localName("changes"), e.localName("policy"), e.localName("operation")
	item, present, builder := e.localName("item"), e.localName("present"), e.localName("builder")
	captured, errName, view := e.localName("captured"), e.localName("err"), e.localName("view")
	fields, history := e.localName("fields"), e.localName("history")
	registrar, pool, resolve, resolver := e.localName("registrar"), e.localName("pool"), e.localName("resolve"), e.localName("resolver")
	name, writer, write := e.localName("name"), e.localName("writer"), e.localName("write")
	ctx, tx := e.localName("ctx"), e.localName("tx")

	e.line("// %sAuditPolicy selects stored fields for auditing. Zero applies automatic sensitive-name redaction.", m.name)
	e.line("// Exclusion/redaction cannot hide the primary key while retaining it as the subject identity; that policy fails capture.")
	e.line("type %sAuditPolicy struct{_ [0]*%s", m.name, m.name)
	for _, f := range m.fields {
		e.line("// %s controls the audit representation of stored %s.%s.", f.name, m.table, f.column)
		e.line("%s %s.Disclosure", f.name, record)
	}
	e.line("}")
	e.line("// Audit captures this operation's existing stored changes without getters, mutators or additional database work.")
	e.line("// Standalone Compare results have no captured operation and cannot become lifecycle audit records.")
	e.line("func(%s %sChanges)Audit(%s %sAuditPolicy)(%s,error){", changes, m.name, policy, m.name, modelType)
	e.line("%s,%s:=%s.operation.Get();if !%s{return %s{},%s.New(%s.Missing,\"audit requires captured model changes\")}", operation, present, changes, present, modelType, fault, fault)
	e.line("%s,%s:=%s.after.Get();if !%s{%s,%s=%s.before.Get()};if !%s{return %s{},%s.New(%s.Missing,\"audit requires a stored model snapshot\")}", item, present, changes, present, item, present, changes, present, modelType, fault, fault)
	e.line("%s,%s:=%s.NewBuilder(%s.FoundryReference(),%s,%q,%s.%s);if %s!=nil{return %s{},%s}", builder, errName, record, item, operation, primary.column, policy, primary.name, errName, modelType, errName)
	e.line("var %s %s.Field[%s]", captured, record, m.name)
	for _, f := range m.fields {
		e.line("%s,%s=%s.CaptureField[%s](%q,%s,%s.fields.%s,%s.%s);if %s!=nil{return %s{},%s}", captured, errName, record, m.name, f.column, e.fieldCodec(f, true), changes, f.name, policy, f.name, errName, modelType, errName)
		e.line("if %s=%s.Add(%s);%s!=nil{return %s{},%s}", errName, builder, captured, errName, modelType, errName)
	}
	e.line("return %s.Build()}", builder)

	e.line("// %sAuditFieldSet contains typed historical fields. An omitted field differs from an absent model or SQL NULL.", m.name)
	e.line("type %sAuditFieldSet struct{_ [0]*%s", m.name, m.name)
	for _, f := range m.fields {
		e.line("// %s retains stored %s.%s after write mutators; read accessors do not replace audit values.", f.name, m.table, f.column)
		e.emitFieldBehavior(m.name, m.table, f)
		e.line("%s %s.Optional[%s.FieldChange[%s]]", f.name, value, record, e.typeName(f.typ))
	}
	e.line("}")
	e.line("// %sAuditFields reads model-owned history through its declared codecs, without populating a partial model.", m.name)
	e.line("// Get on a redacted field reports an explicit error; its Snapshot remains available for deliberate audit export.")
	e.line("func %sAuditFields(%s %s)(%sAuditFieldSet,error){", m.name, history, modelType, m.name)
	e.line("%s,%s:=%s.Inspect(%s);if %s!=nil{return %sAuditFieldSet{},%s};var %s %sAuditFieldSet", view, errName, record, history, errName, m.name, errName, fields, m.name)
	for _, f := range m.fields {
		e.line("%s.%s,%s=%s.ReadField(%s,%q,%s);if %s!=nil{return %sAuditFieldSet{},%s}", fields, f.name, errName, record, view, f.column, e.fieldCodec(f, true), errName, m.name, errName)
	}
	e.line("return %s,nil}", fields)

	e.line("// %sAuditing declares automatic auditing for normal lifecycle writes. Register it once through audit.Register.", m.name)
	e.line("// Existing observer registration owns dependency construction, duplicate detection and transaction lifetime.")
	e.line("func %sAuditing(%s %sAuditPolicy)%s.Declaration{return %s.Declare(func(%s *%s.Registrar,%s %s.Key[*%s.DB],%s %s.ResolveWriter)error{", m.name, policy, m.name, record, record, registrar, foundation, pool, foundation, database, resolve, record)
	e.line("%s,%s:=%s.ObserverName[%s]();if %s!=nil{return %s}", name, errName, record, m.name, errName, errName)
	e.line("return Register%sObserver(%s,%s,New%sObserver(%s),func(%s %s.Resolver)(func()%sHooks,error){", m.name, registrar, pool, m.name, name, resolver, foundation, m.name)
	e.line("%s,%s:=%s(%s);if %s!=nil{return nil,%s};if %s==nil{return nil,%s.New(%s.Invalid,\"audit writer is not initialized\")}", writer, errName, resolve, resolver, errName, errName, writer, fault, fault)
	e.line("return func()%sHooks{", m.name)
	e.line("%s:=func(%s %s.Context,%s *%s.Tx,%s %sChanges)error{", write, ctx, context, tx, database, changes, m.name)
	e.line("%s,%s:=%s.Audit(%s);if %s!=nil{return %s};return %s.RecordModel(%s,%s,%s.Entry())}", captured, errName, changes, policy, errName, errName, writer, ctx, tx, captured)
	e.line("return %sHooks{Created:%s,Updated:%s,Deleted:%s,Restored:%s}},nil})})}", m.name, write, write, write, write)
}
