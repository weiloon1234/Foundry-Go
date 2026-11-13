package sample

//foundry:multipart
type Input struct {
	Field string `form:"-" header:"X-Name"`
}
