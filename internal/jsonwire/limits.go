package jsonwire

// Limits bounds one transport document. Bytes and Nodes must be positive.
// Depth is the maximum child depth, with the root at zero; it may be reduced
// below MaxDepth but cannot exceed that parser recursion bound. Object names
// consume one node each in addition to their values. There are no implicit
// transport defaults here: the calling boundary owns its configuration.
type Limits struct {
	Bytes int
	Depth int
	Nodes int
}

func (l Limits) valid() bool {
	return l.Bytes > 0 && l.Nodes > 0 && l.Depth >= 0 && l.Depth <= MaxDepth
}
