package sample

//foundry:query
type Input struct {
	Field string `query:"q" header:"-"`
}
