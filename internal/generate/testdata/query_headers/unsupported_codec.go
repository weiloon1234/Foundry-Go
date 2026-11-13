package sample

//foundry:query
type Input struct {
	Field map[string]string `header:"X-Name"`
}
