package postgres

import "database/sql/driver"

// Native setup hooks let external tests share testkit schema ownership while
// exercising startup search paths that the public configuration does not expose.
var TestConnectionConfig = connectionConfig
var TestInitializeTypeRegistry = initializeTypeRegistry

var TestProtectedConnector = func(source driver.Connector) driver.Connector {
	return protectedConnector{source: source}
}
