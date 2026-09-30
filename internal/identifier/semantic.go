// Package identifier validates persisted semantic identifiers at declaration
// boundaries. SQL identifiers use the separate SQL grammar and backend limits.
package identifier

import "regexp"

var semantic = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

// MaxSemanticBytes bounds a semantic identifier, including message keys.
const MaxSemanticBytes = 128

func Semantic(name string) bool {
	return len(name) > 0 && len(name) <= MaxSemanticBytes && semantic.MatchString(name)
}
