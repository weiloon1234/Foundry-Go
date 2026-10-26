package oracle

// publishFixture creates synthetic integrity fixtures only. No runtime recorder remains.
func publishFixture(dir string, b Bundle, refreeze bool) error {
	return Publish(dir, b, refreeze)
}
