//go:build !unix

package observability

func readProcessStats() processStats { return processStats{} }
