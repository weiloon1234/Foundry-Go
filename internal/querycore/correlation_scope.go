package querycore

type ScopeRequirement struct {
	Sources map[string][]Column
	Err     error
}
