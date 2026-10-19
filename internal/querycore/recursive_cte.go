package querycore

// Pointer identity binds the working table to one recursive declaration without
// making the AST cyclic or giving an escaped self-reference a new definition.
type RecursiveReference struct {
	Name    string
	Columns []Column
}

type RecursiveCTE struct {
	Reference *RecursiveReference
	Step      SelectNode
	Operator  SetOperator
}
