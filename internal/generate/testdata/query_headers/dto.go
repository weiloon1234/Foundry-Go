package sample

//foundry:dto
type Input struct {
	Field string `json:"-" header:"X-Name"`
}
