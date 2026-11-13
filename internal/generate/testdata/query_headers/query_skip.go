package sample

//foundry:query
type Input struct {
	Field string `query:"-" header:"X-Name"`
}
