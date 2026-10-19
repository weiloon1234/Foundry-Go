package querycore

type CteMaterialization uint8

const (
	CteDefault CteMaterialization = iota
	CteMaterialized
	CteNotMaterialized
)

type CteNode struct {
	Name            string
	Query           SelectNode
	Columns         []Column
	Materialization CteMaterialization
	Recursion       *RecursiveCTE
}
