package querycore

type LockStrength uint8

const (
	LockUpdate LockStrength = iota + 1
	LockNoKeyUpdate
	LockShare
	LockKeyShare
)

type LockBehavior uint8

const (
	LockWait LockBehavior = iota
	LockNoWait
	LockSkip
)

type LockSpec struct {
	Strength LockStrength
	Behavior LockBehavior
	Targets  []string
	// nil targets means all eligible sources; an explicitly empty Of is invalid.
	Explicit bool
}

// lockSources tracks null extension introduced by every join, including a
// later RIGHT/FULL JOIN that makes an earlier preserved side nullable.
func LockSources(s SelectNode) ([]TableSource, []bool) {
	sources := []TableSource{s.Source}
	nullable := []bool{false}
	nullablePrefix := 0
	for _, join := range s.Joins {
		if join.Kind == RightJoin || join.Kind == FullJoin {
			nullablePrefix = len(sources)
		}
		sources = append(sources, join.Source)
		nullable = append(nullable, join.Kind == LeftJoin || join.Kind == FullJoin)
	}
	for i := 0; i < nullablePrefix; i++ {
		nullable[i] = true
	}
	return sources, nullable
}
