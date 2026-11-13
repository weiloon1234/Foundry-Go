package sample

type Token string

//foundry:query
type Input struct {
	Token `header:"X-Name"`
}
