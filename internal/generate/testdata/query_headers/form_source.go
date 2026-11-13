package sample

//foundry:query
type Input struct {
	Field string `form:"f" header:"X-Name"`
}
