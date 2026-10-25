package querycore

import (
	"database/sql/driver"
)

// mutationAssignment is lowered SQL data. Public Assignment/Mutation keep the
// owner, while validation and compilation share this value across models.
type MutationAssignment struct {
	Field FieldRef
	AssignmentValue
	// seal encrypts an encrypted field's plaintext for its row before binding.
	Seal Sealer
}

// assignmentValue is shared by model drafts and conflict literals. Keeping the
// captured value independent of M avoids re-instantiating its codec closure for
// every model/query scope while the public Assignment/Conflict retain ownership.
type AssignmentValue struct {
	Bind      func() (driver.Value, error)
	Value     *InputState // Private type erasure; metadata checks the exact V before mutation.
	CopyValue func() any
}

func (v AssignmentValue) InputValue() any {
	if v.CopyValue != nil {
		return v.CopyValue()
	}
	return StoredInput(v.Value)
}

type MutationKind uint8

const (
	InsertModel MutationKind = iota
	UpdateModel
	DeleteModel
	SoftDeleteModel
	RestoreModel
	ForceDeleteModel
)

// deletes reports the kinds deletion observers observe: delete, soft delete
// and force delete.
func (k MutationKind) Deletes() bool {
	return k == DeleteModel || k == SoftDeleteModel || k == ForceDeleteModel
}
