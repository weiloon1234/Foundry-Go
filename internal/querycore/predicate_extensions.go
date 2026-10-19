package querycore

type NullPlacement uint8

const (
	NullsDefault NullPlacement = iota
	NullsFirst
	NullsLast
)

// orderDirection renders direction and any explicit NULL placement.
func OrderDirection(order OrderNode) string {
	direction := " ASC"
	if order.Descending {
		direction = " DESC"
	}
	switch order.Nulls {
	case NullsFirst:
		direction += " NULLS FIRST"
	case NullsLast:
		direction += " NULLS LAST"
	}
	return direction
}
