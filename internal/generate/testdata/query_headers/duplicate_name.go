package sample

//foundry:query
type Input struct {
	First  string `header:"X-Name"`
	Second string `header:"x-NAME"`
}
