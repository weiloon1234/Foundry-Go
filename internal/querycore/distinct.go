package querycore

type DistinctKind uint8

const (
	NoDistinct DistinctKind = iota
	DistinctRows
	DistinctOn
)

type DistinctSpec struct {
	Kind DistinctKind
	Keys []ValueExpression
}
