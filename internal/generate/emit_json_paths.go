package generate

import "strings"

// jsonPathName keeps property-path segments unambiguous in DTO validation names.
func jsonPathName(segment string) string { return strings.ReplaceAll(segment, "_", "__") }
