package generate

import "strings"

// emitModelChanges reuses the discovered persisted fields and their codec path.
// Separate field data keeps model fields named Before/After/Changed from
// colliding with the snapshot API. Only persisted values enter the snapshots.
func (e *emitter) emitModelChanges(m model) {
	lifecycle := e.use(framework + "/database/lifecycle")
	value, fault, fmtPackage := e.use(framework+"/value"), e.use(framework+"/fault"), e.use("fmt")
	before, after, assigned := e.localName("before"), e.localName("after"), e.localName("assigned")
	result, errName, item := e.localName("changes"), e.localName("err"), e.localName("item")
	snapshot, present := e.localName("snapshot"), e.localName("present")
	e.line("// %sFieldChanges contains typed changes for persisted fields only.", m.name)
	e.line("type %sFieldChanges struct {", m.name)
	for _, f := range m.fields {
		e.line("// %s retains the typed change for %s.", f.name, f.column)
		e.line("%s %s.FieldChange[%s]", f.name, lifecycle, e.typeName(f.typ))
	}
	e.line("}")
	e.line("// %sChanges contains immutable persisted snapshots and field changes. Relation loads and ignored fields are excluded.", m.name)
	e.line("type %sChanges struct {before,after %s.Optional[%s];fields %sFieldChanges;operation %s.Optional[%s.Operation]}", m.name, value, m.name, m.name, value, lifecycle)
	e.line("// Operation identifies an automatically captured lifecycle write. Standalone Compare results have no operation.")
	e.line("func(c %sChanges)Operation()%s.Optional[%s.Operation]{return c.operation}", m.name, value, lifecycle)
	e.line("// Before returns the persisted model before the operation, or absence for creation.")
	beforeValue, afterValue := "c.before", "c.after"
	if hasBinaryFields(m) {
		beforeValue = "foundry" + m.name + "Snapshot(c.before)"
		afterValue = "foundry" + m.name + "Snapshot(c.after)"
	}
	e.line("func(c %sChanges)Before()%s.Optional[%s]{return %s}", m.name, value, m.name, beforeValue)
	e.line("// After returns the persisted model after the operation, or absence for physical deletion. Soft deletion retains its stored result.")
	e.line("func(c %sChanges)After()%s.Optional[%s]{return %s}", m.name, value, m.name, afterValue)
	e.line("// Fields returns model-specific field changes by value.")
	e.line("func(c %sChanges)Fields()%sFieldChanges{return c.fields}", m.name, m.name)
	for _, method := range []string{"Assigned", "Changed"} {
		var checks []string
		for _, f := range m.fields {
			checks = append(checks, "c.fields."+f.name+"."+method+"()")
		}
		e.line("// %s reports whether any persisted field is %s.", method, strings.ToLower(method))
		e.line("func(c %sChanges)%s()bool{return %s}", m.name, method, strings.Join(checks, "||"))
	}
	e.line("// String and GoString keep model snapshots out of routine diagnostics.")
	e.line("func(%sChanges)String()string{return %q}", m.name, m.name+" changes")
	e.line("func(c %sChanges)GoString()string{return c.String()}", m.name)
	e.line("// Compare%s is the generated lifecycle comparison boundary. Pass stored snapshots and the effective assignment draft; its values are not transformed or compared to the result. Both snapshots, when present, must have the same primary key. This function performs no database work or automatic capture.", m.name)
	e.line("func Compare%s(%s,%s %s.Optional[%s],%s %sDraft)(%sChanges,error){", m.name, before, after, value, m.name, assigned, m.name, m.name)
	e.line("%s=foundry%sSnapshot(%s);%s=foundry%sSnapshot(%s)", before, m.name, before, after, m.name, after)
	e.line("var %s %sChanges;var %s error", result, m.name, errName)
	for _, f := range m.fields {
		e.line("%s.fields.%s,%s=%s.CompareModelField(%s,%s,%s,%s.field%s.IsSet(),func(%s %s)%s{return %s.%s})", result, f.name, errName, lifecycle, e.fieldCodec(f, true), before, after, assigned, f.name, item, m.name, e.typeName(f.typ), item, f.name)
		e.line("if %s!=nil{return %sChanges{},%s.Errorf(%q,%s)}", errName, m.name, fmtPackage, "compare "+m.name+"."+f.name+": %w", errName)
		if f.primary {
			e.line("if %s.IsSet()&&%s.IsSet()&&(%s.fields.%s.Changed()||%s.fields.%s.Assigned()){return %sChanges{},%s.New(%s.Invalid,\"model change cannot replace or assign its existing primary key\")}", before, after, result, f.name, result, f.name, m.name, fault, fault)
		}
	}
	e.line("%s.before=%s;%s.after=%s;return %s,nil}", result, before, result, after, result)
	e.line("func foundry%sSnapshot(%s %s.Optional[%s])%s.Optional[%s]{", m.name, snapshot, value, m.name, value, m.name)
	e.line("%s,%s:=%s.Get();if !%s{return %s.Optional[%s]{}};return %s.Set(%s{", item, present, snapshot, present, value, m.name, value, m.name)
	for _, f := range m.fields {
		e.line("%s:%s,", f.name, e.cloneStoredField(f, item+"."+f.name))
	}
	e.line("})}")
}
