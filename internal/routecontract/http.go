package routecontract

func ValidHTTP(mode string) bool {
	return mode == "body" || mode == "response" || mode == "both"
}

// DefaultHTTP is the single transport-helper policy used by generation and migration.
func DefaultHTTP(_ string) string { return "none" }

func HasBody(mode string) bool     { return mode == "body" || mode == "both" }
func HasResponse(mode string) bool { return mode == "response" || mode == "both" }
