package generate

func (e *emitter) emitModelHooks(m model) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	lifecycle, value, fault := e.use(framework+"/database/lifecycle"), e.use(framework+"/value"), e.use(framework+"/fault")
	hooks, ctx, tx := e.localName("hooks"), e.localName("ctx"), e.localName("tx")
	operation, before, after := e.localName("operation"), e.localName("before"), e.localName("after")
	mutation, values, draft := e.localName("mutation"), e.localName("values"), e.localName("draft")
	errName, current, present, changes := e.localName("err"), e.localName("current"), e.localName("present"), e.localName("changes")
	zero := e.localName("zero")
	all, set, factories, factory := e.localName("allHooks"), e.localName("observerSet"), e.localName("factories"), e.localName("factory")
	foundation := e.use(framework + "/foundation")
	registrar, pool, observer, construct := e.localName("registrar"), e.localName("pool"), e.localName("observer"), e.localName("construct")
	e.line("// %sHooks declares typed normal-write callbacks. Reference a factory with hooks=Factory on the model directive. Callbacks run synchronously; do not retain draft or transaction pointers. Nil callbacks are skipped.", m.name)
	e.line("type %sHooks struct{", m.name)
	e.line("// Saving runs before Creating or Updating and may change assigned fields.")
	e.line("Saving func(%s.Context,*%s.Tx,%s.Optional[%s],*%sDraft)error", context, database, value, m.name, m.name)
	e.line("// Creating runs before insert mutators; omitted required fields may be supplied here.")
	e.line("Creating func(%s.Context,*%s.Tx,*%sDraft)error", context, database, m.name)
	e.line("// Updating receives the locked stored model before the write.")
	e.line("Updating func(%s.Context,*%s.Tx,%s,*%sDraft)error", context, database, m.name, m.name)
	e.line("// Deleting receives the locked stored model before soft or physical deletion.")
	e.line("Deleting func(%s.Context,*%s.Tx,%s)error", context, database, m.name)
	for _, name := range []string{"Restoring", "ForceDeleting"} {
		e.line("// %s receives the locked stored model before its special lifecycle write.", name)
		e.line("%s func(%s.Context,*%s.Tx,%s)error", name, context, database, m.name)
	}
	for _, name := range []string{"Created", "Updated", "Deleted", "Saved", "Restored", "ForceDeleted"} {
		e.line("// %s runs after SQL inside the transaction; returning an error rolls back the write.", name)
		e.line("%s func(%s.Context,*%s.Tx,%sChanges)error", name, context, database, m.name)
	}
	e.line("// AfterCommit runs only after the outer transaction commits. Failures cannot roll back committed data; this callback is not crash durable.")
	e.line("AfterCommit func(%s.Context,%s.Operation,%sChanges)error", context, lifecycle, m.name)
	e.line("}")
	e.line("// %sObserver retains this model's concrete hook factory type. Names are unique within the selected database.", m.name)
	e.line("type %sObserver=%s.Observer[%s,%sHooks]", m.name, lifecycle, m.name, m.name)
	e.line("// New%sObserver declares a reusable typed observer identifier without registering or constructing hooks.", m.name)
	e.line("func New%sObserver(%s string)%sObserver{return %s.NewObserver[%s,%sHooks](%s)}", m.name, observer, m.name, lifecycle, m.name, m.name, observer)
	e.line("// Register%sObserver binds dependency construction to a database module. Its returned factory runs once per normal write; reads and set-based writes skip it.", m.name)
	e.line("func Register%sObserver(%s *%s.Registrar,%s %s.Key[*%s.DB],%s %sObserver,%s func(%s.Resolver)(func()%sHooks,error))error{return %s.RegisterObserver(%s,%s,%s,%s)}", m.name, registrar, foundation, pool, foundation, database, observer, m.name, construct, foundation, m.name, database, registrar, pool, observer, construct)
	e.line("func(%sHooks)foundryWriteHooks(%s %s.Context,%s %s.Observers)(%s.WriteHooks[%s],error){", m.name, ctx, context, set, lifecycle, query, m.name)
	e.line("%s,%s:=%s.ObserverFactories[%s,%sHooks](%s);if %s!=nil{return %s.WriteHooks[%s]{},%s};var %s []%sHooks", factories, errName, lifecycle, m.name, m.name, set, errName, query, m.name, errName, all, m.name)
	checkFactory := func() {
		e.line("if %s:=%s.Err();%s!=nil{return %s.WriteHooks[%s]{},%s}", errName, ctx, errName, query, m.name, errName)
	}
	checkFactory()
	if m.hooks != "" {
		e.line("%s=append(%s,%s())", all, all, m.hooks)
	}
	e.line("for _,%s:=range %s{", factory, factories)
	checkFactory()
	e.line("%s=append(%s,%s())}", all, all, factory)
	checkFactory()
	e.line("return %s.WriteHooks[%s]{", query, m.name)
	e.line("Before:func(%s %s.Context,%s *%s.Tx,%s %s.Operation,%s %s.Optional[%s],%s %s.Mutation[%s])(%s.Mutation[%s],error){", ctx, context, tx, database, operation, lifecycle, before, value, m.name, mutation, query, m.name, query, m.name)
	e.line("%s,%s:=%s.ReadMutation(%s);if %s!=nil{return %s.Mutation[%s]{},%s};var %s %sDraft", values, errName, query, mutation, errName, query, m.name, errName, draft, m.name)
	for _, f := range m.fields {
		e.line("%s.field%s,%s=%s.MutationValue[%s,%s](%s,%q,%q);if %s!=nil{return %s.Mutation[%s]{},%s}", draft, f.name, errName, query, m.name, e.fieldInputType(f, true), values, m.table, f.column, errName, query, m.name, errName)
	}
	checkBefore := func() {
		e.line("if %s:=%s.Err();%s!=nil{return %s.Mutation[%s]{},%s}", errName, ctx, errName, query, m.name, errName)
	}
	callBefore := func(name, args string) {
		e.line("for _,%s:=range %s{", hooks, all)
		e.line("if %s.%s!=nil{if %s:=%s.%s(%s,%s,%s);%s!=nil{return %s.Mutation[%s]{},%s}}", hooks, name, errName, hooks, name, ctx, tx, args, errName, query, m.name, errName)
		checkBefore()
		e.line("}")
	}
	checkBefore()
	e.line("if %s==%s.Create||%s==%s.Update{", operation, lifecycle, operation, lifecycle)
	callBefore("Saving", before+",&"+draft)
	e.line("}")
	e.line("switch %s{case %s.Create:", operation, lifecycle)
	callBefore("Creating", "&"+draft)
	e.line("case %s.Update,%s.Delete,%s.SoftDelete,%s.Restore,%s.ForceDelete:", lifecycle, lifecycle, lifecycle, lifecycle, lifecycle)
	e.line("%s,%s:=%s.Get();if !%s{return %s.Mutation[%s]{},%s.New(%s.Invalid,\"model hook requires a stored snapshot\")}", current, present, before, present, query, m.name, fault, fault)
	e.line("switch %s{case %s.Update:", operation, lifecycle)
	callBefore("Updating", current+",&"+draft)
	e.line("case %s.Delete,%s.SoftDelete:", lifecycle, lifecycle)
	callBefore("Deleting", current)
	e.line("case %s.Restore:", lifecycle)
	callBefore("Restoring", current)
	e.line("case %s.ForceDelete:", lifecycle)
	callBefore("ForceDeleting", current)
	callBefore("Deleting", current)
	e.line("};default:return %s.Mutation[%s]{},%s.New(%s.Invalid,\"invalid model hook operation\")}", query, m.name, fault, fault)
	e.line("return %s.foundryMutation(false)},", draft)
	e.line("After:func(%s %s.Context,%s *%s.Tx,%s %s.Operation,%s,%s %s.Optional[%s],%s %s.Mutation[%s])error{", ctx, context, tx, database, operation, lifecycle, before, after, value, m.name, mutation, query, m.name)
	e.line("%s,%s:=%s.ReadMutation(%s);if %s!=nil{return %s};var %s %sDraft", values, errName, query, mutation, errName, errName, draft, m.name)
	for _, f := range m.fields {
		e.line("if %s.Has(%q,%q){var %s %s;%s.field%s=%s.Set(%s)}", values, m.table, f.column, zero, e.fieldInputType(f, true), draft, f.name, value, zero)
	}
	e.line("%s,%s:=Compare%s(%s,%s,%s);if %s!=nil{return %s};%s.operation=%s.Set(%s)", changes, errName, m.name, before, after, draft, errName, errName, changes, value, operation)
	checkAfter := func() { e.line("if %s:=%s.Err();%s!=nil{return %s}", errName, ctx, errName, errName) }
	callAfter := func(name string) {
		e.line("for _,%s:=range %s{", hooks, all)
		e.line("if %s.%s!=nil{if %s:=%s.%s(%s,%s,%s);%s!=nil{return %s}}", hooks, name, errName, hooks, name, ctx, tx, changes, errName, errName)
		checkAfter()
		e.line("}")
	}
	checkAfter()
	e.line("switch %s{", operation)
	for _, pair := range [][2]string{{"Create", "Created"}, {"Update", "Updated"}, {"Delete", "Deleted"}, {"SoftDelete", "Deleted"}, {"Restore", "Restored"}} {
		e.line("case %s.%s:", lifecycle, pair[0])
		callAfter(pair[1])
	}
	e.line("case %s.ForceDelete:", lifecycle)
	callAfter("Deleted")
	callAfter("ForceDeleted")
	e.line("default:return %s.New(%s.Invalid,\"invalid model hook operation\")}", fault, fault)
	e.line("if %s==%s.Create||%s==%s.Update{", operation, lifecycle, operation, lifecycle)
	callAfter("Saved")
	e.line("}")
	e.line("for _,%s:=range %s{if %s.AfterCommit!=nil{if %s:=%s.AfterCommit(func(%s %s.Context)error{return %s.AfterCommit(%s,%s,%s)});%s!=nil{return %s}}};return nil},},nil}", hooks, all, hooks, errName, tx, ctx, context, hooks, ctx, operation, changes, errName, errName)
}
