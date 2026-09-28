package lifecycle

// Operation identifies a normal model write. Bulk statements have separate
// semantics and do not dispatch per-model hooks.
type Operation uint8

const (
	Create Operation = iota + 1
	Update
	Delete
	SoftDelete
	Restore
	ForceDelete
)

func (o Operation) String() string {
	switch o {
	case Create:
		return "create"
	case Update:
		return "update"
	case Delete:
		return "delete"
	case SoftDelete:
		return "soft_delete"
	case Restore:
		return "restore"
	case ForceDelete:
		return "force_delete"
	default:
		return "invalid model operation"
	}
}
