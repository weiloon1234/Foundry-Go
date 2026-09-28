// Package keyqueries exercises typed computed grouping and selection keys.
package keyqueries

//foundry:projection
type BucketCount struct {
	Bucket int
	Count  int64
}
