package querycore

// Conflict contains SQL policy data only; public facades retain model ownership.
type Conflict struct {
	Keys                                     []ValueExpression
	Constraint                               string
	Named                                    bool
	Action                                   ConflictAction
	Updates                                  []ConflictUpdate
	Condition, RowCondition, TargetCondition []Expression
}

type ConflictAction uint8

const (
	_ ConflictAction = iota
	ConflictNothing
	ConflictDoUpdate
)

type ConflictUpdate struct {
	Field          FieldRef
	Literal        *AssignmentValue
	MutatedLiteral bool
	Incoming, Null bool
	Value          ValueExpression
}

const ConflictStoredTable = "foundry_conflict_stored"
