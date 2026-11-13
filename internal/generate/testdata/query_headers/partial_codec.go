package sample

type Token string

func (Token) MarshalText() ([]byte, error) { return nil, nil }

//foundry:query
type Input struct {
	Field Token `header:"X-Name"`
}
