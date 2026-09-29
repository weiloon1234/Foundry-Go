//go:build unix && !linux

package observability

// Other Unix systems report CPU time and descriptor limits only; counting open
// descriptors or resident memory there needs platform-specific interfaces.
func addPlatformProcessStats(*processStats) {}
