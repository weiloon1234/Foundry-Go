package querycore

// Cursor limits bound token decoding and lexicographic predicate construction.
const (
	MaxCursorBytes  = 64 * 1024
	MaxCursorFields = 16
)
