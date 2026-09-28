// Package status demonstrates enums generated in a separate consumer package.
package status

//foundry:enum
type Status string

const (
	Available    Status = "available"
	Discontinued Status = "discontinued"
)
