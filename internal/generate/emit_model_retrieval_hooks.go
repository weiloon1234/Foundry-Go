package generate

func (e *emitter) emitModelRetrievalHooks(m model) {
	query, database, context := e.use(framework+"/database/query"), e.use(framework+"/database"), e.use("context")
	lifecycle, foundation := e.use(framework+"/database/lifecycle"), e.use(framework+"/foundation")
	ctx, executor, item := e.localName("ctx"), e.localName("executor"), e.localName("item")
	set, factories, factory := e.localName("observerSet"), e.localName("factories"), e.localName("factory")
	all, hooks, errName := e.localName("allHooks"), e.localName("hooks"), e.localName("err")
	registrar, pool, observer, construct := e.localName("registrar"), e.localName("pool"), e.localName("observer"), e.localName("construct")
	e.line("// %sRetrievalHooks declares complete-model read callbacks. Reference a local factory with retrieval=Factory on the model directive. The stored model is a read-only value; getters remain explicit.", m.name)
	e.line("type %sRetrievalHooks struct{", m.name)
	e.line("// Retrieved runs after successful stored hydration and row closure. The executor retains the caller's capability and wrappers; no write transaction is created implicitly. Return an error to stop this read. Nil is skipped.")
	e.line("Retrieved func(%s.Context,%s.Executor,%s)error", context, database, m.name)
	e.line("}")
	e.line("// %sRetrievalObserver retains this model's concrete retrieval factory type. Names share the selected database's write/retrieval namespace.", m.name)
	e.line("type %sRetrievalObserver=%s.Observer[%s,%sRetrievalHooks]", m.name, lifecycle, m.name, m.name)
	e.line("// New%sRetrievalObserver declares a typed retrieval observer without registering or constructing hooks.", m.name)
	e.line("func New%sRetrievalObserver(%s string)%sRetrievalObserver{return %s.NewRetrievalObserver[%s,%sRetrievalHooks](%s)}", m.name, observer, m.name, lifecycle, m.name, m.name, observer)
	e.line("// Register%sRetrievalObserver binds dependency construction to a database module. Its factory runs once per nonempty complete fetch/batch and selected model role; write hydration, DTOs and scalar reads skip it.", m.name)
	e.line("func Register%sRetrievalObserver(%s *%s.Registrar,%s %s.Key[*%s.DB],%s %sRetrievalObserver,%s func(%s.Resolver)(func()%sRetrievalHooks,error))error{return %s.RegisterObserver(%s,%s,%s,%s)}", m.name, registrar, foundation, pool, foundation, database, observer, m.name, construct, foundation, m.name, database, registrar, pool, observer, construct)
	e.line("func(%sRetrievalHooks)foundryRetrievalHooks(%s %s.Context,%s %s.Observers)(%s.RetrievalHooks[%s],error){", m.name, ctx, context, set, lifecycle, query, m.name)
	e.line("%s,%s:=%s.RetrievalObserverFactories[%s,%sRetrievalHooks](%s);if %s!=nil{return %s.RetrievalHooks[%s]{},%s};var %s []%sRetrievalHooks", factories, errName, lifecycle, m.name, m.name, set, errName, query, m.name, errName, all, m.name)
	check := func() {
		e.line("if %s:=%s.Err();%s!=nil{return %s.RetrievalHooks[%s]{},%s}", errName, ctx, errName, query, m.name, errName)
	}
	check()
	if m.retrieval != "" {
		e.line("%s=append(%s,%s())", all, all, m.retrieval)
	}
	e.line("for _,%s:=range %s{", factory, factories)
	check()
	e.line("%s=append(%s,%s())}", all, all, factory)
	check()
	e.line("return %s.RetrievalHooks[%s]{Retrieved:func(%s %s.Context,%s %s.Executor,%s %s)error{", query, m.name, ctx, context, executor, database, item, m.name)
	e.line("for _,%s:=range %s{", hooks, all)
	e.line("if %s:=%s.Err();%s!=nil{return %s}", errName, ctx, errName, errName)
	e.line("if %s.Retrieved!=nil{if %s:=%s.Retrieved(%s,%s,%s);%s!=nil{return %s}}}", hooks, errName, hooks, ctx, executor, item, errName, errName)
	e.line("return %s.Err()},},nil}", ctx)
}
