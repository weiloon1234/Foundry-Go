package datatable

import "github.com/weiloon1234/Foundry-Go/http"

// ColumnFeature removes a default capability from a direct column constructor.
// Zero or unknown values fail table declaration validation; repeated valid flags
// are harmless. For joined, computed, or custom-codec columns, use Column.
// Example: datatable.Text(w.Name, "users.fields.name", Users.Name, datatable.NoExport).
type ColumnFeature uint8

const (
	// NoSort disables sorting on a direct column constructor.
	NoSort ColumnFeature = iota + 1
	// NoFilter disables filtering (and search) on a direct column constructor.
	NoFilter
	// NoSearch disables text search while keeping the pattern filter on Text and NullableText; passing NoSearch to any other constructor fails declaration validation.
	NoSearch
	// NoExport excludes a direct column constructor from CSV/XLSX export.
	NoExport
)

func featureFlags(defaultSearch bool, features []ColumnFeature) (sort, where, search, export bool, err error) {
	sort, where, search, export = true, true, defaultSearch, true
	for _, f := range features {
		switch f {
		case NoSort:
			sort = false
		case NoFilter:
			where = false
			search = false
		case NoSearch:
			if !defaultSearch {
				return false, false, false, false, invalid("column feature NoSearch applies only to text columns")
			}
			search = false
		case NoExport:
			export = false
		default:
			if f == 0 {
				return false, false, false, false, invalid("column feature is missing")
			}
			return false, false, false, false, invalid("column feature is unknown")
		}
	}
	return sort, where, search, export, nil
}

var (
	defaultStringQuery = http.StringQuery[string]()
	defaultBoolQuery   = http.BoolQuery[bool]()
)

func stringQueryCodec[V ~string]() http.QueryCodec[V] {
	if c, ok := any(defaultStringQuery).(http.QueryCodec[V]); ok {
		return c
	}
	return http.StringQuery[V]()
}

func boolQueryCodec[V ~bool]() http.QueryCodec[V] {
	if c, ok := any(defaultBoolQuery).(http.QueryCodec[V]); ok {
		return c
	}
	return http.BoolQuery[V]()
}
