package sample

type Tokens []string

func (Tokens) MarshalText() ([]byte, error) { return nil, nil }

//foundry:query
type Input struct {
	Field Tokens `header:"X-Name"`
}
