package datatable

// ColumnKind classifies the data and filter behavior of a datatable column.
//
//foundry:enum
type ColumnKind string

const (
	ColumnText          ColumnKind = "text"
	ColumnInteger       ColumnKind = "integer"
	ColumnDecimal       ColumnKind = "decimal"
	ColumnBoolean       ColumnKind = "boolean"
	ColumnEnum          ColumnKind = "enum"
	ColumnDate          ColumnKind = "date"
	ColumnDateTime      ColumnKind = "date_time"
	ColumnLocalDateTime ColumnKind = "local_date_time"
	ColumnTime          ColumnKind = "time"
)
