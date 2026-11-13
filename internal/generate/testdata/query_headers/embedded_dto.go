package sample

type Child struct {
	Field string `header:"X-Name"`
}

//foundry:dto
type Input struct {
	Child
}
