package sample

//foundry:form
type Input struct {
	Field string `form:"-" header:"X-Name"`
}
