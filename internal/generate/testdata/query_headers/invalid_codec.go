package sample

type Token string

func (Token) MarshalText() string { return "" }

//foundry:query
type Input struct {
	Field Token `header:"X-Name"`
}
