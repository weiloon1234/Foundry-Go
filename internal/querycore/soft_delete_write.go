package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
)

// SQL shape and lifecycle intent are distinct: restoration and soft deletion
// write updates without emitting ordinary saving/updating callbacks.
func (kind MutationKind) SqlKind() MutationKind {
	switch kind {
	case SoftDeleteModel, RestoreModel:
		return UpdateModel
	case ForceDeleteModel:
		return DeleteModel
	default:
		return kind
	}
}

func (kind MutationKind) Operation() lifecycle.Operation {
	switch kind {
	case InsertModel:
		return lifecycle.Create
	case UpdateModel:
		return lifecycle.Update
	case DeleteModel:
		return lifecycle.Delete
	case SoftDeleteModel:
		return lifecycle.SoftDelete
	case RestoreModel:
		return lifecycle.Restore
	case ForceDeleteModel:
		return lifecycle.ForceDelete
	default:
		return 0
	}
}
