package querycore

import (
	"database/sql/driver"
)

type Adjustment struct {
	Field    FieldRef
	Bind     func() (driver.Value, error)
	Subtract bool
}
