package jsonshape

// Scalar format names are shared by public contracts, Go discovery and runtime
// URL codecs. This layer has no dependency on transport or generated packages.
const (
	UUIDFormat          = "uuid"
	DecimalFormat       = "decimal"
	DateFormat          = "date"
	TimeFormat          = "time"
	DateTimeFormat      = "date_time"
	LocalDateTimeFormat = "local_date_time"
	IntervalFormat      = "interval"
	Base64Format        = "base64"
)

// ScalarFormat recognizes only the exact declarations whose wire formats
// Foundry owns. A new named type with the same underlying fields is not inferred.
func ScalarFormat(packagePath, name string) string {
	switch packagePath {
	case "github.com/weiloon1234/Foundry-Go/decimal":
		if name == "Decimal" {
			return DecimalFormat
		}
	case "time":
		if name == "Time" {
			return DateTimeFormat
		}
	case "github.com/weiloon1234/Foundry-Go/temporal":
		switch name {
		case "Date":
			return DateFormat
		case "Time":
			return TimeFormat
		case "DateTime":
			return DateTimeFormat
		case "LocalDateTime":
			return LocalDateTimeFormat
		case "Interval":
			return IntervalFormat
		}
	}
	return ""
}
