package ormcore

// MergeNonCollatable conservatively resolves SQL result capability: a result
// chosen from several inputs cannot gain collation support from another input.
func MergeNonCollatable(inputs ...bool) bool {
	for _, marked := range inputs {
		if marked {
			return true
		}
	}
	return false
}
