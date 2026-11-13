package sample

//foundry:path pattern=/headers
type Input struct {
	Field string `path:"-" header:"X-Name"`
}
