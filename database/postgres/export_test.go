package postgres

// Native setup hooks let external tests share testkit schema ownership while
// exercising startup search paths that the public configuration does not expose.
var TestConnectionConfig = connectionConfig
var TestInitializeTypeRegistry = initializeTypeRegistry
